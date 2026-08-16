package cleanup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeStores struct {
	cacheCount   int64
	sessionCount int64
	pairingCount int64
	quotaCount   int64
	searchCount  int64
	cacheErr     error
	quotaDay     string
	searchDay    string
	called       []string
}

func (f *fakeStores) PurgeExpired(context.Context) (int64, error) {
	f.called = append(f.called, "cache")
	return f.cacheCount, f.cacheErr
}

func (f *fakeStores) PurgeExpiredSessions(context.Context) (int64, error) {
	f.called = append(f.called, "sessions")
	return f.sessionCount, nil
}

func (f *fakeStores) PurgeExpiredPairingCodes(context.Context) (int64, error) {
	f.called = append(f.called, "pairing")
	return f.pairingCount, nil
}

func (f *fakeStores) PurgeBefore(_ context.Context, day string) (int64, error) {
	f.called = append(f.called, "quota")
	f.quotaDay = day
	return f.quotaCount, nil
}

func (f *fakeStores) PurgeSearchesBefore(_ context.Context, day string) (int64, error) {
	f.called = append(f.called, "searches")
	f.searchDay = day
	return f.searchCount, nil
}

func newService(t *testing.T, stores *fakeStores, now time.Time) *Service {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := New(stores, stores, stores, stores, logger, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestCleanPurgesEveryBoundedTable(t *testing.T) {
	stores := &fakeStores{
		cacheCount:   1,
		sessionCount: 2,
		pairingCount: 3,
		quotaCount:   4,
		searchCount:  5,
	}
	now := time.Date(2026, 8, 16, 6, 30, 0, 0, time.UTC)

	stats, err := newService(t, stores, now).Clean(context.Background())
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	want := Stats{Cache: 1, Sessions: 2, PairingCodes: 3, Quota: 4, Searches: 5}
	if stats != want {
		t.Errorf("Clean() stats = %+v, want %+v", stats, want)
	}
	if stores.quotaDay != "2026-08-15" || stores.searchDay != "2026-08-15" {
		t.Errorf("ledger cutoff = (%q, %q), want Pacific quota day 2026-08-15",
			stores.quotaDay, stores.searchDay)
	}
	if len(stores.called) != 5 {
		t.Errorf("cleanup calls = %v, want all five", stores.called)
	}
}

func TestCleanContinuesAfterFailure(t *testing.T) {
	wantErr := errors.New("database unavailable")
	stores := &fakeStores{cacheErr: wantErr, sessionCount: 2, pairingCount: 3, quotaCount: 4, searchCount: 5}

	stats, err := newService(t, stores, time.Now()).Clean(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Clean() error = %v, want cache failure", err)
	}
	if stats.Sessions != 2 || stats.PairingCodes != 3 || stats.Quota != 4 || stats.Searches != 5 {
		t.Errorf("Clean() stopped after a failure: %+v", stats)
	}
	if len(stores.called) != 5 {
		t.Errorf("cleanup calls = %v, want all five after a failure", stores.called)
	}
}

func TestRunStopsWithContext(t *testing.T) {
	stores := &fakeStores{}
	svc := newService(t, stores, time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.Run(ctx)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after cancellation")
	}
}
