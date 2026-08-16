package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// validKey is 32 zero bytes, base64 encoded.
const validKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

func validConfig() *Config {
	return &Config{
		Server: Server{
			Addr:            ":8080",
			ReadTimeout:     15 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 20 * time.Second,
			PublicURL:       "https://coop.example",
		},
		Database: Database{DSN: "postgres://u:p@localhost:5432/coop"},
		YouTube: YouTube{
			DailyUnitBudget:        8000,
			DailySearchBudget:      90,
			BackfillCallBudget:     500,
			CacheTTLDefault:        time.Hour,
			UploadsRefreshInterval: 6 * time.Hour,
			IngestPollInterval:     time.Minute,
		},
		Auth: Auth{EncryptionKey: validKey},
		Log:  Log{Level: "info", Format: "json"},
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "valid", mutate: func(*Config) {}},
		{
			name:    "missing dsn",
			mutate:  func(c *Config) { c.Database.DSN = "" },
			wantErr: "database.dsn",
		},
		{
			name:    "missing public url",
			mutate:  func(c *Config) { c.Server.PublicURL = "" },
			wantErr: "server.public_url",
		},
		{
			name:    "missing encryption key",
			mutate:  func(c *Config) { c.Auth.EncryptionKey = "" },
			wantErr: "auth.encryption_key",
		},
		{
			name:    "encryption key not base64",
			mutate:  func(c *Config) { c.Auth.EncryptionKey = "not!base64!" },
			wantErr: "not valid base64",
		},
		{
			name:    "encryption key wrong length",
			mutate:  func(c *Config) { c.Auth.EncryptionKey = "c2hvcnQ=" },
			wantErr: "want 32 for AES-256",
		},
		{
			name:    "cache ttl below floor",
			mutate:  func(c *Config) { c.YouTube.CacheTTLDefault = time.Minute },
			wantErr: "below the 1h0m0s floor",
		},
		{
			name:    "search budget above google limit",
			mutate:  func(c *Config) { c.YouTube.DailySearchBudget = 250 },
			wantErr: "above Google's hard limit",
		},
		{
			name:    "unit budget not positive",
			mutate:  func(c *Config) { c.YouTube.DailyUnitBudget = 0 },
			wantErr: "daily_unit_budget must be positive",
		},
		{
			name:    "negative backfill budget",
			mutate:  func(c *Config) { c.YouTube.BackfillCallBudget = -1 },
			wantErr: "backfill_call_budget must not be negative",
		},
		{
			name:    "refresh interval not positive",
			mutate:  func(c *Config) { c.YouTube.UploadsRefreshInterval = 0 },
			wantErr: "uploads_refresh_interval must be positive",
		},
		{
			name:    "ingest poll interval not positive",
			mutate:  func(c *Config) { c.YouTube.IngestPollInterval = 0 },
			wantErr: "ingest_poll_interval must be positive",
		},
		{
			name:    "ingest poll slower than refresh",
			mutate:  func(c *Config) { c.YouTube.IngestPollInterval = 7 * time.Hour },
			wantErr: "ingest_poll_interval must not exceed uploads_refresh_interval",
		},
		{
			name:    "bad log format",
			mutate:  func(c *Config) { c.Log.Format = "yaml" },
			wantErr: "want json or text",
		},
		{
			name:    "bad log level",
			mutate:  func(c *Config) { c.Log.Level = "trace" },
			wantErr: "want debug, info, warn or error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)

			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// One problem per restart is a miserable first run.
func TestValidateReportsAllErrors(t *testing.T) {
	cfg := validConfig()
	cfg.Database.DSN = ""
	cfg.Server.PublicURL = ""
	cfg.Auth.EncryptionKey = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error")
	}
	for _, want := range []string{"database.dsn", "server.public_url", "auth.encryption_key"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() = %q, missing %q", err, want)
		}
	}
}

func TestEncryptionKeyBytes(t *testing.T) {
	cfg := validConfig()
	key, err := cfg.EncryptionKeyBytes()
	if err != nil {
		t.Fatalf("EncryptionKeyBytes() error = %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("len(key) = %d, want 32", len(key))
	}
}

func TestLoadDefaultsFromEnv(t *testing.T) {
	t.Setenv("COOP_DATABASE_DSN", "postgres://u:p@localhost:5432/coop")
	t.Setenv("COOP_PUBLIC_URL", "https://coop.example")
	t.Setenv("COOP_AUTH_ENCRYPTION_KEY", validKey)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", cfg.Server.Addr)
	}
	if cfg.YouTube.DailySearchBudget != 90 {
		t.Errorf("DailySearchBudget = %d, want 90", cfg.YouTube.DailySearchBudget)
	}
	if cfg.YouTube.CacheTTLDefault != time.Hour {
		t.Errorf("CacheTTLDefault = %v, want 1h", cfg.YouTube.CacheTTLDefault)
	}
	if cfg.YouTube.IngestPollInterval != time.Minute {
		t.Errorf("IngestPollInterval = %v, want 1m", cfg.YouTube.IngestPollInterval)
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	t.Setenv("COOP_DATABASE_DSN", "postgres://u:p@localhost:5432/coop")
	t.Setenv("COOP_PUBLIC_URL", "https://coop.example")
	t.Setenv("COOP_AUTH_ENCRYPTION_KEY", validKey)

	if _, err := Load(filepath.Join(t.TempDir(), "absent.toml")); err != nil {
		t.Fatalf("Load() error = %v, want nil for a missing file", err)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coop.toml")
	contents := `
[server]
addr = ":9000"
public_url = "https://from-file.example"

[database]
dsn = "postgres://file@localhost:5432/coop"

[auth]
encryption_key = "` + validKey + `"

[youtube]
cache_ttl_default = "2h"
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("COOP_SERVER_ADDR", ":7000")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Addr != ":7000" {
		t.Errorf("Addr = %q, want :7000 from the environment", cfg.Server.Addr)
	}
	if cfg.Server.PublicURL != "https://from-file.example" {
		t.Errorf("PublicURL = %q, want the file value to survive", cfg.Server.PublicURL)
	}
	if cfg.YouTube.CacheTTLDefault != 2*time.Hour {
		t.Errorf("CacheTTLDefault = %v, want 2h from the file", cfg.YouTube.CacheTTLDefault)
	}
	// Defaults must still apply to keys absent from both file and environment.
	if cfg.YouTube.DailyUnitBudget != 8000 {
		t.Errorf("DailyUnitBudget = %d, want the 8000 default", cfg.YouTube.DailyUnitBudget)
	}
}

func TestLoadRejectsMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(path, []byte("this is not = valid toml ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() = nil, want a parse error")
	}
}
