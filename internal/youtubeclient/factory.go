// Package youtubeclient constructs family-scoped YouTube clients.
package youtubeclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/config"
	"github.com/nerdswhofish/coop/internal/store"
	"github.com/nerdswhofish/coop/internal/youtube"
)

// ErrNoAPIKey reports that a family has not configured YouTube yet.
var ErrNoAPIKey = errors.New("family has no YouTube API key")

type familyStore interface {
	Family(context.Context, uuid.UUID) (store.Family, error)
}

type keyOpener interface {
	OpenString([]byte) (string, error)
}

// Factory owns the shared client configuration while keeping keys scoped to
// the family selected for each operation.
type Factory struct {
	cfg      config.YouTube
	families familyStore
	cache    youtube.Cache
	ledger   youtube.Ledger
	sealer   keyOpener
	now      func() time.Time
}

// NewFactory validates the dependencies used by every family client.
func NewFactory(cfg config.YouTube, families familyStore, cache youtube.Cache,
	ledger youtube.Ledger, sealer keyOpener, now func() time.Time) (*Factory, error) {

	if families == nil {
		return nil, errors.New("youtubeclient: family store is required")
	}
	if cache == nil {
		return nil, errors.New("youtubeclient: cache is required")
	}
	if ledger == nil {
		return nil, errors.New("youtubeclient: ledger is required")
	}
	if sealer == nil {
		return nil, errors.New("youtubeclient: sealer is required")
	}
	if now == nil {
		now = time.Now
	}

	return &Factory{
		cfg:      cfg,
		families: families,
		cache:    cache,
		ledger:   ledger,
		sealer:   sealer,
		now:      now,
	}, nil
}

// ForFamily decrypts the family's current key for one client lifetime.
func (f *Factory) ForFamily(ctx context.Context, familyID uuid.UUID) (*youtube.Client, error) {
	family, err := f.families.Family(ctx, familyID)
	if err != nil {
		return nil, err
	}
	if len(family.EncryptedAPIKey) == 0 {
		return nil, ErrNoAPIKey
	}

	apiKey, err := f.sealer.OpenString(family.EncryptedAPIKey)
	if err != nil {
		return nil, fmt.Errorf("opening YouTube API key: %w", err)
	}
	return f.newClient(familyID, apiKey, f.cache)
}

// ForAPIKey builds a client for a candidate key, which lets setup validate a
// replacement before encrypting and storing it. Candidate validation bypasses
// shared response cache so an old valid response cannot bless a bad new key.
func (f *Factory) ForAPIKey(familyID uuid.UUID, apiKey string) (*youtube.Client, error) {
	return f.newClient(familyID, apiKey, discardCache{})
}

func (f *Factory) newClient(familyID uuid.UUID, apiKey string, cache youtube.Cache) (*youtube.Client, error) {
	return youtube.New(youtube.Config{
		APIKey:   apiKey,
		FamilyID: familyID,
		Cache:    cache,
		Ledger:   f.ledger,
		Budget: youtube.Budget{
			Units:    f.cfg.DailyUnitBudget,
			Searches: f.cfg.DailySearchBudget,
			Backfill: f.cfg.BackfillCallBudget,
		},
		TTLs: youtube.TTLs{
			Default: f.cfg.CacheTTLDefault,
			Channel: 30 * 24 * time.Hour,
			Uploads: f.cfg.UploadsRefreshInterval,
			Video:   30 * 24 * time.Hour,
			Feed:    f.cfg.UploadsRefreshInterval,
			Search:  24 * time.Hour,
		},
		Now: f.now,
	})
}

type discardCache struct{}

func (discardCache) Get(context.Context, string) ([]byte, bool, error) { return nil, false, nil }
func (discardCache) Put(context.Context, string, string, []byte, time.Duration) error {
	return nil
}
