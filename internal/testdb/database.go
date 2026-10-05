//go:build integration

// Package testdb isolates database integration tests from concurrent suites.
package testdb

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// New creates a database owned by the test. The configured role needs CREATEDB.
// Register application connection cleanup after this call so it runs first.
func New(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("COOP_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("COOP_TEST_DATABASE_DSN not set")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connecting to integration database: %v", err)
	}
	name := "coop_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{name}.Sanitize()
	created := false
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		defer func() { _ = admin.Close(ctx) }()
		if created {
			if _, err := admin.Exec(ctx, "DROP DATABASE "+identifier+" WITH (FORCE)"); err != nil {
				t.Errorf("dropping integration database: %v", err)
			}
		}
	})
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0"); err != nil {
		t.Fatalf("creating integration database (role needs CREATEDB): %v", err)
	}
	created = true
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		u.Path, u.RawPath = "/"+name, ""
		query := u.Query()
		query.Del("dbname")
		u.RawQuery = query.Encode()
		return u.String()
	}
	return dsn + " dbname=" + name
}
