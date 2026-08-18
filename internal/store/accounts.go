package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nerdswhofish/coop/internal/domain"
)

// ErrNotFound reports a row that does not exist, or that the caller may not
// see. Handlers render it as 404 either way.
var ErrNotFound = errors.New("store: not found")

// ErrLastAdmin reports an operation that would leave a family unable to manage
// itself, which nothing could undo.
var ErrLastAdmin = errors.New("store: a family must keep at least one admin")

// ErrAlreadySetup reports that the singleton first-family slot was consumed.
var ErrAlreadySetup = errors.New("store: instance is already set up")

// Accounts holds families, parents, children and paired devices.
type Accounts struct {
	db  *DB
	now func() time.Time
}

// NewAccounts builds the repository. A nil now defaults to time.Now.
func NewAccounts(db *DB, now func() time.Time) *Accounts {
	if now == nil {
		now = time.Now
	}
	return &Accounts{db: db, now: now}
}

func wrap(err error, what string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

// normalizeEmail folds case so the unique index cannot be sidestepped by
// signing up as the same address in different capitalisation.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// CreateFamily creates a family and its first admin in one transaction, so a
// half-built family with no way to sign in is impossible.
func (a *Accounts) CreateFamily(ctx context.Context, familyName, timezone,
	email, passwordHash string) (Family, Parent, error) {

	family := Family{Name: familyName, Timezone: timezone}
	parent := Parent{
		Email:        normalizeEmail(email),
		PasswordHash: passwordHash,
		Role:         domain.RoleAdmin,
	}

	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&family).Error; err != nil {
			return fmt.Errorf("creating family: %w", err)
		}
		parent.FamilyID = family.ID
		if err := tx.Create(&parent).Error; err != nil {
			return fmt.Errorf("creating admin parent: %w", err)
		}
		return nil
	})
	if err != nil {
		return Family{}, Parent{}, err
	}
	return family, parent, nil
}

// CreateInitialFamily serializes the public first-run path and rechecks the
// singleton invariant inside the transaction. The advisory lock closes the
// race between the setup status check and family creation.
func (a *Accounts) CreateInitialFamily(ctx context.Context, familyName, timezone,
	email, passwordHash string) (Family, Parent, error) {

	family := Family{Name: familyName, Timezone: timezone}
	parent := Parent{
		Email:        normalizeEmail(email),
		PasswordHash: passwordHash,
		Role:         domain.RoleAdmin,
	}

	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		const setupLockID int64 = 0x434f4f50
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", setupLockID).Error; err != nil {
			return fmt.Errorf("locking initial setup: %w", err)
		}
		var count int64
		if err := tx.Model(&Family{}).Count(&count).Error; err != nil {
			return fmt.Errorf("checking initial setup: %w", err)
		}
		if count != 0 {
			return ErrAlreadySetup
		}
		if err := tx.Create(&family).Error; err != nil {
			return fmt.Errorf("creating family: %w", err)
		}
		parent.FamilyID = family.ID
		if err := tx.Create(&parent).Error; err != nil {
			return fmt.Errorf("creating admin parent: %w", err)
		}
		return nil
	})
	if err != nil {
		return Family{}, Parent{}, err
	}
	return family, parent, nil
}

// FamilyCount reports how many families exist, which first-run setup uses to
// decide whether to offer registration at all.
func (a *Accounts) FamilyCount(ctx context.Context) (int64, error) {
	var n int64
	err := a.db.WithContext(ctx).Model(&Family{}).Count(&n).Error
	return n, wrap(err, "counting families")
}

// Family reads one family.
func (a *Accounts) Family(ctx context.Context, id uuid.UUID) (Family, error) {
	var family Family
	err := a.db.WithContext(ctx).First(&family, "id = ?", id).Error
	return family, wrap(err, "reading family")
}

// DeleteFamily permanently removes a family and all tenant-owned data through
// the database's cascading foreign keys. It intentionally does not retain an
// audit event because the deletion request includes the audit history itself.
func (a *Accounts) DeleteFamily(ctx context.Context, id uuid.UUID) error {
	result := a.db.WithContext(ctx).Delete(&Family{}, "id = ?", id)
	if result.Error != nil {
		return fmt.Errorf("deleting family: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// FamiliesWithAPIKeys lists the tenants that can perform YouTube work.
// Instances normally contain one family, but keeping the worker tenant-aware
// costs nothing and preserves the isolation already enforced by the ledger.
func (a *Accounts) FamiliesWithAPIKeys(ctx context.Context) ([]Family, error) {
	var families []Family
	err := a.db.WithContext(ctx).
		Where("encrypted_api_key IS NOT NULL AND octet_length(encrypted_api_key) > 0").
		Order("created_at").
		Find(&families).Error
	return families, wrap(err, "listing families with api keys")
}

// UpdateFamily changes a family's display settings.
func (a *Accounts) UpdateFamily(ctx context.Context, id uuid.UUID, name, timezone string,
	actorID uuid.UUID) error {
	updates := map[string]any{"updated_at": a.now()}
	if name != "" {
		updates["name"] = name
	}
	if timezone != "" {
		updates["timezone"] = timezone
	}
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before Family
		if err := tx.First(&before, "id = ?", id).Error; err != nil {
			return wrap(err, "reading family before update")
		}
		if err := tx.Model(&Family{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return fmt.Errorf("updating family: %w", err)
		}
		var after Family
		if err := tx.First(&after, "id = ?", id).Error; err != nil {
			return fmt.Errorf("reading family after update: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: id, ActorParentID: &actorID, Action: "family.update",
			TargetType: "family", TargetID: id.String(), Before: familyAuditState(before),
			After: familyAuditState(after), CreatedAt: a.now(),
		})
	})
}

// SetAPIKey stores an already-encrypted YouTube API key.
func (a *Accounts) SetAPIKey(ctx context.Context, familyID uuid.UUID, sealed []byte,
	actorID uuid.UUID) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var family Family
		if err := tx.First(&family, "id = ?", familyID).Error; err != nil {
			return wrap(err, "reading family before API key update")
		}
		before := map[string]any{"configured": len(family.EncryptedAPIKey) > 0}
		if err := tx.Model(&Family{}).Where("id = ?", familyID).
			Updates(map[string]any{"encrypted_api_key": sealed, "updated_at": a.now()}).Error; err != nil {
			return fmt.Errorf("storing api key: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: familyID, ActorParentID: &actorID, Action: "family.api_key_replace",
			TargetType: "family", TargetID: familyID.String(), Before: before,
			After: map[string]any{"configured": true}, CreatedAt: a.now(),
		})
	})
}

func familyAuditState(family Family) map[string]any {
	return map[string]any{"name": family.Name, "timezone": family.Timezone}
}

// ParentByEmail looks up a parent for sign-in.
func (a *Accounts) ParentByEmail(ctx context.Context, email string) (Parent, error) {
	var parent Parent
	err := a.db.WithContext(ctx).First(&parent, "email = ?", normalizeEmail(email)).Error
	return parent, wrap(err, "reading parent by email")
}

// ResetParentTOTP clears a parent's authenticator enrollment and revokes every
// session and in-flight challenge. This is deliberately exposed only through
// the host CLI, never the HTTP API.
func (a *Accounts) ResetParentTOTP(ctx context.Context, email string) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var parent Parent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&parent, "email = ?", normalizeEmail(email)).Error; err != nil {
			return wrap(err, "reading parent for TOTP reset")
		}
		if err := tx.Model(&parent).Updates(map[string]any{
			"encrypted_totp_secret": nil,
			"totp_last_used_step":   nil,
			"updated_at":            a.now(),
		}).Error; err != nil {
			return fmt.Errorf("resetting parent TOTP: %w", err)
		}
		if err := tx.Where("parent_id = ?", parent.ID).Delete(&ParentSession{}).Error; err != nil {
			return fmt.Errorf("revoking parent sessions after TOTP reset: %w", err)
		}
		if err := tx.Where("parent_id = ?", parent.ID).Delete(&ParentAuthChallenge{}).Error; err != nil {
			return fmt.Errorf("invalidating parent challenges after TOTP reset: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: parent.FamilyID, Action: "parent.totp_reset", TargetType: "parent",
			TargetID: parent.ID.String(), Before: map[string]any{"enrolled": len(parent.EncryptedTOTPSecret) > 0},
			After: map[string]any{"enrolled": false}, CreatedAt: a.now(),
		})
	})
}

// UnlockParentLogin removes the email-specific login throttle while leaving
// address-wide throttles intact, so recovering one account does not unblock a
// hostile source address for every account.
func (a *Accounts) UnlockParentLogin(ctx context.Context, email, emailKeyHash string) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var parent Parent
		if err := tx.First(&parent, "email = ?", normalizeEmail(email)).Error; err != nil {
			return wrap(err, "reading parent for login unlock")
		}
		result := tx.Delete(&AuthThrottle{}, "action = ? AND key_hash = ?", "parent-login", emailKeyHash)
		if result.Error != nil {
			return fmt.Errorf("unlocking parent login: %w", result.Error)
		}
		return appendAudit(tx, auditChange{
			FamilyID: parent.FamilyID, Action: "parent.login_unlock", TargetType: "parent",
			TargetID: parent.ID.String(), Before: map[string]any{"locked": result.RowsAffected > 0},
			After: map[string]any{"locked": false}, CreatedAt: a.now(),
		})
	})
}

// Parent reads one parent.
func (a *Accounts) Parent(ctx context.Context, id uuid.UUID) (Parent, error) {
	var parent Parent
	err := a.db.WithContext(ctx).First(&parent, "id = ?", id).Error
	return parent, wrap(err, "reading parent")
}

// Parents lists a family's parents.
func (a *Accounts) Parents(ctx context.Context, familyID uuid.UUID) ([]Parent, error) {
	var parents []Parent
	err := a.db.WithContext(ctx).
		Where("family_id = ?", familyID).
		Order("created_at").
		Find(&parents).Error
	return parents, wrap(err, "listing parents")
}

// CreateParent adds a parent and their child scope in one transaction.
func (a *Accounts) CreateParent(ctx context.Context, familyID uuid.UUID, email,
	passwordHash string, role domain.ParentRole, childIDs []uuid.UUID) (Parent, error) {

	parent := Parent{
		FamilyID:     familyID,
		Email:        normalizeEmail(email),
		PasswordHash: passwordHash,
		Role:         role,
	}

	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&parent).Error; err != nil {
			return fmt.Errorf("creating parent: %w", err)
		}
		return setScopeTx(tx, parent.ID, childIDs)
	})
	if err != nil {
		return Parent{}, err
	}
	return parent, nil
}

// CreateParentInvitation records a single-use credential and the scope that
// will be granted when it is redeemed.
func (a *Accounts) CreateParentInvitation(ctx context.Context, invitation ParentInvitation,
	childIDs []uuid.UUID) (ParentInvitation, error) {

	invitation.Email = normalizeEmail(invitation.Email)
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&invitation).Error; err != nil {
			return fmt.Errorf("creating parent invitation: %w", err)
		}
		if len(childIDs) == 0 {
			return appendAudit(tx, auditChange{
				FamilyID: invitation.FamilyID, ActorParentID: &invitation.CreatedBy,
				Action: "parent.invitation_create", TargetType: "invitation",
				TargetID: invitation.ID.String(), Before: map[string]any{},
				After:     map[string]any{"email": invitation.Email, "role": invitation.Role, "childIds": childIDs},
				CreatedAt: a.now(),
			})
		}

		scopes := make([]ParentInvitationScope, len(childIDs))
		for i, childID := range childIDs {
			scopes[i] = ParentInvitationScope{InvitationID: invitation.ID, ChildID: childID}
		}
		if err := tx.Create(&scopes).Error; err != nil {
			return fmt.Errorf("writing parent invitation scope: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: invitation.FamilyID, ActorParentID: &invitation.CreatedBy,
			Action: "parent.invitation_create", TargetType: "invitation",
			TargetID: invitation.ID.String(), Before: map[string]any{},
			After:     map[string]any{"email": invitation.Email, "role": invitation.Role, "childIds": childIDs},
			CreatedAt: a.now(),
		})
	})
	if err != nil {
		return ParentInvitation{}, err
	}
	return invitation, nil
}

// RedeemParentInvitation consumes an invitation and creates the parent and
// their scope in one transaction. The row lock makes concurrent redemption
// attempts resolve to one winner.
func (a *Accounts) RedeemParentInvitation(ctx context.Context, tokenHash,
	passwordHash string) (Parent, error) {

	var parent Parent
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var invitation ParentInvitation
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&invitation, "token_hash = ? AND used_at IS NULL AND expires_at > ?", tokenHash, a.now()).Error
		if err != nil {
			return wrap(err, "reading parent invitation")
		}

		var scopes []ParentInvitationScope
		if err := tx.Where("invitation_id = ?", invitation.ID).Find(&scopes).Error; err != nil {
			return fmt.Errorf("reading parent invitation scope: %w", err)
		}
		childIDs := make([]uuid.UUID, len(scopes))
		for i, scope := range scopes {
			childIDs[i] = scope.ChildID
		}

		parent = Parent{
			FamilyID:     invitation.FamilyID,
			Email:        invitation.Email,
			PasswordHash: passwordHash,
			Role:         invitation.Role,
		}
		if err := tx.Create(&parent).Error; err != nil {
			return fmt.Errorf("creating invited parent: %w", err)
		}
		if invitation.Role == domain.RoleParent {
			if err := setScopeTx(tx, parent.ID, childIDs); err != nil {
				return err
			}
		}

		usedAt := a.now()
		if err := tx.Model(&invitation).Update("used_at", usedAt).Error; err != nil {
			return fmt.Errorf("consuming parent invitation: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: parent.FamilyID, ActorParentID: &parent.ID,
			Action: "parent.invitation_redeem", TargetType: "parent", TargetID: parent.ID.String(),
			Before: map[string]any{}, After: map[string]any{"email": parent.Email, "role": parent.Role},
			CreatedAt: usedAt,
		})
	})
	if err != nil {
		return Parent{}, err
	}
	return parent, nil
}

// DeleteParent removes a parent, refusing to strand a family without an admin.
func (a *Accounts) DeleteParent(ctx context.Context, familyID, parentID, actorID uuid.UUID) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var parent Parent
		if err := tx.First(&parent, "id = ? AND family_id = ?", parentID, familyID).Error; err != nil {
			return wrap(err, "reading parent")
		}

		if parent.Role == domain.RoleAdmin {
			var admins int64
			if err := tx.Model(&Parent{}).
				Where("family_id = ? AND role = ?", familyID, domain.RoleAdmin).
				Count(&admins).Error; err != nil {
				return fmt.Errorf("counting admins: %w", err)
			}
			if admins <= 1 {
				return ErrLastAdmin
			}
		}
		if err := appendAudit(tx, auditChange{
			FamilyID: familyID, ActorParentID: &actorID,
			Action: "parent.delete", TargetType: "parent", TargetID: parentID.String(),
			Before: map[string]any{"email": parent.Email, "role": parent.Role},
			After:  map[string]any{}, CreatedAt: a.now(),
		}); err != nil {
			return err
		}
		return wrap(tx.Delete(&Parent{}, "id = ?", parentID).Error, "deleting parent")
	})
}

// ScopedChildIDs lists the children a parent may act on. Admins are unscoped
// and callers must not consult this for them.
func (a *Accounts) ScopedChildIDs(ctx context.Context, parentID uuid.UUID) ([]uuid.UUID, error) {
	var scopes []ParentScope
	if err := a.db.WithContext(ctx).
		Where("parent_id = ?", parentID).Find(&scopes).Error; err != nil {
		return nil, fmt.Errorf("reading parent scope: %w", err)
	}

	ids := make([]uuid.UUID, len(scopes))
	for i, s := range scopes {
		ids[i] = s.ChildID
	}
	return ids, nil
}

// SetScope replaces which children a parent may act on.
func (a *Accounts) SetScope(ctx context.Context, parentID uuid.UUID, childIDs []uuid.UUID,
	actorID uuid.UUID) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var parent Parent
		if err := tx.First(&parent, "id = ?", parentID).Error; err != nil {
			return wrap(err, "reading scoped parent")
		}
		var before []uuid.UUID
		if err := tx.Model(&ParentScope{}).Where("parent_id = ?", parentID).
			Pluck("child_id", &before).Error; err != nil {
			return fmt.Errorf("reading parent scope: %w", err)
		}
		if err := setScopeTx(tx, parentID, childIDs); err != nil {
			return err
		}
		return appendAudit(tx, auditChange{
			FamilyID: parent.FamilyID, ActorParentID: &actorID,
			Action: "parent.scope_update", TargetType: "parent", TargetID: parentID.String(),
			Before: map[string]any{"childIds": before}, After: map[string]any{"childIds": childIDs},
			CreatedAt: a.now(),
		})
	})
}

func setScopeTx(tx *gorm.DB, parentID uuid.UUID, childIDs []uuid.UUID) error {
	if err := tx.Delete(&ParentScope{}, "parent_id = ?", parentID).Error; err != nil {
		return fmt.Errorf("clearing parent scope: %w", err)
	}
	if len(childIDs) == 0 {
		return nil
	}

	scopes := make([]ParentScope, len(childIDs))
	for i, id := range childIDs {
		scopes[i] = ParentScope{ParentID: parentID, ChildID: id}
	}
	if err := tx.Create(&scopes).Error; err != nil {
		return fmt.Errorf("writing parent scope: %w", err)
	}
	return nil
}

// CreateSession records a signed-in parent device.
func (a *Accounts) CreateSession(ctx context.Context, parentID uuid.UUID,
	tokenHash string, expiresAt time.Time) (ParentSession, error) {

	session := ParentSession{ParentID: parentID, TokenHash: tokenHash, ExpiresAt: expiresAt}
	err := a.db.WithContext(ctx).Create(&session).Error
	return session, wrap(err, "creating session")
}

// SessionByToken resolves a session token to its parent, rejecting expired
// sessions as though they never existed.
func (a *Accounts) SessionByToken(ctx context.Context, tokenHash string) (ParentSession, Parent, error) {
	var session ParentSession
	err := a.db.WithContext(ctx).
		First(&session, "token_hash = ? AND expires_at > ?", tokenHash, a.now()).Error
	if err != nil {
		return ParentSession{}, Parent{}, wrap(err, "reading session")
	}

	var parent Parent
	if err := a.db.WithContext(ctx).First(&parent, "id = ?", session.ParentID).Error; err != nil {
		return ParentSession{}, Parent{}, wrap(err, "reading session parent")
	}
	return session, parent, nil
}

// ClientApp is what a caller reports it is running. Empty when the client
// predates version reporting, which is exactly what a migration needs to see.
type ClientApp struct {
	Build   string
	Version string
}

// Reported says whether the client named a build.
func (c ClientApp) Reported() bool { return c.Build != "" }

// touch builds the column set for a last-seen update. A client that reports
// nothing must not erase a build another client already recorded for the row.
func (c ClientApp) touch(now time.Time) map[string]any {
	columns := map[string]any{"last_seen_at": now}
	if c.Reported() {
		columns["app_build"] = c.Build
		columns["app_version"] = c.Version
	}
	return columns
}

// TouchSession records that a session was used, best effort.
func (a *Accounts) TouchSession(ctx context.Context, sessionID uuid.UUID, client ClientApp) error {
	return wrap(a.db.WithContext(ctx).Model(&ParentSession{}).
		Where("id = ?", sessionID).
		Updates(client.touch(a.now())).Error, "touching session")
}

// DeleteSession signs one device out.
func (a *Accounts) DeleteSession(ctx context.Context, sessionID uuid.UUID) error {
	return wrap(a.db.WithContext(ctx).
		Delete(&ParentSession{}, "id = ?", sessionID).Error, "deleting session")
}

// PurgeExpiredSessions drops sessions past their expiry.
func (a *Accounts) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	result := a.db.WithContext(ctx).
		Where("expires_at <= ?", a.now()).
		Delete(&ParentSession{})
	if result.Error != nil {
		return 0, fmt.Errorf("purging sessions: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// PurgeParentInvitations drops credentials that are expired or already used.
func (a *Accounts) PurgeParentInvitations(ctx context.Context) (int64, error) {
	result := a.db.WithContext(ctx).
		Where("expires_at <= ? OR used_at IS NOT NULL", a.now()).
		Delete(&ParentInvitation{})
	return result.RowsAffected, wrap(result.Error, "purging parent invitations")
}

// CreateChild adds a viewing profile.
func (a *Accounts) CreateChild(ctx context.Context, familyID uuid.UUID, name, avatarID string,
	actorID uuid.UUID) (Child, error) {
	child := Child{
		FamilyID:                familyID,
		Name:                    name,
		AvatarID:                avatarID,
		ShortsEnabled:           true,
		WatchPageAutoplay:       false,
		VideoSearchTiles:        true,
		ChannelDiscoveryEnabled: false,
	}
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&child).Error; err != nil {
			return fmt.Errorf("creating child: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: familyID, ActorParentID: &actorID, ChildID: &child.ID,
			Action: "child.create", TargetType: "child", TargetID: child.ID.String(),
			Before: map[string]any{}, After: childAuditState(child), CreatedAt: a.now(),
		})
	})
	return child, err
}

// Child reads one child within a family.
func (a *Accounts) Child(ctx context.Context, familyID, childID uuid.UUID) (Child, error) {
	var child Child
	err := a.db.WithContext(ctx).
		First(&child, "id = ? AND family_id = ?", childID, familyID).Error
	return child, wrap(err, "reading child")
}

// Children lists a family's children.
func (a *Accounts) Children(ctx context.Context, familyID uuid.UUID) ([]Child, error) {
	var children []Child
	err := a.db.WithContext(ctx).
		Where("family_id = ?", familyID).
		Order("created_at").
		Find(&children).Error
	return children, wrap(err, "listing children")
}

// ChildSettings is the mutable part of a child profile. Nil fields are left
// unchanged, which is what lets the parent app PATCH one toggle at a time.
type ChildSettings struct {
	Name                    *string
	AvatarID                *string
	ShortsEnabled           *bool
	WatchPageAutoplay       *bool
	VideoSearchTiles        *bool
	ChannelDiscoveryEnabled *bool
	WebLinkingEnabled       *bool
	DailySearchLimit        *int
}

// UpdateChild applies the settings that were supplied.
func (a *Accounts) UpdateChild(ctx context.Context, familyID, childID uuid.UUID,
	settings ChildSettings, actorID uuid.UUID) error {

	updates := map[string]any{"updated_at": a.now()}
	if settings.Name != nil {
		updates["name"] = *settings.Name
	}
	if settings.AvatarID != nil {
		updates["avatar_id"] = *settings.AvatarID
	}
	if settings.ShortsEnabled != nil {
		updates["shorts_enabled"] = *settings.ShortsEnabled
	}
	if settings.WatchPageAutoplay != nil {
		updates["watch_page_autoplay"] = *settings.WatchPageAutoplay
	}
	if settings.VideoSearchTiles != nil {
		updates["video_search_tiles"] = *settings.VideoSearchTiles
	}
	if settings.ChannelDiscoveryEnabled != nil {
		updates["channel_discovery_enabled"] = *settings.ChannelDiscoveryEnabled
	}
	if settings.WebLinkingEnabled != nil {
		updates["web_linking_enabled"] = *settings.WebLinkingEnabled
	}
	if settings.DailySearchLimit != nil {
		updates["daily_search_limit"] = *settings.DailySearchLimit
	}

	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before Child
		if err := tx.First(&before, "id = ? AND family_id = ?", childID, familyID).Error; err != nil {
			return wrap(err, "reading child before update")
		}
		result := tx.Model(&Child{}).Where("id = ? AND family_id = ?", childID, familyID).Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("updating child: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		var after Child
		if err := tx.First(&after, "id = ?", childID).Error; err != nil {
			return fmt.Errorf("reading child after update: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: familyID, ActorParentID: &actorID, ChildID: &childID,
			Action: "child.update", TargetType: "child", TargetID: childID.String(),
			Before: childAuditState(before), After: childAuditState(after), CreatedAt: a.now(),
		})
	})
}

// DeleteChild removes a child and everything belonging to them.
func (a *Accounts) DeleteChild(ctx context.Context, familyID, childID, actorID uuid.UUID) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before Child
		if err := tx.First(&before, "id = ? AND family_id = ?", childID, familyID).Error; err != nil {
			return wrap(err, "reading child before delete")
		}
		if err := appendAudit(tx, auditChange{
			FamilyID: familyID, ActorParentID: &actorID, ChildID: &childID,
			Action: "child.delete", TargetType: "child", TargetID: childID.String(),
			Before: childAuditState(before), After: map[string]any{}, CreatedAt: a.now(),
		}); err != nil {
			return err
		}
		result := tx.Delete(&Child{}, "id = ? AND family_id = ?", childID, familyID)
		if result.Error != nil {
			return fmt.Errorf("deleting child: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func childAuditState(child Child) map[string]any {
	return map[string]any{
		"name": child.Name, "avatarId": child.AvatarID,
		"shortsEnabled": child.ShortsEnabled, "watchPageAutoplay": child.WatchPageAutoplay,
		"videoSearchTiles":        child.VideoSearchTiles,
		"channelDiscoveryEnabled": child.ChannelDiscoveryEnabled,
		"webLinkingEnabled":       child.WebLinkingEnabled,
		"dailySearchLimit":        child.DailySearchLimit,
	}
}

// CreatePairingCode mints a single-use code bound to a child.
func (a *Accounts) CreatePairingCode(ctx context.Context, childID uuid.UUID,
	code string, expiresAt time.Time, actorID uuid.UUID) error {

	row := PairingCode{Code: code, ChildID: childID, ExpiresAt: expiresAt}
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		familyID, err := familyIDForChild(tx, childID)
		if err != nil {
			return err
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("creating pairing code: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: familyID, ActorParentID: &actorID, ChildID: &childID,
			Action: "device.pairing_code_create", TargetType: "child", TargetID: childID.String(),
			Before: map[string]any{}, After: map[string]any{"expiresAt": expiresAt},
			CreatedAt: a.now(),
		})
	})
}

// RedeemPairingCode consumes a code and registers a device in one transaction.
// The code is claimed with a conditional update, so two devices racing on the
// same code cannot both succeed.
func (a *Accounts) RedeemPairingCode(ctx context.Context, code, deviceName,
	tokenHash string) (Child, ChildDevice, error) {

	var child Child
	var device ChildDevice

	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()

		claim := tx.Model(&PairingCode{}).
			Where("code = ? AND used_at IS NULL AND expires_at > ?", code, now).
			Update("used_at", now)
		if claim.Error != nil {
			return fmt.Errorf("claiming pairing code: %w", claim.Error)
		}
		if claim.RowsAffected == 0 {
			return ErrNotFound
		}

		var row PairingCode
		if err := tx.First(&row, "code = ?", code).Error; err != nil {
			return wrap(err, "reading pairing code")
		}
		if err := tx.First(&child, "id = ?", row.ChildID).Error; err != nil {
			return wrap(err, "reading child")
		}

		device = ChildDevice{ChildID: child.ID, Name: deviceName, TokenHash: tokenHash}
		if err := tx.Create(&device).Error; err != nil {
			return fmt.Errorf("registering device: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: child.FamilyID, ChildID: &child.ID,
			Action: "device.pair", TargetType: "device", TargetID: device.ID.String(),
			Before: map[string]any{}, After: map[string]any{"name": device.Name},
			CreatedAt: now,
		})
	})
	if err != nil {
		return Child{}, ChildDevice{}, err
	}
	return child, device, nil
}

// CreateWebDeviceLink stores the two halves of a browser handoff.
func (a *Accounts) CreateWebDeviceLink(ctx context.Context, approvalHash, redemptionHash,
	deviceName string, expiresAt time.Time) (WebDeviceLink, error) {

	link := WebDeviceLink{
		ApprovalTokenHash:   approvalHash,
		RedemptionTokenHash: redemptionHash,
		DeviceName:          deviceName,
		ExpiresAt:           expiresAt,
	}
	if err := a.db.WithContext(ctx).Create(&link).Error; err != nil {
		return WebDeviceLink{}, fmt.Errorf("creating web device link: %w", err)
	}
	return link, nil
}

// WebDeviceLinkStatus resolves the browser-only half of a link without
// exposing which child approved it.
func (a *Accounts) WebDeviceLinkStatus(ctx context.Context, id uuid.UUID,
	redemptionHash string) (WebDeviceLink, error) {

	var link WebDeviceLink
	err := a.db.WithContext(ctx).First(&link,
		"id = ? AND redemption_token_hash = ? AND expires_at > ? AND redeemed_at IS NULL",
		id, redemptionHash, a.now()).Error
	return link, wrap(err, "reading web device link")
}

// ApproveWebDeviceLink binds an unclaimed browser handoff to a child.
func (a *Accounts) ApproveWebDeviceLink(ctx context.Context, id uuid.UUID, approvalHash string,
	childID uuid.UUID, deviceID, parentID *uuid.UUID) error {

	now := a.now()
	updates := map[string]any{
		"child_id": childID, "approved_at": now,
		"approved_by_device_id": deviceID, "approved_by_parent_id": parentID,
	}
	result := a.db.WithContext(ctx).Model(&WebDeviceLink{}).
		Where("id = ? AND approval_token_hash = ? AND expires_at > ? AND approved_at IS NULL AND redeemed_at IS NULL",
			id, approvalHash, now).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("approving web device link: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// RedeemWebDeviceLink consumes an approved handoff and registers the browser
// as an ordinary revocable child device in the same transaction.
func (a *Accounts) RedeemWebDeviceLink(ctx context.Context, id uuid.UUID, redemptionHash,
	tokenHash string) (Child, ChildDevice, error) {

	var child Child
	var device ChildDevice
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		var link WebDeviceLink
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&link,
			"id = ? AND redemption_token_hash = ? AND expires_at > ? AND approved_at IS NOT NULL AND redeemed_at IS NULL",
			id, redemptionHash, now).Error; err != nil {
			return wrap(err, "claiming web device link")
		}
		if link.ChildID == nil {
			return ErrNotFound
		}
		if err := tx.First(&child, "id = ?", *link.ChildID).Error; err != nil {
			return wrap(err, "reading linked child")
		}
		if !child.WebLinkingEnabled {
			return ErrNotFound
		}
		device = ChildDevice{ChildID: child.ID, Name: link.DeviceName, TokenHash: tokenHash}
		if err := tx.Create(&device).Error; err != nil {
			return fmt.Errorf("registering browser device: %w", err)
		}
		if err := tx.Model(&link).Update("redeemed_at", now).Error; err != nil {
			return fmt.Errorf("consuming web device link: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: child.FamilyID, ActorParentID: link.ApprovedByParentID, ChildID: &child.ID,
			Action: "device.web_pair", TargetType: "device", TargetID: device.ID.String(),
			Before: map[string]any{}, After: map[string]any{"name": device.Name}, CreatedAt: now,
		})
	})
	if err != nil {
		return Child{}, ChildDevice{}, err
	}
	return child, device, nil
}

// RevokeOwnDevice lets a cookie-backed browser explicitly end its own session.
func (a *Accounts) RevokeOwnDevice(ctx context.Context, deviceID uuid.UUID) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var device ChildDevice
		if err := tx.First(&device, "id = ? AND revoked_at IS NULL", deviceID).Error; err != nil {
			return wrap(err, "reading own device before revocation")
		}
		familyID, err := familyIDForChild(tx, device.ChildID)
		if err != nil {
			return err
		}
		now := a.now()
		if err := tx.Model(&device).Update("revoked_at", now).Error; err != nil {
			return fmt.Errorf("revoking own device: %w", err)
		}
		if err := tx.Model(&PlaybackSession{}).Where("device_id = ?", deviceID).
			Updates(map[string]any{"active": false, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("stopping own device playback: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: familyID, ChildID: &device.ChildID,
			Action: "device.self_revoke", TargetType: "device", TargetID: device.ID.String(),
			Before: map[string]any{"active": true, "name": device.Name},
			After:  map[string]any{"active": false, "name": device.Name}, CreatedAt: now,
		})
	})
}

// DeviceByToken resolves a device token to its device and child, rejecting
// revoked devices.
func (a *Accounts) DeviceByToken(ctx context.Context, tokenHash string) (ChildDevice, Child, error) {
	var device ChildDevice
	err := a.db.WithContext(ctx).
		First(&device, "token_hash = ? AND revoked_at IS NULL", tokenHash).Error
	if err != nil {
		return ChildDevice{}, Child{}, wrap(err, "reading device")
	}

	var child Child
	if err := a.db.WithContext(ctx).First(&child, "id = ?", device.ChildID).Error; err != nil {
		return ChildDevice{}, Child{}, wrap(err, "reading device child")
	}
	return device, child, nil
}

// TouchDevice records that a device was seen, best effort.
func (a *Accounts) TouchDevice(ctx context.Context, deviceID uuid.UUID, client ClientApp) error {
	return wrap(a.db.WithContext(ctx).Model(&ChildDevice{}).
		Where("id = ?", deviceID).
		Updates(client.touch(a.now())).Error, "touching device")
}

// FamilyDevice is one installed client: a signed-in parent session or a paired
// child device, flattened so a parent can see every build in the family.
type FamilyDevice struct {
	ID         uuid.UUID
	Audience   string
	Owner      string
	Name       string
	AppBuild   string
	AppVersion string
	LastSeenAt *time.Time
	CreatedAt  time.Time
}

// FamilyDevices lists every live client in a family, most recently seen first.
// Parent rows are sessions rather than devices: signing in twice on one phone
// is two sessions, and there is nothing else to tell them apart by.
func (a *Accounts) FamilyDevices(ctx context.Context, familyID uuid.UUID) ([]FamilyDevice, error) {
	var sessions []FamilyDevice
	err := a.db.WithContext(ctx).Model(&ParentSession{}).
		Select(`parent_session.id, 'parent' AS audience, parent.email AS owner, '' AS name,
			parent_session.app_build, parent_session.app_version,
			parent_session.last_seen_at, parent_session.created_at`).
		Joins("JOIN parent ON parent.id = parent_session.parent_id").
		Where("parent.family_id = ? AND parent_session.expires_at > ?", familyID, a.now()).
		Scan(&sessions).Error
	if err != nil {
		return nil, wrap(err, "listing parent sessions")
	}

	var devices []FamilyDevice
	err = a.db.WithContext(ctx).Model(&ChildDevice{}).
		Select(`child_device.id, 'child' AS audience, child.name AS owner, child_device.name,
			child_device.app_build, child_device.app_version,
			child_device.last_seen_at, child_device.created_at`).
		Joins("JOIN child ON child.id = child_device.child_id").
		Where("child.family_id = ? AND child_device.revoked_at IS NULL", familyID).
		Scan(&devices).Error
	if err != nil {
		return nil, wrap(err, "listing child devices")
	}

	all := append(sessions, devices...)
	slices.SortFunc(all, func(x, y FamilyDevice) int {
		return compareLastSeen(y, x)
	})
	return all, nil
}

// compareLastSeen orders by last contact, treating a client that has never
// been seen as older than any that has.
func compareLastSeen(x, y FamilyDevice) int {
	switch {
	case x.LastSeenAt == nil && y.LastSeenAt == nil:
		return x.CreatedAt.Compare(y.CreatedAt)
	case x.LastSeenAt == nil:
		return -1
	case y.LastSeenAt == nil:
		return 1
	default:
		return x.LastSeenAt.Compare(*y.LastSeenAt)
	}
}

// Devices lists a child's paired devices, most recent first.
func (a *Accounts) Devices(ctx context.Context, childID uuid.UUID) ([]ChildDevice, error) {
	var devices []ChildDevice
	err := a.db.WithContext(ctx).
		Where("child_id = ? AND revoked_at IS NULL", childID).
		Order("created_at DESC").
		Find(&devices).Error
	return devices, wrap(err, "listing devices")
}

// Device returns one active paired device.
func (a *Accounts) Device(ctx context.Context, deviceID uuid.UUID) (ChildDevice, error) {
	var device ChildDevice
	err := a.db.WithContext(ctx).First(&device, "id = ? AND revoked_at IS NULL", deviceID).Error
	return device, wrap(err, "reading device")
}

// SetDeviceSelfUnpair controls whether a child can clear this device's pairing.
func (a *Accounts) SetDeviceSelfUnpair(ctx context.Context, deviceID, actorID uuid.UUID, allowed bool) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var device ChildDevice
		if err := tx.First(&device, "id = ? AND revoked_at IS NULL", deviceID).Error; err != nil {
			return wrap(err, "reading device before update")
		}
		familyID, err := familyIDForChild(tx, device.ChildID)
		if err != nil {
			return err
		}
		if err := tx.Model(&device).Update("allow_self_unpair", allowed).Error; err != nil {
			return fmt.Errorf("updating device self-unpair permission: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: familyID, ActorParentID: &actorID, ChildID: &device.ChildID,
			Action: "device.self_unpair.update", TargetType: "device", TargetID: device.ID.String(),
			Before: map[string]any{"allowed": device.AllowSelfUnpair},
			After:  map[string]any{"allowed": allowed}, CreatedAt: a.now(),
		})
	})
}

// RevokeDevice marks a device's token dead. The row is kept so the parent app
// can still show that the device once existed.
func (a *Accounts) RevokeDevice(ctx context.Context, deviceID, actorID uuid.UUID) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var device ChildDevice
		if err := tx.First(&device, "id = ? AND revoked_at IS NULL", deviceID).Error; err != nil {
			return wrap(err, "reading device before revocation")
		}
		familyID, err := familyIDForChild(tx, device.ChildID)
		if err != nil {
			return err
		}
		now := a.now()
		if err := tx.Model(&device).Update("revoked_at", now).Error; err != nil {
			return fmt.Errorf("revoking device: %w", err)
		}
		if err := tx.Model(&PlaybackSession{}).Where("device_id = ?", device.ID).
			Updates(map[string]any{"active": false, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("stopping revoked device playback: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: familyID, ActorParentID: &actorID, ChildID: &device.ChildID,
			Action: "device.revoke", TargetType: "device", TargetID: device.ID.String(),
			Before: map[string]any{"active": true, "name": device.Name},
			After:  map[string]any{"active": false, "name": device.Name}, CreatedAt: now,
		})
	})
}

// PurgeExpiredPairingCodes drops codes that were never redeemed.
func (a *Accounts) PurgeExpiredPairingCodes(ctx context.Context) (int64, error) {
	result := a.db.WithContext(ctx).
		Where("expires_at <= ? OR used_at IS NOT NULL", a.now()).
		Delete(&PairingCode{})
	if result.Error != nil {
		return 0, fmt.Errorf("purging pairing codes: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// PurgeExpiredWebDeviceLinks removes consumed and expired browser handoffs.
func (a *Accounts) PurgeExpiredWebDeviceLinks(ctx context.Context) (int64, error) {
	result := a.db.WithContext(ctx).
		Where("expires_at <= ? OR redeemed_at IS NOT NULL", a.now()).
		Delete(&WebDeviceLink{})
	if result.Error != nil {
		return 0, fmt.Errorf("purging web device links: %w", result.Error)
	}
	return result.RowsAffected, nil
}
