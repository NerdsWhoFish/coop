package youtubeclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/config"
	"github.com/nerdswhofish/coop/internal/domain"
	"github.com/nerdswhofish/coop/internal/store"
	"github.com/nerdswhofish/coop/internal/youtube"
)

type fakeFamilyStore struct {
	family store.Family
	err    error
}

func (f *fakeFamilyStore) Family(context.Context, uuid.UUID) (store.Family, error) {
	return f.family, f.err
}

type fakeKeyOpener struct {
	key string
	err error
}

func (f *fakeKeyOpener) OpenString([]byte) (string, error) { return f.key, f.err }

type fakeCache struct{}

func (*fakeCache) Get(context.Context, string) ([]byte, bool, error) { return nil, false, nil }
func (*fakeCache) Put(context.Context, string, string, []byte, time.Duration) error {
	return nil
}

type fakeLedger struct{}

func (*fakeLedger) Record(context.Context, uuid.UUID, string, domain.QuotaPurpose, int, int) error {
	return nil
}

func (*fakeLedger) Usage(context.Context, uuid.UUID, string) (map[domain.QuotaPurpose]youtube.Spend, error) {
	return map[domain.QuotaPurpose]youtube.Spend{}, nil
}

func factoryConfig() config.YouTube {
	return config.YouTube{
		DailyUnitBudget:        8000,
		DailySearchBudget:      90,
		BackfillCallBudget:     500,
		CacheTTLDefault:        time.Hour,
		UploadsRefreshInterval: 6 * time.Hour,
	}
}

func TestForFamilyUsesCurrentStoredKey(t *testing.T) {
	families := &fakeFamilyStore{family: store.Family{EncryptedAPIKey: []byte("sealed")}}
	factory, err := NewFactory(factoryConfig(), families, &fakeCache{}, &fakeLedger{},
		&fakeKeyOpener{key: "api-key"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	client, err := factory.ForFamily(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("ForFamily() error = %v", err)
	}
	if got := client.Budget(); got.Units != 8000 || got.Searches != 90 || got.Backfill != 500 {
		t.Errorf("Budget() = %+v, want configured limits", got)
	}
}

func TestForFamilyRequiresAStoredKey(t *testing.T) {
	factory, err := NewFactory(factoryConfig(), &fakeFamilyStore{}, &fakeCache{},
		&fakeLedger{}, &fakeKeyOpener{}, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = factory.ForFamily(context.Background(), uuid.New())
	if !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("ForFamily() error = %v, want ErrNoAPIKey", err)
	}
}

func TestForFamilySurfacesDecryptionFailure(t *testing.T) {
	want := errors.New("wrong encryption key")
	factory, err := NewFactory(factoryConfig(),
		&fakeFamilyStore{family: store.Family{EncryptedAPIKey: []byte("sealed")}},
		&fakeCache{}, &fakeLedger{}, &fakeKeyOpener{err: want}, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = factory.ForFamily(context.Background(), uuid.New())
	if !errors.Is(err, want) {
		t.Fatalf("ForFamily() error = %v, want decryption failure", err)
	}
}

func TestForAPIKeyRejectsBlankKey(t *testing.T) {
	factory, err := NewFactory(factoryConfig(), &fakeFamilyStore{}, &fakeCache{},
		&fakeLedger{}, &fakeKeyOpener{}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := factory.ForAPIKey(uuid.New(), ""); err == nil {
		t.Fatal("ForAPIKey() with a blank key = nil, want error")
	}
}
