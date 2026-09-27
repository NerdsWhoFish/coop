//go:build integration

package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/auth"
	"github.com/nerdswhofish/coop/internal/config"
	"github.com/nerdswhofish/coop/internal/domain"
	"github.com/nerdswhofish/coop/internal/store"
	"github.com/nerdswhofish/coop/internal/youtube"
)

func TestDeleteParentWithApprovals(t *testing.T) {
	dsn := os.Getenv("COOP_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("COOP_TEST_DATABASE_DSN not set")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, config.Database{DSN: dsn}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	accounts := store.NewAccounts(db, nil)
	family, admin, err := accounts.CreateFamily(ctx, "Deletion test", "UTC", uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Delete(&store.Family{}, "id = ?", family.ID) })
	parent, err := accounts.CreateParent(ctx, family.ID, uuid.NewString()+"@example.com", "hash", domain.RoleParent, nil)
	if err != nil {
		t.Fatal(err)
	}
	channelID := uuid.NewString()
	if err := store.NewCatalog(db, nil).UpsertChannels(ctx, []youtube.Channel{{ID: channelID}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Delete(&store.Channel{}, "id = ?", channelID) })
	if err := store.NewRules(db, nil).AllowGlobally(ctx, family.ID, channelID, parent.ID); err != nil {
		t.Fatal(err)
	}
	server := &Server{deps: Deps{Accounts: accounts, Logger: slog.Default()}}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/parent/parents/"+parent.ID.String(), nil)
	request.SetPathValue("parentId", parent.ID.String())
	for _, actor := range []auth.Parent{
		{ID: parent.ID, FamilyID: family.ID, Role: parent.Role},
		{ID: admin.ID, FamilyID: family.ID, Role: admin.Role},
	} {
		recorder := httptest.NewRecorder()
		err := server.handleDeleteParent(recorder, request, actor)
		if actor.Role != domain.RoleAdmin {
			if !errors.Is(err, auth.ErrNotAdmin) {
				t.Fatalf("non-admin deletion = %v, want ErrNotAdmin", err)
			}
			continue
		}
		if err != nil || recorder.Code != http.StatusNoContent {
			t.Fatalf("delete status = %d, error = %v; want 204", recorder.Code, err)
		}
	}
	var approval store.AllowGlobal
	if err := db.First(&approval, "family_id = ? AND channel_id = ?", family.ID, channelID).Error; err != nil {
		t.Fatal(err)
	}
	if approval.ApprovedBy != nil {
		t.Fatalf("deleted parent attribution = %v, want nil", approval.ApprovedBy)
	}
}
