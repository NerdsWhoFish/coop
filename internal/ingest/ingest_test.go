package ingest

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/domain"
	"github.com/nerdswhofish/coop/internal/store"
	"github.com/nerdswhofish/coop/internal/youtube"
)

type fakeFamilies struct {
	rows []store.Family
	err  error
}

func (f *fakeFamilies) FamiliesWithAPIKeys(context.Context) ([]store.Family, error) {
	return f.rows, f.err
}

type staleCall struct {
	familyID uuid.UUID
	before   time.Time
	limit    int
}

type fakeCatalog struct {
	stale       []string
	staleErr    error
	staleCalls  []staleCall
	channels    []youtube.Channel
	videos      []youtube.Video
	feedEntries []youtube.FeedEntry
	refreshed   []string
}

func (f *fakeCatalog) StaleApprovedChannelIDs(_ context.Context, familyID uuid.UUID,
	before time.Time, limit int) ([]string, error) {
	f.staleCalls = append(f.staleCalls, staleCall{familyID: familyID, before: before, limit: limit})
	return f.stale, f.staleErr
}

func (f *fakeCatalog) UpsertChannels(_ context.Context, channels []youtube.Channel) error {
	f.channels = append(f.channels, channels...)
	return nil
}

func (f *fakeCatalog) UpsertVideos(_ context.Context, videos []youtube.Video) error {
	f.videos = append(f.videos, videos...)
	return nil
}

func (f *fakeCatalog) ApplyFeedClassification(_ context.Context, entries []youtube.FeedEntry) error {
	f.feedEntries = append(f.feedEntries, entries...)
	return nil
}

func (f *fakeCatalog) MarkChannelRefreshed(_ context.Context, channelID string) error {
	f.refreshed = append(f.refreshed, channelID)
	return nil
}

type fakeClient struct {
	channels       []youtube.Channel
	uploads        map[string][]string
	videos         []youtube.Video
	feeds          map[string][]youtube.FeedEntry
	uploadErrors   map[string]error
	channelBatches [][]string
	uploadCalls    []string
	purposes       []domain.QuotaPurpose
}

func (f *fakeClient) Channels(_ context.Context, ids []string,
	purpose domain.QuotaPurpose) ([]youtube.Channel, error) {
	f.channelBatches = append(f.channelBatches, append([]string(nil), ids...))
	f.purposes = append(f.purposes, purpose)
	return f.channels, nil
}

func (f *fakeClient) UploadIDs(_ context.Context, channelID string, _ int,
	purpose domain.QuotaPurpose) ([]string, error) {
	f.uploadCalls = append(f.uploadCalls, channelID)
	f.purposes = append(f.purposes, purpose)
	return f.uploads[channelID], f.uploadErrors[channelID]
}

func (f *fakeClient) Videos(_ context.Context, _ []string,
	purpose domain.QuotaPurpose) ([]youtube.Video, error) {
	f.purposes = append(f.purposes, purpose)
	return f.videos, nil
}

func (f *fakeClient) ChannelFeed(_ context.Context, channelID string) ([]youtube.FeedEntry, error) {
	return f.feeds[channelID], nil
}

func testService(t *testing.T, families *fakeFamilies, catalog *fakeCatalog,
	client *fakeClient, now time.Time) *Service {
	t.Helper()
	svc, err := New(families, catalog,
		func(context.Context, uuid.UUID) (Client, error) { return client, nil },
		time.Minute, 6*time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)),
		func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestRefreshIngestsApprovedChannels(t *testing.T) {
	familyID := uuid.New()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	channel := youtube.Channel{ID: "UCabcdefghijklmnopqrstuv", Title: "Cooper"}
	video := youtube.Video{ID: "video-1", ChannelID: channel.ID, Title: "First"}
	entry := youtube.FeedEntry{VideoID: video.ID, IsShort: true}

	families := &fakeFamilies{rows: []store.Family{{ID: familyID}}}
	catalog := &fakeCatalog{stale: []string{channel.ID}}
	client := &fakeClient{
		channels: []youtube.Channel{channel},
		uploads:  map[string][]string{channel.ID: {video.ID}},
		videos:   []youtube.Video{video},
		feeds:    map[string][]youtube.FeedEntry{channel.ID: {entry}},
	}

	if err := testService(t, families, catalog, client, now).Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(catalog.channels) != 1 || catalog.channels[0].ID != channel.ID {
		t.Fatalf("upserted channels = %+v, want %s", catalog.channels, channel.ID)
	}
	if len(catalog.videos) != 1 || catalog.videos[0].ID != video.ID {
		t.Fatalf("upserted videos = %+v, want %s", catalog.videos, video.ID)
	}
	if len(catalog.feedEntries) != 1 || catalog.feedEntries[0].VideoID != video.ID {
		t.Fatalf("feed entries = %+v, want %s", catalog.feedEntries, video.ID)
	}
	if len(catalog.refreshed) != 1 || catalog.refreshed[0] != channel.ID {
		t.Fatalf("refreshed channels = %v, want %s", catalog.refreshed, channel.ID)
	}
	if got := catalog.staleCalls[0].before; !got.Equal(now.Add(-6 * time.Hour)) {
		t.Errorf("stale cutoff = %s, want %s", got, now.Add(-6*time.Hour))
	}
	for _, purpose := range client.purposes {
		if purpose != domain.PurposeFeed {
			t.Errorf("purpose = %q, want feed", purpose)
		}
	}
}

func TestRefreshSkipsClientWhenNothingIsStale(t *testing.T) {
	familyID := uuid.New()
	families := &fakeFamilies{rows: []store.Family{{ID: familyID}}}
	catalog := &fakeCatalog{}
	called := false

	svc, err := New(families, catalog,
		func(context.Context, uuid.UUID) (Client, error) {
			called = true
			return &fakeClient{}, nil
		}, time.Minute, time.Hour, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("client factory was called with no stale channels")
	}
}

func TestRefreshContinuesAfterOneChannelFails(t *testing.T) {
	familyID := uuid.New()
	first := "UCabcdefghijklmnopqrstuv"
	second := "UCzyxwvutsrqponmlkjihgfe"
	families := &fakeFamilies{rows: []store.Family{{ID: familyID}}}
	catalog := &fakeCatalog{stale: []string{first, second}}
	client := &fakeClient{
		channels: []youtube.Channel{{ID: first}, {ID: second}},
		uploads:  map[string][]string{second: {"video-2"}},
		videos:   []youtube.Video{{ID: "video-2", ChannelID: second}},
		feeds:    map[string][]youtube.FeedEntry{second: {}},
		uploadErrors: map[string]error{
			first: errors.New("temporary failure"),
		},
	}

	err := testService(t, families, catalog, client, time.Now()).Refresh(context.Background())
	if err == nil || !errors.Is(err, client.uploadErrors[first]) {
		t.Fatalf("Refresh() error = %v, want the first channel failure", err)
	}
	if len(client.uploadCalls) != 2 || client.uploadCalls[1] != second {
		t.Fatalf("upload calls = %v, want both channels", client.uploadCalls)
	}
	if len(catalog.videos) != 1 || catalog.videos[0].ChannelID != second {
		t.Fatalf("upserted videos = %+v, want the second channel", catalog.videos)
	}
	if len(catalog.refreshed) != 1 || catalog.refreshed[0] != second {
		t.Fatalf("refreshed channels = %v, want only the second channel", catalog.refreshed)
	}
}

func TestRefreshStopsAtBudgetExhaustion(t *testing.T) {
	familyID := uuid.New()
	first := "UCabcdefghijklmnopqrstuv"
	second := "UCzyxwvutsrqponmlkjihgfe"
	families := &fakeFamilies{rows: []store.Family{{ID: familyID}}}
	catalog := &fakeCatalog{stale: []string{first, second}}
	client := &fakeClient{
		channels: []youtube.Channel{{ID: first}, {ID: second}},
		uploads:  map[string][]string{},
		feeds:    map[string][]youtube.FeedEntry{},
		uploadErrors: map[string]error{
			first: youtube.ErrBudgetExhausted,
		},
	}

	if err := testService(t, families, catalog, client, time.Now()).Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v, want quota exhaustion treated as a clean pause", err)
	}
	if len(client.uploadCalls) != 1 || client.uploadCalls[0] != first {
		t.Fatalf("upload calls = %v, want to stop after the first", client.uploadCalls)
	}
}
