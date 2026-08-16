package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/auth"
	"github.com/nerdswhofish/coop/internal/domain"
	"github.com/nerdswhofish/coop/internal/store"
	"github.com/nerdswhofish/coop/internal/youtube"
)

// handleSetupStatus reports whether this instance still needs its first family.
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) error {
	count, err := s.deps.Accounts.FamilyCount(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, s.deps.Logger, http.StatusOK, map[string]bool{"needsSetup": count == 0})
	return nil
}

// handleSetup creates the first family and its admin. Available only while no
// family exists: leaving it open would let anyone who can reach the instance
// make themselves an account on it.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		FamilyName string `json:"familyName"`
		Timezone   string `json:"timezone"`
		Email      string `json:"email"`
		Password   string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	setupKeys := []string{auth.HashToken("client-address:" + s.clientAddress(r))}
	if until, locked, err := s.deps.Accounts.AuthLocked(r.Context(), "initial-setup", setupKeys); err != nil {
		return err
	} else if locked {
		return rateLimited(until.Sub(s.deps.Now()))
	}

	count, err := s.deps.Accounts.FamilyCount(r.Context())
	if err != nil {
		return err
	}
	if count > 0 {
		if err := s.recordThrottleFailure(r, "initial-setup", setupKeys); err != nil {
			return err
		}
		return conflict("already_set_up", "this instance already has a family")
	}

	if body.Email == "" || body.FamilyName == "" {
		return badRequest("familyName and email are required")
	}
	if body.Timezone == "" {
		body.Timezone = "UTC"
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		return badRequest(err.Error())
	}

	_, parent, err := s.deps.Accounts.CreateInitialFamily(r.Context(),
		body.FamilyName, body.Timezone, body.Email, hash)
	if errors.Is(err, store.ErrAlreadySetup) {
		if failureErr := s.recordThrottleFailure(r, "initial-setup", setupKeys); failureErr != nil {
			return failureErr
		}
		return conflict("already_set_up", "this instance already has a family")
	}
	if err != nil {
		return err
	}

	challenge, err := s.beginParentAuth(r, parent)
	if err != nil {
		return err
	}
	writeJSON(w, s.deps.Logger, http.StatusCreated, challenge)
	return nil
}

// handleLogin exchanges credentials for a session.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	keys := s.authThrottleKeys(body.Email, r)
	if err := s.rejectLockedAuth(r, keys); err != nil {
		return err
	}

	parent, err := s.deps.Accounts.ParentByEmail(r.Context(), body.Email)
	if err != nil {
		// Spend the same work as a real verification, so response timing does
		// not reveal which email addresses have accounts.
		auth.SpendVerifyTime()
		if failureErr := s.recordAuthFailure(r, keys); failureErr != nil {
			return failureErr
		}
		return unauthorized()
	}

	ok, err := auth.VerifyPassword(parent.PasswordHash, body.Password)
	if err != nil || !ok {
		if failureErr := s.recordAuthFailure(r, keys); failureErr != nil {
			return failureErr
		}
		return unauthorized()
	}

	challenge, err := s.beginParentAuth(r, parent)
	if err != nil {
		return err
	}
	writeJSON(w, s.deps.Logger, http.StatusOK, challenge)
	return nil
}

// handleAcceptParentInvitation turns a one-time invitation into an adult
// login. The invitation code stays in the body so reverse proxies do not log it.
func (s *Server) handleAcceptParentInvitation(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	if body.Code == "" {
		return badRequest("code is required")
	}
	keys := []string{
		auth.HashToken("parent-invitation:" + body.Code),
		auth.HashToken("client-address:" + s.clientAddress(r)),
	}
	if until, locked, err := s.deps.Accounts.AuthLocked(r.Context(), "parent-invitation", keys); err != nil {
		return err
	} else if locked {
		return rateLimited(until.Sub(s.deps.Now()))
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		return badRequest(err.Error())
	}
	parent, err := s.deps.Accounts.RedeemParentInvitation(
		r.Context(), auth.HashToken(body.Code), hash)
	if errors.Is(err, store.ErrNotFound) {
		if failureErr := s.recordThrottleFailure(r, "parent-invitation", keys); failureErr != nil {
			return failureErr
		}
		return unauthorized()
	}
	if err != nil {
		return err
	}
	if err := s.deps.Accounts.ClearAuthThrottle(r.Context(), "parent-invitation", keys); err != nil {
		return err
	}

	challenge, err := s.beginParentAuth(r, parent)
	if err != nil {
		return err
	}
	writeJSON(w, s.deps.Logger, http.StatusCreated, challenge)
	return nil
}

func (s *Server) beginParentAuth(r *http.Request, parent store.Parent) (authChallengeDTO, error) {
	challengeToken, err := auth.NewToken()
	if err != nil {
		return authChallengeDTO{}, internal(err)
	}
	expiresAt := s.deps.Now().Add(s.deps.Config.Auth.ChallengeTTL)
	challenge := store.ParentAuthChallenge{
		ParentID:  parent.ID,
		TokenHash: challengeToken.Hash,
		Purpose:   store.AuthPurposeLogin,
		ExpiresAt: expiresAt,
	}
	response := authChallengeDTO{
		Challenge: challengeToken.Plain,
		ExpiresAt: expiresAt,
		Method:    "totp",
	}

	if len(parent.EncryptedTOTPSecret) == 0 {
		secret, provisioningURL, err := auth.NewTOTPSecret(parent.Email)
		if err != nil {
			return authChallengeDTO{}, internal(err)
		}
		sealed, err := s.deps.Sealer.SealString(secret)
		if err != nil {
			return authChallengeDTO{}, internal(err)
		}
		challenge.Purpose = store.AuthPurposeEnroll
		challenge.EncryptedTOTPSecret = sealed
		response.Enroll = &authEnrollmentDTO{Secret: secret, ProvisioningURL: provisioningURL}
	}

	if err := s.deps.Accounts.CreateAuthChallenge(r.Context(), challenge); err != nil {
		return authChallengeDTO{}, err
	}
	return response, nil
}

func (s *Server) handleVerifyTOTP(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	if body.Challenge == "" || body.Code == "" {
		return badRequest("challenge and code are required")
	}

	challengeHash := auth.HashToken(body.Challenge)
	challenge, parent, err := s.deps.Accounts.AuthChallengeByToken(
		r.Context(), challengeHash, s.deps.Config.Auth.ChallengeMaxAttempts)
	if err != nil {
		return unauthorized()
	}
	keys := s.authThrottleKeys(parent.Email, r)
	if err := s.rejectLockedAuth(r, keys); err != nil {
		return err
	}

	sealed := parent.EncryptedTOTPSecret
	if challenge.Purpose == store.AuthPurposeEnroll {
		sealed = challenge.EncryptedTOTPSecret
	}
	secret, err := s.deps.Sealer.OpenString(sealed)
	if err != nil {
		return internal(err)
	}
	step, ok := auth.MatchTOTPStep(body.Code, secret, s.deps.Now())
	if !ok {
		if err := s.deps.Accounts.FailAuthChallenge(r.Context(), challengeHash); err != nil {
			return err
		}
		if err := s.recordAuthFailure(r, keys); err != nil {
			return err
		}
		return unauthorized()
	}

	expiresAt := s.deps.Now().Add(s.deps.Config.Auth.ParentSessionTTL)
	token, err := auth.NewToken()
	if err != nil {
		return internal(err)
	}
	parent, err = s.deps.Accounts.CompleteAuthChallenge(
		r.Context(), challenge.ID, step, s.deps.Config.Auth.ChallengeMaxAttempts,
		store.ParentSession{TokenHash: token.Hash, ExpiresAt: expiresAt})
	if errors.Is(err, store.ErrAuthChallengeInvalid) || errors.Is(err, store.ErrTOTPReplay) {
		return unauthorized()
	}
	if err != nil {
		return err
	}
	if err := s.deps.Accounts.ClearAuthThrottle(r.Context(), "parent-login", keys); err != nil {
		return err
	}

	scoped, err := s.scopeOf(r, parent)
	if err != nil {
		return err
	}
	writeJSON(w, s.deps.Logger, http.StatusOK, sessionDTO{
		Token:     token.Plain,
		ExpiresAt: expiresAt,
		Parent:    newParentDTO(parent, scoped),
	})
	return nil
}

func (s *Server) authThrottleKeys(email string, r *http.Request) []string {
	normalized := strings.ToLower(strings.TrimSpace(email))
	return []string{
		auth.HashToken("parent-email:" + normalized),
		auth.HashToken("client-address:" + s.clientAddress(r)),
	}
}

func (s *Server) rejectLockedAuth(r *http.Request, keys []string) error {
	until, locked, err := s.deps.Accounts.AuthLocked(r.Context(), "parent-login", keys)
	if err != nil {
		return err
	}
	if locked {
		return rateLimited(until.Sub(s.deps.Now()))
	}
	return nil
}

func (s *Server) recordAuthFailure(r *http.Request, keys []string) error {
	return s.recordThrottleFailure(r, "parent-login", keys)
}

func (s *Server) recordThrottleFailure(r *http.Request, action string, keys []string) error {
	cfg := s.deps.Config.Auth
	return s.deps.Accounts.RecordAuthFailure(r.Context(), action, keys,
		cfg.MaxFailures, cfg.FailureWindow, cfg.LockoutDuration)
}

func (s *Server) scopeOf(r *http.Request, parent store.Parent) ([]uuid.UUID, error) {
	if parent.Role == domain.RoleAdmin {
		return nil, nil
	}
	return s.deps.Accounts.ScopedChildIDs(r.Context(), parent.ID)
}

func (s *Server) handleParentMe(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	parent, err := s.deps.Accounts.Parent(r.Context(), p.ID)
	if err != nil {
		return err
	}
	writeJSON(w, s.deps.Logger, http.StatusOK, newParentDTO(parent, p.ScopedChildIDs))
	return nil
}

func (s *Server) handleGetFamily(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	family, err := s.deps.Accounts.Family(r.Context(), p.FamilyID)
	if err != nil {
		return err
	}
	writeJSON(w, s.deps.Logger, http.StatusOK, newFamilyDTO(family))
	return nil
}

func (s *Server) handleUpdateFamily(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	if err := p.RequireAdmin(); err != nil {
		return err
	}

	var body struct {
		Name     string `json:"name"`
		Timezone string `json:"timezone"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	if err := s.deps.Accounts.UpdateFamily(r.Context(), p.FamilyID, body.Name, body.Timezone, p.ID); err != nil {
		return err
	}

	family, err := s.deps.Accounts.Family(r.Context(), p.FamilyID)
	if err != nil {
		return err
	}
	writeJSON(w, s.deps.Logger, http.StatusOK, newFamilyDTO(family))
	return nil
}

func (s *Server) handleDeleteFamily(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	if err := p.RequireAdmin(); err != nil {
		return err
	}
	if err := s.deps.Accounts.DeleteFamily(r.Context(), p.FamilyID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleSetAPIKey validates a key against YouTube before storing it, so a typo
// surfaces immediately rather than as a broken feed hours later.
func (s *Server) handleSetAPIKey(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	if err := p.RequireAdmin(); err != nil {
		return err
	}

	var body struct {
		APIKey string `json:"apiKey"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	if body.APIKey == "" {
		return badRequest("apiKey is required")
	}

	probe, err := s.deps.YouTube.ForAPIKey(p.FamilyID, body.APIKey)
	if err != nil {
		return internal(err)
	}

	// YouTube's own channel is a stable, always-present id to probe with.
	if _, err := probe.Channels(r.Context(),
		[]string{"UCBR8-60-B28hp2BmDPdntcQ"}, domain.PurposeFeed); err != nil {
		return badRequest("YouTube could not validate that API key")
	}

	sealed, err := s.deps.Sealer.SealString(body.APIKey)
	if err != nil {
		return internal(err)
	}
	if err := s.deps.Accounts.SetAPIKey(r.Context(), p.FamilyID, sealed, p.ID); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleQuota(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	now := s.deps.Now()
	usage, err := s.deps.Quota.Usage(r.Context(), p.FamilyID, youtube.QuotaDay(now))
	if err != nil {
		return err
	}

	budgets := map[domain.QuotaPurpose]int{
		domain.PurposeFeed:     s.deps.Config.YouTube.DailyUnitBudget,
		domain.PurposeSearch:   s.deps.Config.YouTube.DailySearchBudget,
		domain.PurposeBackfill: s.deps.Config.YouTube.BackfillCallBudget,
	}

	out := make([]quotaDTO, 0, len(budgets))
	for _, purpose := range []domain.QuotaPurpose{
		domain.PurposeFeed, domain.PurposeSearch, domain.PurposeBackfill,
	} {
		spend := usage[purpose]
		used := spend.Units
		if purpose != domain.PurposeFeed {
			used = spend.Calls
		}
		out = append(out, quotaDTO{
			Purpose:  string(purpose),
			Used:     used,
			Budget:   budgets[purpose],
			ResetsAt: youtube.NextQuotaReset(now),
		})
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, out)
	return nil
}

func (s *Server) handleListParents(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	if err := p.RequireAdmin(); err != nil {
		return err
	}

	parents, err := s.deps.Accounts.Parents(r.Context(), p.FamilyID)
	if err != nil {
		return err
	}

	out := make([]parentDTO, 0, len(parents))
	for _, parent := range parents {
		scoped, err := s.scopeOf(r, parent)
		if err != nil {
			return err
		}
		out = append(out, newParentDTO(parent, scoped))
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, out)
	return nil
}

func (s *Server) handleInviteParent(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	if err := p.RequireAdmin(); err != nil {
		return err
	}

	var body struct {
		Email    string      `json:"email"`
		Role     string      `json:"role"`
		ChildIDs []uuid.UUID `json:"childIds"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	if body.Email == "" {
		return badRequest("email is required")
	}

	role := domain.ParentRole(body.Role)
	if role == "" {
		role = domain.RoleParent
	}
	if role != domain.RoleParent && role != domain.RoleAdmin {
		return badRequest("role must be admin or parent")
	}
	if role == domain.RoleAdmin && len(body.ChildIDs) > 0 {
		return badRequest("admin invitations cannot have a child scope")
	}

	// Every named child must belong to this family, or an admin could scope a
	// new parent onto someone else's children.
	for _, childID := range body.ChildIDs {
		if _, err := s.deps.Accounts.Child(r.Context(), p.FamilyID, childID); err != nil {
			return err
		}
	}

	if _, err := s.deps.Accounts.ParentByEmail(r.Context(), body.Email); err == nil {
		return conflict("parent_exists", "a parent with that email already exists")
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}

	token, err := auth.NewToken()
	if err != nil {
		return internal(err)
	}
	expiresAt := s.deps.Now().Add(s.deps.Config.Auth.InvitationTTL)
	invitation, err := s.deps.Accounts.CreateParentInvitation(r.Context(), store.ParentInvitation{
		FamilyID:  p.FamilyID,
		Email:     body.Email,
		Role:      role,
		TokenHash: token.Hash,
		CreatedBy: p.ID,
		ExpiresAt: expiresAt,
	}, body.ChildIDs)
	if err != nil {
		return err
	}

	writeJSON(w, s.deps.Logger, http.StatusCreated, invitationDTO{
		Code:      token.Plain,
		Email:     invitation.Email,
		Role:      string(invitation.Role),
		ChildIDs:  body.ChildIDs,
		ExpiresAt: invitation.ExpiresAt,
	})
	return nil
}

func (s *Server) handleDeleteParent(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	if err := p.RequireAdmin(); err != nil {
		return err
	}

	parentID, err := pathUUID(r, "parentId")
	if err != nil {
		return err
	}
	if err := s.deps.Accounts.DeleteParent(r.Context(), p.FamilyID, parentID, p.ID); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleSetScope(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	if err := p.RequireAdmin(); err != nil {
		return err
	}

	parentID, err := pathUUID(r, "parentId")
	if err != nil {
		return err
	}

	var body struct {
		ChildIDs []uuid.UUID `json:"childIds"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}

	if _, err := s.deps.Accounts.Parent(r.Context(), parentID); err != nil {
		return err
	}
	for _, childID := range body.ChildIDs {
		if _, err := s.deps.Accounts.Child(r.Context(), p.FamilyID, childID); err != nil {
			return err
		}
	}

	if err := s.deps.Accounts.SetScope(r.Context(), parentID, body.ChildIDs, p.ID); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleListChildren(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	children, err := s.deps.Accounts.Children(r.Context(), p.FamilyID)
	if err != nil {
		return err
	}

	visible := make([]store.Child, 0, len(children))
	ids := make([]uuid.UUID, 0, len(children))
	for _, child := range children {
		if p.CanManage(child.ID) {
			visible = append(visible, child)
			ids = append(ids, child.ID)
		}
	}

	pending, err := s.deps.Activity.PendingRequestCounts(r.Context(), ids)
	if err != nil {
		return err
	}

	out := make([]childDTO, 0, len(visible))
	for _, child := range visible {
		devices, err := s.deps.Accounts.Devices(r.Context(), child.ID)
		if err != nil {
			return err
		}
		out = append(out, newChildDTO(child, len(devices), pending[child.ID]))
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, out)
	return nil
}

func (s *Server) handleCreateChild(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	if err := p.RequireAdmin(); err != nil {
		return err
	}

	var body struct {
		Name     string `json:"name"`
		AvatarID string `json:"avatarId"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	if body.Name == "" {
		return badRequest("name is required")
	}

	child, err := s.deps.Accounts.CreateChild(r.Context(), p.FamilyID, body.Name, body.AvatarID, p.ID)
	if err != nil {
		return err
	}

	writeJSON(w, s.deps.Logger, http.StatusCreated, newChildDTO(child, 0, 0))
	return nil
}

func (s *Server) handleGetChild(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}

	devices, err := s.deps.Accounts.Devices(r.Context(), child.ID)
	if err != nil {
		return err
	}
	pending, err := s.deps.Activity.PendingRequestCounts(r.Context(), []uuid.UUID{child.ID})
	if err != nil {
		return err
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, newChildDTO(child, len(devices), pending[child.ID]))
	return nil
}

func (s *Server) handleUpdateChild(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}

	var body struct {
		Name                    *string `json:"name"`
		AvatarID                *string `json:"avatarId"`
		ShortsEnabled           *bool   `json:"shortsEnabled"`
		WatchPageAutoplay       *bool   `json:"watchPageAutoplay"`
		VideoSearchTiles        *bool   `json:"videoSearchTiles"`
		ChannelDiscoveryEnabled *bool   `json:"channelDiscoveryEnabled"`
		DailySearchLimit        *int    `json:"dailySearchLimit"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}

	err = s.deps.Accounts.UpdateChild(r.Context(), p.FamilyID, child.ID, store.ChildSettings{
		Name:                    body.Name,
		AvatarID:                body.AvatarID,
		ShortsEnabled:           body.ShortsEnabled,
		WatchPageAutoplay:       body.WatchPageAutoplay,
		VideoSearchTiles:        body.VideoSearchTiles,
		ChannelDiscoveryEnabled: body.ChannelDiscoveryEnabled,
		DailySearchLimit:        body.DailySearchLimit,
	}, p.ID)
	if err != nil {
		return err
	}

	updated, err := s.deps.Accounts.Child(r.Context(), p.FamilyID, child.ID)
	if err != nil {
		return err
	}
	devices, err := s.deps.Accounts.Devices(r.Context(), child.ID)
	if err != nil {
		return err
	}
	pending, err := s.deps.Activity.PendingRequestCounts(r.Context(), []uuid.UUID{child.ID})
	if err != nil {
		return err
	}
	writeJSON(w, s.deps.Logger, http.StatusOK, newChildDTO(updated, len(devices), pending[child.ID]))
	return nil
}

func (s *Server) handleDeleteChild(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	if err := p.RequireAdmin(); err != nil {
		return err
	}

	childID, err := pathUUID(r, "childId")
	if err != nil {
		return err
	}
	if err := s.deps.Accounts.DeleteChild(r.Context(), p.FamilyID, childID, p.ID); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleCreatePairingCode(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}

	code, err := auth.NewPairingCode()
	if err != nil {
		return internal(err)
	}

	expiresAt := s.deps.Now().Add(s.deps.Config.Auth.PairingCodeTTL)
	if err := s.deps.Accounts.CreatePairingCode(r.Context(), child.ID, code, expiresAt, p.ID); err != nil {
		return err
	}

	writeJSON(w, s.deps.Logger, http.StatusCreated, pairingCodeDTO{
		Code:      code,
		ExpiresAt: expiresAt,
		// The child device needs both the code and where to redeem it, so the
		// QR payload carries the server's own public URL.
		PairingURL: s.deps.Config.Server.PublicURL + "/pair?code=" + code,
	})
	return nil
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}

	devices, err := s.deps.Accounts.Devices(r.Context(), child.ID)
	if err != nil {
		return err
	}

	out := make([]deviceDTO, 0, len(devices))
	for _, device := range devices {
		out = append(out, newDeviceDTO(device))
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, out)
	return nil
}

// handleRevokeDevice kills a device token. The device's child is checked for
// scope first, so a scoped parent cannot revoke a device they cannot see.
func (s *Server) handleRevokeDevice(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	deviceID, err := pathUUID(r, "deviceId")
	if err != nil {
		return err
	}

	children, err := s.deps.Accounts.Children(r.Context(), p.FamilyID)
	if err != nil {
		return err
	}

	for _, child := range children {
		if !p.CanManage(child.ID) {
			continue
		}
		devices, err := s.deps.Accounts.Devices(r.Context(), child.ID)
		if err != nil {
			return err
		}
		for _, device := range devices {
			if device.ID == deviceID {
				if err := s.deps.Accounts.RevokeDevice(r.Context(), deviceID, p.ID); err != nil {
					return err
				}
				w.WriteHeader(http.StatusNoContent)
				return nil
			}
		}
	}
	return notFound()
}

// childInScope resolves the childId path value and enforces the caller's scope.
func (s *Server) childInScope(r *http.Request, p auth.Parent) (store.Child, error) {
	childID, err := pathUUID(r, "childId")
	if err != nil {
		return store.Child{}, err
	}
	if err := p.RequireChild(childID); err != nil {
		return store.Child{}, err
	}
	return s.deps.Accounts.Child(r.Context(), p.FamilyID, childID)
}

func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	raw := r.PathValue(name)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, badRequest("malformed " + name)
	}
	return id, nil
}

func queryInt(r *http.Request, name string, fallback int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}
