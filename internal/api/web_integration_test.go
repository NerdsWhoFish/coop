//go:build integration

package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/auth"
	"github.com/nerdswhofish/coop/internal/config"
	"github.com/nerdswhofish/coop/internal/store"
)

func TestDisabledWebLinkingRejectsChildApproval(t *testing.T) {
	dsn := os.Getenv("COOP_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("COOP_TEST_DATABASE_DSN not set")
	}

	ctx := context.Background()
	now := time.Date(2026, 8, 17, 20, 0, 0, 0, time.UTC)
	db, err := store.Open(ctx, config.Database{DSN: dsn}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	accounts := store.NewAccounts(db, func() time.Time { return now })
	family, parent, err := accounts.CreateFamily(
		ctx, "Web link policy", "UTC", uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Delete(&store.Family{}, "id = ?", family.ID) })
	child, err := accounts.CreateChild(ctx, family.ID, "River", "", parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	if err := accounts.UpdateChild(ctx, family.ID, child.ID,
		store.ChildSettings{WebLinkingEnabled: &disabled}, parent.ID); err != nil {
		t.Fatal(err)
	}

	approvalToken := uuid.NewString()
	link, err := accounts.CreateWebDeviceLink(ctx, auth.HashToken(approvalToken),
		auth.HashToken(uuid.NewString()), "Family computer", now.Add(webLinkTTL))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Delete(&store.WebDeviceLink{}, "id = ?", link.ID) })
	server := &Server{deps: Deps{
		Accounts: accounts,
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}}
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/child/web-links/"+link.ID.String()+"/approve",
		bytes.NewBufferString(`{"approvalToken":"`+approvalToken+`"}`))
	request.SetPathValue("linkId", link.ID.String())
	recorder := httptest.NewRecorder()
	err = server.handleApproveWebLinkAsChild(recorder, request, auth.Child{
		ID: child.ID, FamilyID: family.ID, DeviceID: uuid.New(),
	})
	var responseErr *apiError
	if !errors.As(err, &responseErr) || responseErr.status != http.StatusForbidden {
		t.Fatalf("approval error = %v, want 403", err)
	}

	parentRequest := httptest.NewRequest(http.MethodPost,
		"/api/v1/parent/children/"+child.ID.String()+"/web-links/"+link.ID.String()+"/approve",
		bytes.NewBufferString(`{"approvalToken":"`+approvalToken+`"}`))
	parentRequest.SetPathValue("childId", child.ID.String())
	parentRequest.SetPathValue("linkId", link.ID.String())
	err = server.handleApproveWebLinkAsParent(recorder, parentRequest, auth.Parent{
		ID: parent.ID, FamilyID: family.ID, Role: parent.Role,
	})
	if !errors.As(err, &responseErr) || responseErr.status != http.StatusForbidden {
		t.Fatalf("parent approval error = %v, want 403", err)
	}
}
