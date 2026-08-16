// Package config loads Coop's configuration from an optional TOML file
// overlaid by environment variables, and validates the result.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/caarlos0/env/v11"
)

// CacheFloor is the shortest TTL any cached YouTube response may have. A floor
// rather than a default: one short-TTL call site can drain the daily search
// allocation, which cannot be bought back. See docs/PLAN.md §8.
const CacheFloor = time.Hour

// SearchCallsPerDay is the hard per-project daily limit Google enforces on
// search.list. It is not configurable because it is not ours to change.
const SearchCallsPerDay = 100

// Config is the complete server configuration.
type Config struct {
	Server   Server   `toml:"server"`
	Database Database `toml:"database"`
	YouTube  YouTube  `toml:"youtube"`
	Auth     Auth     `toml:"auth"`
	Log      Log      `toml:"log"`
}

// Server holds HTTP listener settings.
type Server struct {
	Addr              string        `toml:"addr"             env:"SERVER_ADDR"`
	ReadTimeout       time.Duration `toml:"read_timeout"     env:"SERVER_READ_TIMEOUT"`
	ReadHeaderTimeout time.Duration `toml:"read_header_timeout" env:"SERVER_READ_HEADER_TIMEOUT"`
	WriteTimeout      time.Duration `toml:"write_timeout"    env:"SERVER_WRITE_TIMEOUT"`
	IdleTimeout       time.Duration `toml:"idle_timeout"     env:"SERVER_IDLE_TIMEOUT"`
	ShutdownTimeout   time.Duration `toml:"shutdown_timeout" env:"SERVER_SHUTDOWN_TIMEOUT"`
	MaxHeaderBytes    int           `toml:"max_header_bytes" env:"SERVER_MAX_HEADER_BYTES"`

	// PublicURL is the externally reachable base URL. Pairing codes embed it, so
	// a wrong value here produces devices that pair and then reach nothing.
	PublicURL string `toml:"public_url" env:"PUBLIC_URL"`
}

// Database holds Postgres connection settings.
type Database struct {
	DSN             string        `toml:"dsn"               env:"DATABASE_DSN"`
	MaxOpenConns    int           `toml:"max_open_conns"    env:"DATABASE_MAX_OPEN_CONNS"`
	MaxIdleConns    int           `toml:"max_idle_conns"    env:"DATABASE_MAX_IDLE_CONNS"`
	ConnMaxLifetime time.Duration `toml:"conn_max_lifetime" env:"DATABASE_CONN_MAX_LIFETIME"`
}

// YouTube holds quota budgets and cache behaviour. API keys deliberately live
// per-family and encrypted in the database, not here.
type YouTube struct {
	// DailyUnitBudget caps general-endpoint spend. Google allows 10,000; the
	// default leaves headroom so a bug hits our wall first.
	DailyUnitBudget int `toml:"daily_unit_budget" env:"YOUTUBE_DAILY_UNIT_BUDGET"`

	// DailySearchBudget caps search.list, which Google meters in its own bucket
	// of 100/day. Defaulting under that means callers see our error, not theirs.
	DailySearchBudget int `toml:"daily_search_budget" env:"YOUTUBE_DAILY_SEARCH_BUDGET"`

	// BackfillCallBudget caps back-catalog fetching, which only ever spends what
	// the guaranteed reserves leave behind.
	BackfillCallBudget int `toml:"backfill_call_budget" env:"YOUTUBE_BACKFILL_CALL_BUDGET"`

	// CacheTTLDefault applies to any response without a longer specific TTL.
	CacheTTLDefault time.Duration `toml:"cache_ttl_default" env:"YOUTUBE_CACHE_TTL_DEFAULT"`

	// UploadsRefreshInterval is how often subscribed channels are re-polled.
	UploadsRefreshInterval time.Duration `toml:"uploads_refresh_interval" env:"YOUTUBE_UPLOADS_REFRESH_INTERVAL"`

	// IngestPollInterval controls how quickly a newly approved channel is
	// noticed without shortening the quota-bearing uploads refresh interval.
	IngestPollInterval time.Duration `toml:"ingest_poll_interval" env:"YOUTUBE_INGEST_POLL_INTERVAL"`
}

// Auth holds credential and token settings.
type Auth struct {
	// EncryptionKey is a base64-encoded 32 byte key used to encrypt per-family
	// YouTube API keys and TOTP secrets at rest. Losing it means re-entering
	// every family's API key.
	EncryptionKey string `toml:"encryption_key" env:"AUTH_ENCRYPTION_KEY"`

	// ParentSessionTTL bounds a parent's login session.
	ParentSessionTTL time.Duration `toml:"parent_session_ttl" env:"AUTH_PARENT_SESSION_TTL"`

	// ChildTokenTTL bounds a paired child device token. Long by design: a device
	// that silently logs itself out is a support call. Revocation is explicit,
	// from the parent app.
	ChildTokenTTL time.Duration `toml:"child_token_ttl" env:"AUTH_CHILD_TOKEN_TTL"`

	// PairingCodeTTL bounds how long a pairing code stays redeemable.
	PairingCodeTTL time.Duration `toml:"pairing_code_ttl" env:"AUTH_PAIRING_CODE_TTL"`

	// InvitationTTL bounds how long a parent invitation stays redeemable.
	InvitationTTL time.Duration `toml:"invitation_ttl" env:"AUTH_INVITATION_TTL"`
}

// Log holds logging settings.
type Log struct {
	Level  string `toml:"level"  env:"LOG_LEVEL"`
	Format string `toml:"format" env:"LOG_FORMAT"`
}

// Defaults returns the baseline configuration. Not envDefault struct tags:
// those apply whether or not the variable is set, silently overwriting
// config file values.
func Defaults() *Config {
	return &Config{
		Server: Server{
			Addr:              ":8080",
			ReadTimeout:       15 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
			ShutdownTimeout:   20 * time.Second,
			MaxHeaderBytes:    1 << 20,
		},
		Database: Database{
			MaxOpenConns:    25,
			MaxIdleConns:    5,
			ConnMaxLifetime: time.Hour,
		},
		YouTube: YouTube{
			DailyUnitBudget:        8000,
			DailySearchBudget:      90,
			BackfillCallBudget:     500,
			CacheTTLDefault:        CacheFloor,
			UploadsRefreshInterval: 6 * time.Hour,
			IngestPollInterval:     time.Minute,
		},
		Auth: Auth{
			ParentSessionTTL: 30 * 24 * time.Hour,
			ChildTokenTTL:    365 * 24 * time.Hour,
			PairingCodeTTL:   15 * time.Minute,
			InvitationTTL:    7 * 24 * time.Hour,
		},
		Log: Log{Level: "info", Format: "json"},
	}
}

// Load resolves configuration in precedence order: defaults, then the TOML file
// at path, then COOP_-prefixed environment variables. A missing path is not an
// error, since environment-only deployments are a first-class case.
func Load(path string) (*Config, error) {
	cfg := Defaults()

	if path != "" {
		if _, err := os.Stat(path); err == nil {
			if _, err := toml.DecodeFile(path, cfg); err != nil {
				return nil, fmt.Errorf("parsing config file %s: %w", path, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("reading config file %s: %w", path, err)
		}
	}

	if err := overlaySecretFiles(cfg); err != nil {
		return nil, err
	}

	if err := env.ParseWithOptions(cfg, env.Options{Prefix: "COOP_"}); err != nil {
		return nil, fmt.Errorf("parsing environment: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func overlaySecretFiles(cfg *Config) error {
	for _, secret := range []struct {
		envName string
		target  *string
	}{
		{envName: "COOP_DATABASE_DSN_FILE", target: &cfg.Database.DSN},
		{envName: "COOP_AUTH_ENCRYPTION_KEY_FILE", target: &cfg.Auth.EncryptionKey},
	} {
		path := os.Getenv(secret.envName)
		if path == "" {
			continue
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", secret.envName, err)
		}
		*secret.target = strings.TrimRight(string(contents), "\r\n")
	}
	return nil
}

// Validate reports every problem it finds rather than only the first, because
// a first-run operator fixing one variable per restart is a bad experience.
func (c *Config) Validate() error {
	var errs []error

	if c.Database.DSN == "" {
		errs = append(errs, errors.New("database.dsn (COOP_DATABASE_DSN) is required"))
	}
	if c.Server.PublicURL == "" {
		errs = append(errs, errors.New("server.public_url (COOP_PUBLIC_URL) is required, child devices embed it during pairing"))
	}
	if c.Server.ReadHeaderTimeout <= 0 {
		errs = append(errs, errors.New("server.read_header_timeout must be positive"))
	}
	if c.Server.IdleTimeout <= 0 {
		errs = append(errs, errors.New("server.idle_timeout must be positive"))
	}
	if c.Server.MaxHeaderBytes < 1024 {
		errs = append(errs, errors.New("server.max_header_bytes must be at least 1024"))
	}

	if _, err := c.EncryptionKeyBytes(); err != nil {
		errs = append(errs, err)
	}

	if c.YouTube.CacheTTLDefault < CacheFloor {
		errs = append(errs, fmt.Errorf(
			"youtube.cache_ttl_default is %s, which is below the %s floor; see docs/PLAN.md §8",
			c.YouTube.CacheTTLDefault, CacheFloor))
	}
	if c.YouTube.DailySearchBudget > SearchCallsPerDay {
		errs = append(errs, fmt.Errorf(
			"youtube.daily_search_budget is %d, above Google's hard limit of %d per project per day",
			c.YouTube.DailySearchBudget, SearchCallsPerDay))
	}
	if c.YouTube.DailyUnitBudget < 1 {
		errs = append(errs, errors.New("youtube.daily_unit_budget must be positive"))
	}
	if c.YouTube.BackfillCallBudget < 0 {
		errs = append(errs, errors.New("youtube.backfill_call_budget must not be negative"))
	}
	if c.YouTube.UploadsRefreshInterval <= 0 {
		errs = append(errs, errors.New("youtube.uploads_refresh_interval must be positive"))
	}
	if c.YouTube.IngestPollInterval <= 0 {
		errs = append(errs, errors.New("youtube.ingest_poll_interval must be positive"))
	}
	if c.YouTube.IngestPollInterval > c.YouTube.UploadsRefreshInterval {
		errs = append(errs, errors.New("youtube.ingest_poll_interval must not exceed uploads_refresh_interval"))
	}
	if c.Auth.InvitationTTL <= 0 {
		errs = append(errs, errors.New("auth.invitation_ttl must be positive"))
	}

	switch c.Log.Format {
	case "json", "text":
	default:
		errs = append(errs, fmt.Errorf("log.format is %q, want json or text", c.Log.Format))
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("log.level is %q, want debug, info, warn or error", c.Log.Level))
	}

	return errors.Join(errs...)
}

// EncryptionKeyBytes decodes the configured key and checks its length.
func (c *Config) EncryptionKeyBytes() ([]byte, error) {
	if c.Auth.EncryptionKey == "" {
		return nil, errors.New("auth.encryption_key (COOP_AUTH_ENCRYPTION_KEY) is required, generate one with: openssl rand -base64 32")
	}
	key, err := base64.StdEncoding.DecodeString(c.Auth.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("auth.encryption_key is not valid base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("auth.encryption_key decodes to %d bytes, want 32 for AES-256", len(key))
	}
	return key, nil
}
