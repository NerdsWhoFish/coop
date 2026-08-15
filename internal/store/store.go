package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"github.com/nerdswhofish/coop/internal/config"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// namingStrategy keeps table names singular, so Go type Child maps to table
// "child" rather than depending on GORM's pluralizer for irregular nouns.
var namingStrategy = schema.NamingStrategy{SingularTable: true}

// DB wraps a GORM handle plus the underlying pool, which migrations need.
type DB struct {
	*gorm.DB
	sql *sql.DB
}

// Open connects to Postgres and configures the pool. It does not migrate.
func Open(ctx context.Context, cfg config.Database, quiet bool) (*DB, error) {
	gormLog := logger.Default.LogMode(logger.Warn)
	if quiet {
		gormLog = logger.Discard
	}

	gdb, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{
		NamingStrategy: namingStrategy,
		Logger:         gormLog,
		NowFunc:        func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres: %w", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("unwrapping sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}
	return &DB{DB: gdb, sql: sqlDB}, nil
}

// Close releases the connection pool.
func (d *DB) Close() error { return d.sql.Close() }

// SQL exposes the raw pool for migrations and health checks.
func (d *DB) SQL() *sql.DB { return d.sql }

// migrator opens the embedded migrations. Callers must Close the result.
func (d *DB) migrator() (*migrate.Migrate, error) {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("opening embedded migrations: %w", err)
	}
	drv, err := migratepg.WithInstance(d.sql, &migratepg.Config{})
	if err != nil {
		return nil, fmt.Errorf("preparing migration driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", drv)
	if err != nil {
		return nil, fmt.Errorf("building migrator: %w", err)
	}
	return m, nil
}

// Migrate applies every pending migration. Already-current is not an error.
func (d *DB) Migrate() error {
	m, err := d.migrator()
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}

// MigrateDown rolls back the most recent migration. Development only.
func (d *DB) MigrateDown() error {
	m, err := d.migrator()
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("rolling back migration: %w", err)
	}
	return nil
}

// Version reports the applied migration version and whether the schema is
// dirty, which means a migration failed partway and needs manual repair.
func (d *DB) Version() (version uint, dirty bool, err error) {
	m, err := d.migrator()
	if err != nil {
		return 0, false, err
	}
	defer m.Close()

	version, dirty, err = m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("reading migration version: %w", err)
	}
	return version, dirty, nil
}
