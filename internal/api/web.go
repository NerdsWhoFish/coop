package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/auth"
	"github.com/nerdswhofish/coop/internal/store"
)

const (
	webSessionCookie = "__Host-coop-child"
	webLinkSecret    = "X-Coop-Link-Secret"
	webLinkTTL       = 5 * time.Minute
	webLinkCreateMax = 20
)

func (s *Server) handleCreateWebLink(w http.ResponseWriter, r *http.Request) error {
	keys := []string{auth.HashToken("client-address:" + s.clientAddress(r))}
	if until, locked, err := s.deps.Accounts.AuthLocked(r.Context(), "web-link-create", keys); err != nil {
		return err
	} else if locked {
		return rateLimited(until.Sub(s.deps.Now()))
	}

	var body struct {
		DeviceName string `json:"deviceName"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	body.DeviceName = strings.TrimSpace(body.DeviceName)
	if body.DeviceName == "" {
		body.DeviceName = "Web browser"
	}
	if len(body.DeviceName) > 80 {
		return badRequest("deviceName must be 80 characters or fewer")
	}

	approval, err := auth.NewToken()
	if err != nil {
		return internal(err)
	}
	redemption, err := auth.NewToken()
	if err != nil {
		return internal(err)
	}
	expiresAt := s.deps.Now().Add(webLinkTTL)
	link, err := s.deps.Accounts.CreateWebDeviceLink(r.Context(), approval.Hash,
		redemption.Hash, body.DeviceName, expiresAt)
	if err != nil {
		return err
	}
	cfg := s.deps.Config.Auth
	if err := s.deps.Accounts.RecordAuthFailure(r.Context(), "web-link-create", keys,
		webLinkCreateMax, cfg.FailureWindow, cfg.LockoutDuration); err != nil {
		return err
	}

	server := url.QueryEscape(s.deps.Config.Server.PublicURL)
	qrPayload := "coop://link?server=" + server + "&id=" + link.ID.String() +
		"&approval=" + url.QueryEscape(approval.Plain)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, s.deps.Logger, http.StatusCreated, map[string]any{
		"id": link.ID, "approvalToken": approval.Plain,
		"redemptionToken": redemption.Plain, "expiresAt": expiresAt,
		"qrPayload": qrPayload,
	})
	return nil
}

func (s *Server) handleWebLinkStatus(w http.ResponseWriter, r *http.Request) error {
	id, err := pathUUID(r, "linkId")
	if err != nil {
		return err
	}
	secret := r.Header.Get(webLinkSecret)
	if secret == "" {
		return unauthorized()
	}
	link, err := s.deps.Accounts.WebDeviceLinkStatus(r.Context(), id, auth.HashToken(secret))
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, s.deps.Logger, http.StatusOK, map[string]any{
		"approved":  link.ApprovedAt != nil,
		"expiresAt": link.ExpiresAt,
	})
	return nil
}

func (s *Server) handleRedeemWebLink(w http.ResponseWriter, r *http.Request) error {
	id, err := pathUUID(r, "linkId")
	if err != nil {
		return err
	}
	secret := r.Header.Get(webLinkSecret)
	if secret == "" {
		return unauthorized()
	}
	deviceToken, err := auth.NewToken()
	if err != nil {
		return internal(err)
	}
	child, device, err := s.deps.Accounts.RedeemWebDeviceLink(r.Context(), id,
		auth.HashToken(secret), deviceToken.Hash)
	if err != nil {
		return err
	}
	s.setWebSessionCookie(w, deviceToken.Plain)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, s.deps.Logger, http.StatusOK, newChildProfileDTO(child, device.AllowSelfUnpair))
	return nil
}

func (s *Server) handleWebPair(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Code       string `json:"code"`
		DeviceName string `json:"deviceName"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	code := auth.NormalizePairingCode(body.Code)
	keys := []string{
		auth.HashToken("pairing-code:" + code),
		auth.HashToken("client-address:" + s.clientAddress(r)),
	}
	if until, locked, err := s.deps.Accounts.AuthLocked(r.Context(), "child-pair", keys); err != nil {
		return err
	} else if locked {
		return rateLimited(until.Sub(s.deps.Now()))
	}
	if code == "" {
		if err := s.recordThrottleFailure(r, "child-pair", keys); err != nil {
			return err
		}
		return badRequest("that pairing code is not valid")
	}
	body.DeviceName = strings.TrimSpace(body.DeviceName)
	if body.DeviceName == "" {
		body.DeviceName = "Web browser"
	}
	if len(body.DeviceName) > 80 {
		return badRequest("deviceName must be 80 characters or fewer")
	}
	deviceToken, err := auth.NewToken()
	if err != nil {
		return internal(err)
	}
	child, device, err := s.deps.Accounts.RedeemPairingCode(r.Context(), code,
		body.DeviceName, deviceToken.Hash)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			if failureErr := s.recordThrottleFailure(r, "child-pair", keys); failureErr != nil {
				return failureErr
			}
			return badRequest("that pairing code is not valid")
		}
		return err
	}
	if err := s.deps.Accounts.ClearAuthThrottle(r.Context(), "child-pair", keys); err != nil {
		return err
	}
	s.setWebSessionCookie(w, deviceToken.Plain)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, s.deps.Logger, http.StatusOK, newChildProfileDTO(child, device.AllowSelfUnpair))
	return nil
}

func (s *Server) handleApproveWebLinkAsChild(w http.ResponseWriter, r *http.Request,
	c auth.Child) error {
	child, err := s.deps.Accounts.Child(r.Context(), c.FamilyID, c.ID)
	if err != nil {
		return err
	}
	if !child.WebLinkingEnabled {
		return forbidden("a parent disabled computer linking for this profile")
	}
	linkID, approvalHash, err := webLinkApproval(r)
	if err != nil {
		return err
	}
	if err := s.deps.Accounts.ApproveWebDeviceLink(r.Context(), linkID, approvalHash,
		c.ID, &c.DeviceID, nil); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleApproveWebLinkAsParent(w http.ResponseWriter, r *http.Request,
	p auth.Parent) error {
	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}
	if !child.WebLinkingEnabled {
		return forbidden("computer linking is disabled for this profile")
	}
	linkID, approvalHash, err := webLinkApproval(r)
	if err != nil {
		return err
	}
	if err := s.deps.Accounts.ApproveWebDeviceLink(r.Context(), linkID, approvalHash,
		child.ID, nil, &p.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func webLinkApproval(r *http.Request) (uuid.UUID, string, error) {
	linkID, err := pathUUID(r, "linkId")
	if err != nil {
		return uuid.Nil, "", err
	}
	var body struct {
		ApprovalToken string `json:"approvalToken"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return uuid.Nil, "", err
	}
	if body.ApprovalToken == "" {
		return uuid.Nil, "", badRequest("approvalToken is required")
	}
	return linkID, auth.HashToken(body.ApprovalToken), nil
}

func (s *Server) handleWebLogout(w http.ResponseWriter, r *http.Request, c auth.Child) error {
	if err := s.deps.Accounts.RevokeOwnDevice(r.Context(), c.DeviceID); err != nil &&
		!errors.Is(err, store.ErrNotFound) {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: webSessionCookie, Value: "", Path: "/", MaxAge: -1,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) setWebSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: webSessionCookie, Value: token, Path: "/",
		MaxAge: int(s.deps.Config.Auth.ChildTokenTTL.Seconds()),
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}
