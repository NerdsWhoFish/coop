//go:build integration

package store

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/domain"
)

var pushTestNow = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

func TestPushTokenAudiencesAndScope(t *testing.T) {
	db := migratedDB(t)
	accounts := NewAccounts(db, fixedClock(pushTestNow))
	ctx := context.Background()

	family, admin, err := accounts.CreateFamily(
		ctx, "Push Test", "UTC", uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Delete(&Family{}, "id = ?", family.ID) })

	child, err := accounts.CreateChild(ctx, family.ID, "Cooper", "", admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := accounts.CreateParent(ctx, family.ID, uuid.NewString()+"@example.com",
		"hash", domain.RoleParent, []uuid.UUID{child.ID})
	if err != nil {
		t.Fatal(err)
	}
	unscoped, err := accounts.CreateParent(ctx, family.ID, uuid.NewString()+"@example.com",
		"hash", domain.RoleParent, nil)
	if err != nil {
		t.Fatal(err)
	}

	device := ChildDevice{ChildID: child.ID, Name: "iPad", TokenHash: uuid.NewString()}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}

	for parentID, token := range map[uuid.UUID]string{
		admin.ID: "t-admin", scoped.ID: "t-scoped", unscoped.ID: "t-unscoped",
	} {
		if err := accounts.SaveParentPushToken(ctx, family.ID, parentID, token); err != nil {
			t.Fatal(err)
		}
	}
	if err := accounts.SaveChildPushToken(ctx, family.ID, child.ID, device.ID, "t-child"); err != nil {
		t.Fatal(err)
	}

	parentTokens, err := accounts.ParentPushTokensForChild(ctx, family.ID, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(parentTokens)
	if !slices.Equal(parentTokens, []string{"t-admin", "t-scoped"}) {
		t.Fatalf("parent tokens = %v, want the admin and the scoped parent only", parentTokens)
	}

	childTokens, err := accounts.ChildPushTokens(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(childTokens, []string{"t-child"}) {
		t.Fatalf("child tokens = %v, want only the paired device", childTokens)
	}
}

func TestPushTokenLifecycle(t *testing.T) {
	db := migratedDB(t)
	accounts := NewAccounts(db, fixedClock(pushTestNow))
	ctx := context.Background()

	family, admin, err := accounts.CreateFamily(
		ctx, "Push Lifecycle", "UTC", uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Delete(&Family{}, "id = ?", family.ID) })

	child, err := accounts.CreateChild(ctx, family.ID, "River", "", admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	device := ChildDevice{ChildID: child.ID, Name: "iPad", TokenHash: uuid.NewString()}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}

	// Unpairing removes the device row, which must take the token with it.
	if err := accounts.SaveChildPushToken(ctx, family.ID, child.ID, device.ID, "t-cascade"); err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&ChildDevice{}, "id = ?", device.ID).Error; err != nil {
		t.Fatal(err)
	}
	tokens, err := accounts.ChildPushTokens(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Fatalf("child tokens after unpair = %v, want none", tokens)
	}

	// A device signing in as a different parent reassigns its token.
	second, err := accounts.CreateParent(ctx, family.ID, uuid.NewString()+"@example.com",
		"hash", domain.RoleAdmin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := accounts.SaveParentPushToken(ctx, family.ID, admin.ID, "t-shared"); err != nil {
		t.Fatal(err)
	}
	if err := accounts.SaveParentPushToken(ctx, family.ID, second.ID, "t-shared"); err != nil {
		t.Fatal(err)
	}

	// Deleting is scoped to the owner, so the old account cannot unregister it.
	if err := accounts.DeleteParentPushToken(ctx, admin.ID, "t-shared"); err != nil {
		t.Fatal(err)
	}
	remaining, err := accounts.ParentPushTokensForChild(ctx, family.ID, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(remaining, "t-shared") {
		t.Fatal("token deleted by a parent who no longer owns it")
	}
	if err := accounts.DeleteParentPushToken(ctx, second.ID, "t-shared"); err != nil {
		t.Fatal(err)
	}

	// Apple reporting a token gone prunes it regardless of owner.
	if err := accounts.SaveParentPushToken(ctx, family.ID, second.ID, "t-dead"); err != nil {
		t.Fatal(err)
	}
	if err := accounts.PrunePushTokens(ctx, []string{"t-dead"}); err != nil {
		t.Fatal(err)
	}
	remaining, err = accounts.ParentPushTokensForChild(ctx, family.ID, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("tokens after prune and delete = %v, want none", remaining)
	}
}
