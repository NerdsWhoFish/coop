package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/nerdswhofish/coop/internal/domain"
)

// ErrNotFound reports a row that does not exist, or that the caller may not
// see. Handlers render it as 404 either way.
var ErrNotFound = errors.New("store: not found")

// ErrLastAdmin reports an operation that would leave a family unable to manage
// itself, which nothing could undo.
var ErrLastAdmin = errors.New("store: a family must keep at least one admin")

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

// UpdateFamily changes a family's display settings.
func (a *Accounts) UpdateFamily(ctx context.Context, id uuid.UUID, name, timezone string) error {
	updates := map[string]any{"updated_at": a.now()}
	if name != "" {
		updates["name"] = name
	}
	if timezone != "" {
		updates["timezone"] = timezone
	}
	return wrap(a.db.WithContext(ctx).Model(&Family{}).
		Where("id = ?", id).Updates(updates).Error, "updating family")
}

// SetAPIKey stores an already-encrypted YouTube API key.
func (a *Accounts) SetAPIKey(ctx context.Context, familyID uuid.UUID, sealed []byte) error {
	return wrap(a.db.WithContext(ctx).Model(&Family{}).
		Where("id = ?", familyID).
		Updates(map[string]any{"encrypted_api_key": sealed, "updated_at": a.now()}).Error,
		"storing api key")
}

// ParentByEmail looks up a parent for sign-in.
func (a *Accounts) ParentByEmail(ctx context.Context, email string) (Parent, error) {
	var parent Parent
	err := a.db.WithContext(ctx).First(&parent, "email = ?", normalizeEmail(email)).Error
	return parent, wrap(err, "reading parent by email")
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

// DeleteParent removes a parent, refusing to strand a family without an admin.
func (a *Accounts) DeleteParent(ctx context.Context, familyID, parentID uuid.UUID) error {
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
func (a *Accounts) SetScope(ctx context.Context, parentID uuid.UUID, childIDs []uuid.UUID) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return setScopeTx(tx, parentID, childIDs)
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

// TouchSession records that a session was used, best effort.
func (a *Accounts) TouchSession(ctx context.Context, sessionID uuid.UUID) error {
	now := a.now()
	return wrap(a.db.WithContext(ctx).Model(&ParentSession{}).
		Where("id = ?", sessionID).
		Update("last_seen_at", now).Error, "touching session")
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

// CreateChild adds a viewing profile.
func (a *Accounts) CreateChild(ctx context.Context, familyID uuid.UUID, name, avatarID string) (Child, error) {
	child := Child{
		FamilyID:          familyID,
		Name:              name,
		AvatarID:          avatarID,
		ShortsEnabled:     true,
		WatchPageAutoplay: false,
		VideoSearchTiles:  true,
	}
	err := a.db.WithContext(ctx).Create(&child).Error
	return child, wrap(err, "creating child")
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
	Name              *string
	AvatarID          *string
	ShortsEnabled     *bool
	WatchPageAutoplay *bool
	VideoSearchTiles  *bool
	DailySearchLimit  *int
}

// UpdateChild applies the settings that were supplied.
func (a *Accounts) UpdateChild(ctx context.Context, familyID, childID uuid.UUID,
	settings ChildSettings) error {

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
	if settings.DailySearchLimit != nil {
		updates["daily_search_limit"] = *settings.DailySearchLimit
	}

	result := a.db.WithContext(ctx).Model(&Child{}).
		Where("id = ? AND family_id = ?", childID, familyID).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("updating child: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteChild removes a child and everything belonging to them.
func (a *Accounts) DeleteChild(ctx context.Context, familyID, childID uuid.UUID) error {
	result := a.db.WithContext(ctx).
		Delete(&Child{}, "id = ? AND family_id = ?", childID, familyID)
	if result.Error != nil {
		return fmt.Errorf("deleting child: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CreatePairingCode mints a single-use code bound to a child.
func (a *Accounts) CreatePairingCode(ctx context.Context, childID uuid.UUID,
	code string, expiresAt time.Time) error {

	row := PairingCode{Code: code, ChildID: childID, ExpiresAt: expiresAt}
	return wrap(a.db.WithContext(ctx).Create(&row).Error, "creating pairing code")
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
		return wrap(tx.Create(&device).Error, "registering device")
	})
	if err != nil {
		return Child{}, ChildDevice{}, err
	}
	return child, device, nil
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
func (a *Accounts) TouchDevice(ctx context.Context, deviceID uuid.UUID) error {
	now := a.now()
	return wrap(a.db.WithContext(ctx).Model(&ChildDevice{}).
		Where("id = ?", deviceID).
		Update("last_seen_at", now).Error, "touching device")
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

// RevokeDevice marks a device's token dead. The row is kept so the parent app
// can still show that the device once existed.
func (a *Accounts) RevokeDevice(ctx context.Context, deviceID uuid.UUID) error {
	now := a.now()
	result := a.db.WithContext(ctx).Model(&ChildDevice{}).
		Where("id = ? AND revoked_at IS NULL", deviceID).
		Update("revoked_at", now)
	if result.Error != nil {
		return fmt.Errorf("revoking device: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
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
