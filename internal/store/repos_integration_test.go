//go:build integration

package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/auth"
	"github.com/nerdswhofish/coop/internal/domain"
	"github.com/nerdswhofish/coop/internal/youtube"
)

func migratedDB(t *testing.T) *DB {
	t.Helper()
	db := testDB(t)
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return db
}

func newFamily(t *testing.T, db *DB) uuid.UUID {
	t.Helper()
	family := Family{Name: "Test Family", Timezone: "UTC"}
	if err := db.Create(&family).Error; err != nil {
		t.Fatalf("creating family: %v", err)
	}
	t.Cleanup(func() { db.Delete(&Family{}, "id = ?", family.ID) })
	return family.ID
}

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestAPICacheRoundTrip(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	cache := NewAPICacheStore(db, fixedClock(now))

	key := "videos.list:" + uuid.NewString()
	t.Cleanup(func() { db.Delete(&APICache{}, "key = ?", key) })

	if _, ok, err := cache.Get(ctx, key); err != nil || ok {
		t.Fatalf("Get() before Put = (%v, %v), want a clean miss", ok, err)
	}

	body := []byte(`{"items":[{"id":"abc"}]}`)
	if err := cache.Put(ctx, key, "videos.list", body, time.Hour); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	got, ok, err := cache.Get(ctx, key)
	if err != nil || !ok {
		t.Fatalf("Get() after Put = (%v, %v), want a hit", ok, err)
	}
	if string(got) != string(body) {
		t.Errorf("Get() = %q, want %q", got, body)
	}
}

func TestAuthChallengeCompletionRejectsTOTPStepReplay(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	accounts := NewAccounts(db, fixedClock(now))
	family, parent, err := accounts.CreateFamily(
		ctx, "Auth Test", "UTC", uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Delete(&Family{}, "id = ?", family.ID) })

	first := ParentAuthChallenge{
		ParentID: parent.ID, TokenHash: uuid.NewString(), Purpose: AuthPurposeEnroll,
		EncryptedTOTPSecret: []byte("sealed"), ExpiresAt: now.Add(time.Minute),
	}
	if err := accounts.CreateAuthChallenge(ctx, first); err != nil {
		t.Fatal(err)
	}
	stored, _, err := accounts.AuthChallengeByToken(ctx, first.TokenHash, 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.CompleteAuthChallenge(ctx, stored.ID, 100, 5,
		ParentSession{TokenHash: uuid.NewString(), ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatalf("first CompleteAuthChallenge() error = %v", err)
	}

	second := ParentAuthChallenge{
		ParentID: parent.ID, TokenHash: uuid.NewString(), Purpose: AuthPurposeLogin,
		ExpiresAt: now.Add(time.Minute),
	}
	if err := accounts.CreateAuthChallenge(ctx, second); err != nil {
		t.Fatal(err)
	}
	stored, _, err = accounts.AuthChallengeByToken(ctx, second.TokenHash, 5)
	if err != nil {
		t.Fatal(err)
	}
	_, err = accounts.CompleteAuthChallenge(ctx, stored.ID, 100, 5,
		ParentSession{TokenHash: uuid.NewString(), ExpiresAt: now.Add(time.Hour)})
	if !errors.Is(err, ErrTOTPReplay) {
		t.Fatalf("replayed CompleteAuthChallenge() error = %v, want ErrTOTPReplay", err)
	}
}

func TestAuthThrottlePersistsLockout(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	key := uuid.NewString()
	accounts := NewAccounts(db, fixedClock(now))
	t.Cleanup(func() { db.Delete(&AuthThrottle{}, "key_hash = ?", key) })

	for range 3 {
		if err := accounts.RecordAuthFailure(ctx, "test", []string{key},
			3, time.Minute, 5*time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	until, locked, err := accounts.AuthLocked(ctx, "test", []string{key})
	if err != nil {
		t.Fatal(err)
	}
	if !locked || !until.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("AuthLocked() = (%v, %v), want lock until %v", until, locked, now.Add(5*time.Minute))
	}

	reopened := NewAccounts(db, fixedClock(now.Add(time.Minute)))
	if _, locked, err := reopened.AuthLocked(ctx, "test", []string{key}); err != nil || !locked {
		t.Fatalf("AuthLocked() after repository recreation = (%v, %v), want persisted lock", locked, err)
	}
}

func TestAuditEventsRespectParentScopeAndExcludeSecrets(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	accounts := NewAccounts(db, fixedClock(now))
	family, parent, err := accounts.CreateFamily(
		ctx, "Audit Test", "UTC", uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Delete(&Family{}, "id = ?", family.ID) })
	child, err := accounts.CreateChild(ctx, family.ID, "Cooper", "", parent.ID)
	if err != nil {
		t.Fatal(err)
	}

	channel := youtube.Channel{ID: "UCabcdefghijklmnopqrstuv", Title: "Audit channel"}
	if err := NewCatalog(db, fixedClock(now)).UpsertChannels(ctx, []youtube.Channel{channel}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Delete(&Channel{}, "id = ?", channel.ID) })
	rules := NewRules(db, fixedClock(now))
	if err := rules.AllowGlobally(ctx, family.ID, channel.ID, parent.ID); err != nil {
		t.Fatal(err)
	}
	if err := rules.AllowForChild(ctx, child.ID, channel.ID, parent.ID); err != nil {
		t.Fatal(err)
	}
	if err := accounts.SetAPIKey(ctx, family.ID, []byte("sealed-secret-value"), parent.ID); err != nil {
		t.Fatal(err)
	}

	audit := NewAudit(db)
	adminEvents, err := audit.Events(ctx, AuditQuery{FamilyID: family.ID, IncludeGlobal: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(adminEvents) < 4 {
		t.Fatalf("admin audit events = %d, want at least 4", len(adminEvents))
	}
	for _, event := range adminEvents {
		if strings.Contains(string(event.Before), "sealed-secret-value") ||
			strings.Contains(string(event.After), "sealed-secret-value") {
			t.Fatalf("audit event %s contains secret material", event.Action)
		}
	}

	scopedEvents, err := audit.Events(ctx, AuditQuery{
		FamilyID: family.ID, ChildIDs: []uuid.UUID{child.ID}, IncludeGlobal: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range scopedEvents {
		if event.ChildID == nil || *event.ChildID != child.ID {
			t.Fatalf("scoped audit returned global or another-child event: %+v", event)
		}
	}
}

func TestResetParentTOTPRevokesAuthenticationState(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	accounts := NewAccounts(db, fixedClock(now))
	family, parent, err := accounts.CreateFamily(
		ctx, "Recovery Test", "UTC", uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Delete(&Family{}, "id = ?", family.ID) })
	if err := db.Model(&parent).Updates(map[string]any{
		"encrypted_totp_secret": []byte("sealed"), "totp_last_used_step": int64(42),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.CreateSession(ctx, parent.ID, uuid.NewString(), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := accounts.CreateAuthChallenge(ctx, ParentAuthChallenge{
		ParentID: parent.ID, TokenHash: uuid.NewString(), Purpose: AuthPurposeLogin,
		ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	if err := accounts.ResetParentTOTP(ctx, parent.Email); err != nil {
		t.Fatal(err)
	}
	got, err := accounts.Parent(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.EncryptedTOTPSecret) != 0 || got.TOTPLastUsedStep != nil {
		t.Fatalf("parent TOTP state survived reset: %+v", got)
	}
	var sessions, challenges int64
	if err := db.Model(&ParentSession{}).Where("parent_id = ?", parent.ID).Count(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&ParentAuthChallenge{}).Where("parent_id = ?", parent.ID).Count(&challenges).Error; err != nil {
		t.Fatal(err)
	}
	if sessions != 0 || challenges != 0 {
		t.Fatalf("authentication state after reset = %d sessions and %d challenges, want zero", sessions, challenges)
	}
}

func TestUnlockParentLoginOnlyClearsEmailBucket(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	accounts := NewAccounts(db, fixedClock(now))
	family, parent, err := accounts.CreateFamily(
		ctx, "Unlock Test", "UTC", uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Delete(&Family{}, "id = ?", family.ID) })
	emailKey := auth.HashToken("parent-email:" + parent.Email)
	addressKey := auth.HashToken("client-address:192.0.2.1")
	if err := accounts.RecordAuthFailure(ctx, "parent-login", []string{emailKey, addressKey},
		1, time.Minute, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := accounts.UnlockParentLogin(ctx, parent.Email, emailKey); err != nil {
		t.Fatal(err)
	}
	if _, locked, err := accounts.AuthLocked(ctx, "parent-login", []string{emailKey}); err != nil || locked {
		t.Fatalf("email throttle after unlock = (%v, %v), want unlocked", locked, err)
	}
	if _, locked, err := accounts.AuthLocked(ctx, "parent-login", []string{addressKey}); err != nil || !locked {
		t.Fatalf("address throttle after unlock = (%v, %v), want retained", locked, err)
	}
}

func TestDeleteFamilyCascadesTenantData(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	accounts := NewAccounts(db, nil)
	family, parent, err := accounts.CreateFamily(
		ctx, "Delete Test", "UTC", uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.CreateChild(ctx, family.ID, "Cooper", "", parent.ID); err != nil {
		t.Fatal(err)
	}
	if err := accounts.DeleteFamily(ctx, family.ID); err != nil {
		t.Fatal(err)
	}
	var families, parents, children int64
	if err := db.Model(&Family{}).Where("id = ?", family.ID).Count(&families).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Parent{}).Where("family_id = ?", family.ID).Count(&parents).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Child{}).Where("family_id = ?", family.ID).Count(&children).Error; err != nil {
		t.Fatal(err)
	}
	if families != 0 || parents != 0 || children != 0 {
		t.Fatalf("tenant rows after deletion = %d families, %d parents, %d children", families, parents, children)
	}
}

// The response column has to hold raw upstream bodies, and channel feeds are
// XML rather than JSON.
func TestAPICacheStoresXML(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	cache := NewAPICacheStore(db, nil)

	key := "feed:" + uuid.NewString()
	t.Cleanup(func() { db.Delete(&APICache{}, "key = ?", key) })

	body := []byte(`<?xml version="1.0"?><feed><entry>not json at all</entry></feed>`)
	if err := cache.Put(ctx, key, "feed", body, time.Hour); err != nil {
		t.Fatalf("Put() with an XML body error = %v", err)
	}

	got, ok, err := cache.Get(ctx, key)
	if err != nil || !ok {
		t.Fatalf("Get() = (%v, %v), want a hit", ok, err)
	}
	if string(got) != string(body) {
		t.Errorf("Get() = %q, want the XML back unchanged", got)
	}
}

func TestAPICacheExpiryIsAMissNotAnError(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	start := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	key := "videos.list:" + uuid.NewString()
	t.Cleanup(func() { db.Delete(&APICache{}, "key = ?", key) })

	writer := NewAPICacheStore(db, fixedClock(start))
	if err := writer.Put(ctx, key, "videos.list", []byte(`{}`), time.Hour); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	later := NewAPICacheStore(db, fixedClock(start.Add(2*time.Hour)))
	got, ok, err := later.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() past expiry error = %v, want a clean miss", err)
	}
	if ok {
		t.Errorf("Get() past expiry = %q, want a miss", got)
	}
}

func TestAPICachePutOverwrites(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	cache := NewAPICacheStore(db, nil)

	key := "videos.list:" + uuid.NewString()
	t.Cleanup(func() { db.Delete(&APICache{}, "key = ?", key) })

	if err := cache.Put(ctx, key, "videos.list", []byte("first"), time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(ctx, key, "videos.list", []byte("second"), time.Hour); err != nil {
		t.Fatalf("second Put() error = %v, want an upsert not a conflict", err)
	}

	got, _, err := cache.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Errorf("Get() = %q, want the later value", got)
	}
}

func TestAPICachePurgeExpired(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	start := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	stale := "stale:" + uuid.NewString()
	fresh := "fresh:" + uuid.NewString()
	t.Cleanup(func() { db.Delete(&APICache{}, "key IN ?", []string{stale, fresh}) })

	writer := NewAPICacheStore(db, fixedClock(start))
	if err := writer.Put(ctx, stale, "videos.list", []byte("old"), time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := writer.Put(ctx, fresh, "videos.list", []byte("new"), 48*time.Hour); err != nil {
		t.Fatal(err)
	}

	purger := NewAPICacheStore(db, fixedClock(start.Add(2*time.Hour)))
	if _, err := purger.PurgeExpired(ctx); err != nil {
		t.Fatalf("PurgeExpired() error = %v", err)
	}

	if _, ok, _ := purger.Get(ctx, stale); ok {
		t.Error("the expired entry survived the purge")
	}
	if _, ok, _ := purger.Get(ctx, fresh); !ok {
		t.Error("the live entry was purged, want it kept")
	}
}

// The whole point of doing the increment in SQL is that repeated records
// accumulate rather than overwrite.
func TestQuotaRecordAccumulates(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	familyID := newFamily(t, db)
	ledger := NewQuotaStore(db, nil)
	day := "2026-08-15"

	for range 5 {
		if err := ledger.Record(ctx, familyID, day, domain.PurposeFeed, 3, 1); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}

	usage, err := ledger.Usage(ctx, familyID, day)
	if err != nil {
		t.Fatalf("Usage() error = %v", err)
	}
	if got := usage[domain.PurposeFeed]; got.Units != 15 || got.Calls != 5 {
		t.Errorf("feed spend = %+v, want 15 units and 5 calls", got)
	}
}

func TestQuotaPurposesAreIndependent(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	familyID := newFamily(t, db)
	ledger := NewQuotaStore(db, nil)
	day := "2026-08-15"

	if err := ledger.Record(ctx, familyID, day, domain.PurposeFeed, 10, 10); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Record(ctx, familyID, day, domain.PurposeSearch, 0, 3); err != nil {
		t.Fatal(err)
	}

	usage, err := ledger.Usage(ctx, familyID, day)
	if err != nil {
		t.Fatal(err)
	}
	if got := usage[domain.PurposeFeed]; got.Units != 10 {
		t.Errorf("feed units = %d, want 10", got.Units)
	}
	// Search is metered by call, so it must record calls without units.
	if got := usage[domain.PurposeSearch]; got.Calls != 3 || got.Units != 0 {
		t.Errorf("search spend = %+v, want 3 calls and 0 units", got)
	}
	if _, present := usage[domain.PurposeBackfill]; present {
		t.Error("backfill has an entry, want purposes with no spend absent")
	}
}

func TestQuotaDaysAreIndependent(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	familyID := newFamily(t, db)
	ledger := NewQuotaStore(db, nil)

	if err := ledger.Record(ctx, familyID, "2026-08-14", domain.PurposeFeed, 100, 100); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Record(ctx, familyID, "2026-08-15", domain.PurposeFeed, 5, 5); err != nil {
		t.Fatal(err)
	}

	usage, err := ledger.Usage(ctx, familyID, "2026-08-15")
	if err != nil {
		t.Fatal(err)
	}
	if got := usage[domain.PurposeFeed].Units; got != 5 {
		t.Errorf("today's units = %d, want 5 with yesterday excluded", got)
	}
}

func TestQuotaFamiliesAreIsolated(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	first := newFamily(t, db)
	second := newFamily(t, db)
	ledger := NewQuotaStore(db, nil)
	day := "2026-08-15"

	if err := ledger.Record(ctx, first, day, domain.PurposeFeed, 50, 50); err != nil {
		t.Fatal(err)
	}

	usage, err := ledger.Usage(ctx, second, day)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 0 {
		t.Errorf("second family usage = %+v, want empty", usage)
	}
}

func TestQuotaPurgeBefore(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	familyID := newFamily(t, db)
	ledger := NewQuotaStore(db, nil)

	if err := ledger.Record(ctx, familyID, "2026-08-01", domain.PurposeFeed, 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Record(ctx, familyID, "2026-08-15", domain.PurposeFeed, 1, 1); err != nil {
		t.Fatal(err)
	}

	if _, err := ledger.PurgeBefore(ctx, "2026-08-10"); err != nil {
		t.Fatalf("PurgeBefore() error = %v", err)
	}

	old, err := ledger.Usage(ctx, familyID, "2026-08-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(old) != 0 {
		t.Errorf("old day usage = %+v, want it purged", old)
	}

	recent, err := ledger.Usage(ctx, familyID, "2026-08-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) == 0 {
		t.Error("recent day was purged, want it kept")
	}
}

func TestFamiliesWithAPIKeysExcludesUnconfiguredFamilies(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	configured := newFamily(t, db)
	_ = newFamily(t, db)

	if err := db.Model(&Family{}).Where("id = ?", configured).
		Update("encrypted_api_key", []byte("sealed")).Error; err != nil {
		t.Fatal(err)
	}
	families, err := NewAccounts(db, nil).FamiliesWithAPIKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for _, family := range families {
		if family.ID != configured {
			t.Fatalf("FamiliesWithAPIKeys() included unconfigured family %s", family.ID)
		}
	}
}

func TestParentInvitationIsScopedAndSingleUse(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	accounts := NewAccounts(db, fixedClock(now))
	family, admin, err := accounts.CreateFamily(
		ctx, "Invitations", "UTC", uuid.NewString()+"@example.com", "admin-hash")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Delete(&Family{}, "id = ?", family.ID) })

	child, err := accounts.CreateChild(ctx, family.ID, "Cooper", "", admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	tokenHash := "invitation-" + uuid.NewString()
	invitation, err := accounts.CreateParentInvitation(ctx, ParentInvitation{
		FamilyID:  family.ID,
		Email:     "  INVITED@example.com ",
		Role:      domain.RoleParent,
		TokenHash: tokenHash,
		CreatedBy: admin.ID,
		ExpiresAt: now.Add(time.Hour),
	}, []uuid.UUID{child.ID})
	if err != nil {
		t.Fatal(err)
	}
	if invitation.Email != "invited@example.com" {
		t.Errorf("invitation email = %q, want normalized", invitation.Email)
	}

	parent, err := accounts.RedeemParentInvitation(ctx, tokenHash, "parent-hash")
	if err != nil {
		t.Fatalf("RedeemParentInvitation() error = %v", err)
	}
	if parent.Email != "invited@example.com" || parent.PasswordHash != "parent-hash" {
		t.Errorf("created parent = %+v", parent)
	}
	scope, err := accounts.ScopedChildIDs(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(scope) != 1 || scope[0] != child.ID {
		t.Errorf("parent scope = %v, want [%s]", scope, child.ID)
	}

	if _, err := accounts.RedeemParentInvitation(ctx, tokenHash, "another-hash"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second redemption error = %v, want ErrNotFound", err)
	}
}

func TestStaleApprovedChannelIDsUsesUploadsClock(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	accounts := NewAccounts(db, fixedClock(now))
	family, parent, err := accounts.CreateFamily(ctx, "Ingest", "UTC", uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	child, err := accounts.CreateChild(ctx, family.ID, "Cooper", "", parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Delete(&AllowGlobal{}, "family_id = ?", family.ID).Error; err != nil {
			t.Errorf("cleaning global allows: %v", err)
		}
		if err := db.Delete(&AllowChild{}, "child_id = ?", child.ID).Error; err != nil {
			t.Errorf("cleaning child allows: %v", err)
		}
		if err := db.Delete(&Family{}, "id = ?", family.ID).Error; err != nil {
			t.Errorf("cleaning family: %v", err)
		}
	})

	catalog := NewCatalog(db, fixedClock(now))
	channels := []youtube.Channel{
		{ID: "UCabcdefghijklmnopqrstuv", Title: "new global"},
		{ID: "UCbcdefghijklmnopqrstuvwx", Title: "stale child"},
		{ID: "UCcdefghijklmnopqrstuvwxy", Title: "fresh global"},
		{ID: "UCdefghijklmnopqrstuvwxyz", Title: "globally allowed but denied"},
		{ID: "UCefghijklmnopqrstuvwxyz0", Title: "search only"},
	}
	t.Cleanup(func() {
		ids := make([]string, len(channels))
		for i, channel := range channels {
			ids[i] = channel.ID
		}
		if err := db.Delete(&Channel{}, "id IN ?", ids).Error; err != nil {
			t.Errorf("cleaning channels: %v", err)
		}
	})
	if err := catalog.UpsertChannels(ctx, channels); err != nil {
		t.Fatal(err)
	}

	rules := NewRules(db, fixedClock(now))
	if err := rules.AllowGlobally(ctx, family.ID, channels[0].ID, parent.ID); err != nil {
		t.Fatal(err)
	}
	if err := rules.AllowForChild(ctx, child.ID, channels[1].ID, parent.ID); err != nil {
		t.Fatal(err)
	}
	if err := rules.AllowGlobally(ctx, family.ID, channels[2].ID, parent.ID); err != nil {
		t.Fatal(err)
	}
	if err := rules.AllowGlobally(ctx, family.ID, channels[3].ID, parent.ID); err != nil {
		t.Fatal(err)
	}
	if err := rules.DenyForChild(ctx, child.ID, channels[3].ID, parent.ID); err != nil {
		t.Fatal(err)
	}

	staleAt := now.Add(-12 * time.Hour)
	freshAt := now.Add(-time.Hour)
	if err := db.Model(&Channel{}).Where("id = ?", channels[1].ID).
		Update("uploads_fetched_at", staleAt).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Channel{}).Where("id = ?", channels[2].ID).
		Update("uploads_fetched_at", freshAt).Error; err != nil {
		t.Fatal(err)
	}

	ids, err := catalog.StaleApprovedChannelIDs(ctx, family.ID, now.Add(-6*time.Hour), 50)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{channels[0].ID: true, channels[1].ID: true}
	if len(ids) != len(want) {
		t.Fatalf("StaleApprovedChannelIDs() = %v, want the new and stale approved channels", ids)
	}
	for _, id := range ids {
		if !want[id] {
			t.Errorf("StaleApprovedChannelIDs() included %s", id)
		}
	}

	if err := catalog.MarkChannelRefreshed(ctx, channels[0].ID); err != nil {
		t.Fatal(err)
	}
	ids, err = catalog.StaleApprovedChannelIDs(ctx, family.ID, now.Add(-6*time.Hour), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != channels[1].ID {
		t.Fatalf("after MarkChannelRefreshed() = %v, want only %s", ids, channels[1].ID)
	}
}

func TestCatalogVideosByIDPreservesCallerOrder(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	catalog := NewCatalog(db, nil)
	channelID := "UC" + uuid.NewString()
	firstID := "video-" + uuid.NewString()
	secondID := "video-" + uuid.NewString()

	if err := catalog.UpsertChannels(ctx, []youtube.Channel{{ID: channelID, Title: "Channel"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Delete(&Channel{}, "id = ?", channelID).Error; err != nil {
			t.Errorf("cleaning channel: %v", err)
		}
	})

	if err := catalog.UpsertVideos(ctx, []youtube.Video{
		{ID: firstID, ChannelID: channelID, Title: "First"},
		{ID: secondID, ChannelID: channelID, Title: "Second"},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := catalog.VideosByID(ctx, []string{secondID, "missing", firstID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != secondID || got[1].ID != firstID {
		t.Fatalf("VideosByID() = %+v, want second then first with missing skipped", got)
	}
}
