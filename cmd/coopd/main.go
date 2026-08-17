// Command coopd is the Coop backend server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/api"
	"github.com/nerdswhofish/coop/internal/auth"
	"github.com/nerdswhofish/coop/internal/cleanup"
	"github.com/nerdswhofish/coop/internal/config"
	"github.com/nerdswhofish/coop/internal/crypto"
	"github.com/nerdswhofish/coop/internal/feed"
	"github.com/nerdswhofish/coop/internal/ingest"
	"github.com/nerdswhofish/coop/internal/ota"
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
  healthcheck    Check the local server readiness endpoint
  auth-reset-totp --email ADDRESS
                 Clear TOTP enrollment and revoke every parent session
  auth-unlock --email ADDRESS
                 Clear the email-specific parent login throttle
  version        Print build information

Flags:
  -config PATH   Path to a TOML config file (optional)

Configuration is read from the config file if given, then overlaid by
COOP_-prefixed environment variables. See docs/DEPLOYMENT.md.
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
	if command == "healthcheck" {
		return runHealthcheck()
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

	case "auth-reset-totp":
		email, err := recoveryEmail(command, flag.Args()[1:])
		if err != nil {
			return err
		}
		if err := store.NewAccounts(db, time.Now).ResetParentTOTP(ctx, email); err != nil {
			return err
		}
		logger.Info("parent TOTP reset and sessions revoked", "email", email)
		return nil

	case "auth-unlock":
		email, err := recoveryEmail(command, flag.Args()[1:])
		if err != nil {
			return err
		}
		emailKey := auth.HashToken("parent-email:" + email)
		if err := store.NewAccounts(db, time.Now).UnlockParentLogin(ctx, email, emailKey); err != nil {
			return err
		}
		logger.Info("parent login unlocked", "email", email)
		return nil

	case "serve":
		return serve(ctx, cfg, db, logger)

	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func recoveryEmail(command string, args []string) (string, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	email := flags.String("email", "", "parent email address")
	if err := flags.Parse(args); err != nil {
		return "", fmt.Errorf("%s: %w", command, err)
	}
	normalized := strings.ToLower(strings.TrimSpace(*email))
	if normalized == "" || flags.NArg() != 0 {
		return "", fmt.Errorf("usage: coopd [global flags] %s --email ADDRESS", command)
	}
	return normalized, nil
}

func runHealthcheck() error {
	url := os.Getenv("COOP_HEALTHCHECK_URL")
	if url == "" {
		url = "http://127.0.0.1:8080/readyz"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: %s returned %s", url, response.Status)
	}
	return nil
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
	audit := store.NewAudit(db)
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
	cleaner, err := cleanup.New(accounts, cache, quota, activity, logger, time.Now)
	if err != nil {
		return err
	}
	var installer http.Handler
	if cfg.OTA.Enabled {
		installer, err = ota.New(cfg.Server.PublicURL, cfg.OTA.Directory)
		if err != nil {
			return err
		}
		logger.Info("OTA installer enabled", "path", "/install/", "directory", cfg.OTA.Directory)
	}

	api, err := api.NewServer(api.Deps{
		Config:    cfg,
		Logger:    logger,
		Accounts:  accounts,
		Rules:     rules,
		Catalog:   catalog,
		Activity:  activity,
		Audit:     audit,
		Feed:      feed.New(catalog, rules, activity),
		Quota:     quota,
		Sealer:    sealer,
		YouTube:   youtubeClients,
		DB:        db,
		Now:       time.Now,
		Installer: installer,
	})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           api.Handler(),
		ReadTimeout:       cfg.Server.ReadTimeout,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
		MaxHeaderBytes:    cfg.Server.MaxHeaderBytes,
	}

	errCh := make(chan error, 1)
	backgroundCtx, stopBackground := context.WithCancel(ctx)
	var background sync.WaitGroup
	startBackground := func(run func(context.Context)) {
		background.Add(1)
		go func() {
			defer background.Done()
			run(backgroundCtx)
		}()
	}
	startBackground(ingester.Run)
	startBackground(cleaner.Run)
	defer func() {
		stopBackground()
		background.Wait()
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
