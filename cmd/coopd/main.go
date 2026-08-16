// Command coopd is the Coop backend server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/api"
	"github.com/nerdswhofish/coop/internal/config"
	"github.com/nerdswhofish/coop/internal/crypto"
	"github.com/nerdswhofish/coop/internal/feed"
	"github.com/nerdswhofish/coop/internal/ingest"
	"github.com/nerdswhofish/coop/internal/store"
	"github.com/nerdswhofish/coop/internal/version"
	"github.com/nerdswhofish/coop/internal/youtubeclient"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "coopd: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `coopd - Coop backend server

Usage:
  coopd [flags] [command]

Commands:
  serve          Run the HTTP server (default)
  migrate        Apply pending database migrations
  migrate-down   Roll back the most recent migration
  version        Print build information

Flags:
  -config PATH   Path to a TOML config file (optional)

Configuration is read from the config file if given, then overlaid by
COOP_-prefixed environment variables. See docs/PLAN.md.
`)
}

func run() error {
	configPath := flag.String("config", os.Getenv("COOP_CONFIG"), "path to a TOML config file")
	flag.Usage = usage
	flag.Parse()

	command := flag.Arg(0)
	if command == "" {
		command = "serve"
	}
	if command == "version" {
		fmt.Println(version.String())
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	logger := newLogger(cfg.Log)
	slog.SetDefault(logger)

	// Signals cancel this context, which unwinds every command below.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.Database, false)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	switch command {
	case "migrate":
		if err := db.Migrate(); err != nil {
			return err
		}
		return logVersion(db, logger, "migrations applied")

	case "migrate-down":
		if err := db.MigrateDown(); err != nil {
			return err
		}
		return logVersion(db, logger, "migration rolled back")

	case "serve":
		return serve(ctx, cfg, db, logger)

	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

// logVersion reports the schema version after a migration command. A dirty
// schema means a migration failed partway and needs manual repair, so it is
// surfaced loudly rather than folded into the success line.
func logVersion(db *store.DB, logger *slog.Logger, msg string) error {
	v, dirty, err := db.Version()
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("schema is dirty at version %d, manual repair required", v)
	}
	logger.Info(msg, "version", v)
	return nil
}

func serve(ctx context.Context, cfg *config.Config, db *store.DB, logger *slog.Logger) error {
	if err := db.Migrate(); err != nil {
		return err
	}
	v, dirty, err := db.Version()
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("schema is dirty at version %d, manual repair required", v)
	}
	logger.Info("schema ready", "version", v)

	key, err := cfg.EncryptionKeyBytes()
	if err != nil {
		return err
	}
	sealer, err := crypto.NewSealer(key)
	if err != nil {
		return err
	}

	accounts := store.NewAccounts(db, time.Now)
	rules := store.NewRules(db, time.Now)
	catalog := store.NewCatalog(db, time.Now)
	activity := store.NewActivity(db, time.Now)
	cache := store.NewAPICacheStore(db, time.Now)
	quota := store.NewQuotaStore(db, time.Now)
	youtubeClients, err := youtubeclient.NewFactory(cfg.YouTube, accounts, cache, quota, sealer, time.Now)
	if err != nil {
		return err
	}
	ingester, err := ingest.New(accounts, catalog,
		func(ctx context.Context, familyID uuid.UUID) (ingest.Client, error) {
			return youtubeClients.ForFamily(ctx, familyID)
		},
		cfg.YouTube.IngestPollInterval, cfg.YouTube.UploadsRefreshInterval, logger, time.Now)
	if err != nil {
		return err
	}

	api, err := api.NewServer(api.Deps{
		Config:   cfg,
		Logger:   logger,
		Accounts: accounts,
		Rules:    rules,
		Catalog:  catalog,
		Activity: activity,
		Feed:     feed.New(catalog, rules, activity),
		Quota:    quota,
		Sealer:   sealer,
		YouTube:  youtubeClients,
		DB:       db,
		Now:      time.Now,
	})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      api.Handler(),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	errCh := make(chan error, 1)
	workerCtx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		ingester.Run(workerCtx)
	}()
	defer func() {
		stopWorker()
		<-workerDone
	}()

	go func() {
		logger.Info("listening", "addr", cfg.Server.Addr, "public_url", cfg.Server.PublicURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down", "timeout", cfg.Server.ShutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	return nil
}

func newLogger(cfg config.Log) *slog.Logger {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	if cfg.Format == "text" {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}
