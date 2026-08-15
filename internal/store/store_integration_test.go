//go:build integration

package store

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm/schema"

	"github.com/nerdswhofish/coop/internal/config"
)

// parseModel resolves a model's table and column names exactly as GORM will at
// runtime, without needing a session or a live query.
func parseModel(t *testing.T, model any) *schema.Schema {
	t.Helper()

	s, err := schema.Parse(model, &sync.Map{}, namingStrategy)
	if err != nil {
		t.Fatalf("parsing %T: %v", model, err)
	}
	return s
}

func testDB(t *testing.T) *DB {
	t.Helper()

	dsn := os.Getenv("COOP_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("COOP_TEST_DATABASE_DSN not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := Open(ctx, config.Database{
		DSN:             dsn,
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
	}, true)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// A migration that cannot roll back is a migration that cannot be recovered
// from, so the down leg is exercised rather than assumed.
func TestMigrateRoundTrip(t *testing.T) {
	db := testDB(t)

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	v, dirty, err := db.Version()
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if dirty {
		t.Fatal("schema is dirty after a clean migration")
	}
	if v == 0 {
		t.Fatal("Version() = 0, want at least one applied migration")
	}

	if err := db.MigrateDown(); err != nil {
		t.Fatalf("MigrateDown() error = %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() after rollback error = %v", err)
	}

	after, dirty, err := db.Version()
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if dirty {
		t.Fatal("schema is dirty after replaying migrations")
	}
	if after != v {
		t.Fatalf("version after round trip = %d, want %d", after, v)
	}
}

// GORM derives table names from Go types. A mismatch against the migrations
// would not fail until a query ran in production, so it is asserted here.
func TestModelsMatchMigratedTables(t *testing.T) {
	db := testDB(t)
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	for _, model := range AllModels() {
		table := parseModel(t, model).Table

		var exists bool
		err := db.Raw(
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			                WHERE table_schema = 'public' AND table_name = ?)`,
			table,
		).Scan(&exists).Error
		if err != nil {
			t.Fatalf("querying information_schema for %q: %v", table, err)
		}
		if !exists {
			t.Errorf("model %T maps to table %q, which no migration creates", model, table)
		}
	}
}

// Every column GORM expects must exist, which catches a field renamed in Go
// without a matching migration.
func TestModelColumnsExist(t *testing.T) {
	db := testDB(t)
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	for _, model := range AllModels() {
		s := parseModel(t, model)

		for _, field := range s.Fields {
			if field.DBName == "" {
				continue
			}
			var exists bool
			err := db.Raw(
				`SELECT EXISTS (SELECT 1 FROM information_schema.columns
				                WHERE table_schema = 'public'
				                  AND table_name = ? AND column_name = ?)`,
				s.Table, field.DBName,
			).Scan(&exists).Error
			if err != nil {
				t.Fatalf("querying columns for %s.%s: %v", s.Table, field.DBName, err)
			}
			if !exists {
				t.Errorf("%T expects column %s.%s, which no migration creates",
					model, s.Table, field.DBName)
			}
		}
	}
}
