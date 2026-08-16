package youtube

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/domain"
)

type fakeCache struct {
	entries map[string][]byte
	gets    int
	puts    int
}

func newFakeCache() *fakeCache {
	return &fakeCache{entries: map[string][]byte{}}
}

func (f *fakeCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	f.gets++
	body, ok := f.entries[key]
	return body, ok, nil
}

func (f *fakeCache) Put(_ context.Context, key, _ string, body []byte, _ time.Duration) error {
	f.puts++
	f.entries[key] = body
	return nil
}

type fakeLedger struct {
	usage   map[domain.QuotaPurpose]Spend
	records int
}

func newFakeLedger() *fakeLedger {
	return &fakeLedger{usage: map[domain.QuotaPurpose]Spend{}}
}

func (f *fakeLedger) Record(_ context.Context, _ uuid.UUID, _ string,
	purpose domain.QuotaPurpose, units, calls int) error {
	f.records++
	s := f.usage[purpose]
	s.Units += units
	s.Calls += calls
	f.usage[purpose] = s
	return nil
}

func (f *fakeLedger) Usage(_ context.Context, _ uuid.UUID, _ string) (map[domain.QuotaPurpose]Spend, error) {
	return maps.Clone(f.usage), nil
}

type testHarness struct {
	client *Client
	cache  *fakeCache
	ledger *fakeLedger
	calls  *int
	server *httptest.Server
}

func newHarness(t *testing.T, handler http.HandlerFunc) *testHarness {
	t.Helper()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	cache := newFakeCache()
	ledger := newFakeLedger()

	client, err := New(Config{
		APIKey:      "test-key",
		FamilyID:    uuid.New(),
		Cache:       cache,
		Ledger:      ledger,
		Budget:      Budget{Units: 100, Searches: 10, Backfill: 20},
		HTTP:        srv.Client(),
		Now:         func() time.Time { return time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC) },
		APIBaseURL:  srv.URL,
		FeedBaseURL: srv.URL + "/feeds/videos.xml",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return &testHarness{client: client, cache: cache, ledger: ledger, calls: &calls, server: srv}
}

func jsonHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

const channelsBody = `{"items":[{
  "id":"UCabcdefghijklmnopqrstuv",
  "snippet":{"title":"Example","description":"desc",
    "thumbnails":{"high":{"url":"https://i.ytimg.com/high.jpg"}}},
  "statistics":{"subscriberCount":"1234"},
  "brandingSettings":{"image":{"bannerExternalUrl":"https://i.ytimg.com/banner.jpg"}}
}]}`

func TestChannels(t *testing.T) {
	h := newHarness(t, jsonHandler(channelsBody))

	got, err := h.client.Channels(context.Background(),
		[]string{"UCabcdefghijklmnopqrstuv"}, domain.PurposeFeed)
	if err != nil {
		t.Fatalf("Channels() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d channels, want 1", len(got))
	}

	c := got[0]
	if c.Title != "Example" || c.SubscriberCount != 1234 {
		t.Errorf("channel = %+v", c)
	}
	if c.ThumbnailURL != "https://i.ytimg.com/high.jpg" {
		t.Errorf("thumbnail = %q", c.ThumbnailURL)
	}
	// Derived, never fetched.
	if c.UploadsPlaylistID != "UUabcdefghijklmnopqrstuv" {
		t.Errorf("uploads playlist = %q", c.UploadsPlaylistID)
	}
}

// A cache hit must skip both the network and the budget, or the cache would
// protect latency without protecting quota.
func TestCacheHitSkipsNetworkAndBudget(t *testing.T) {
	h := newHarness(t, jsonHandler(channelsBody))
	ctx := context.Background()
	ids := []string{"UCabcdefghijklmnopqrstuv"}

	if _, err := h.client.Channels(ctx, ids, domain.PurposeFeed); err != nil {
		t.Fatalf("first call error = %v", err)
	}
	if *h.calls != 1 {
		t.Fatalf("http calls after first request = %d, want 1", *h.calls)
	}
	spentAfterFirst := h.ledger.usage[domain.PurposeFeed]

	if _, err := h.client.Channels(ctx, ids, domain.PurposeFeed); err != nil {
		t.Fatalf("second call error = %v", err)
	}
	if *h.calls != 1 {
		t.Errorf("http calls after second request = %d, want it served from cache", *h.calls)
	}
	if h.ledger.usage[domain.PurposeFeed] != spentAfterFirst {
		t.Errorf("spend changed on a cache hit: %+v then %+v",
			spentAfterFirst, h.ledger.usage[domain.PurposeFeed])
	}
}

func TestBudgetExhaustionBlocksTheCall(t *testing.T) {
	h := newHarness(t, jsonHandler(channelsBody))
	h.ledger.usage[domain.PurposeFeed] = Spend{Units: 100, Calls: 100}

	_, err := h.client.Channels(context.Background(),
		[]string{"UCabcdefghijklmnopqrstuv"}, domain.PurposeFeed)
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("error = %v, want ErrBudgetExhausted", err)
	}
	if *h.calls != 0 {
		t.Errorf("http calls = %d, want the request blocked before the network", *h.calls)
	}

	var budgetErr *BudgetError
	if !errors.As(err, &budgetErr) {
		t.Fatal("error does not unwrap to *BudgetError")
	}
	if budgetErr.Limit != 100 {
		t.Errorf("BudgetError.Limit = %d, want 100", budgetErr.Limit)
	}
	if budgetErr.ResetsAt.IsZero() {
		t.Error("BudgetError.ResetsAt is zero, want the next quota reset")
	}
}

// Search has its own bucket metered in calls, so it must not consume units and
// must not be blocked by unit exhaustion.
func TestSearchUsesItsOwnBucket(t *testing.T) {
	h := newHarness(t, jsonHandler(
		`{"items":[{"id":{"channelId":"UCabcdefghijklmnopqrstuv"},
		   "snippet":{"title":"Found","thumbnails":{}}}]}`))
	h.ledger.usage[domain.PurposeFeed] = Spend{Units: 100}

	got, err := h.client.SearchChannels(context.Background(), "example")
	if err != nil {
		t.Fatalf("SearchChannels() error = %v", err)
	}
	if len(got) != 1 || got[0].Title != "Found" {
		t.Fatalf("results = %+v", got)
	}

	spend := h.ledger.usage[domain.PurposeSearch]
	if spend.Calls != 1 {
		t.Errorf("search calls = %d, want 1", spend.Calls)
	}
	if spend.Units != 0 {
		t.Errorf("search units = %d, want 0 since search is metered by call", spend.Units)
	}
}

func TestSearchBudgetIsMeteredInCalls(t *testing.T) {
	h := newHarness(t, jsonHandler(`{"items":[]}`))
	h.ledger.usage[domain.PurposeSearch] = Spend{Calls: 10}

	_, err := h.client.SearchChannels(context.Background(), "example")
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("error = %v, want ErrBudgetExhausted at the call ceiling", err)
	}
}

func TestSearchIgnoresBlankQueries(t *testing.T) {
	h := newHarness(t, jsonHandler(`{"items":[]}`))

	got, err := h.client.SearchChannels(context.Background(), "   ")
	if err != nil {
		t.Fatalf("SearchChannels() error = %v", err)
	}
	if got != nil {
		t.Errorf("results = %+v, want nil", got)
	}
	if *h.calls != 0 {
		t.Errorf("http calls = %d, want a blank query to cost nothing", *h.calls)
	}
}

const videosBody = `{"items":[
 {"id":"vid1","snippet":{"channelId":"UCabcdefghijklmnopqrstuv","channelTitle":"Example",
   "title":"Regular","description":"d","tags":["a","b"],
   "publishedAt":"2026-08-01T12:00:00Z","liveBroadcastContent":"none",
   "thumbnails":{"maxres":{"url":"https://i.ytimg.com/max.jpg"}}},
  "contentDetails":{"duration":"PT10M"},"status":{"madeForKids":true}},
 {"id":"vid2","snippet":{"channelId":"UCabcdefghijklmnopqrstuv","title":"Finished stream",
   "publishedAt":"2026-08-02T12:00:00Z","liveBroadcastContent":"none","thumbnails":{}},
  "contentDetails":{"duration":"PT1H"},"liveStreamingDetails":{}},
 {"id":"vid3","snippet":{"channelId":"UCabcdefghijklmnopqrstuv","title":"Tiny",
   "publishedAt":"2026-08-03T12:00:00Z","liveBroadcastContent":"none","thumbnails":{}},
  "contentDetails":{"duration":"PT45S"}}
]}`

func TestVideos(t *testing.T) {
	h := newHarness(t, jsonHandler(videosBody))

	got, err := h.client.Videos(context.Background(),
		[]string{"vid1", "vid2", "vid3"}, domain.PurposeFeed)
	if err != nil {
		t.Fatalf("Videos() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d videos, want 3", len(got))
	}

	if got[0].Duration != 10*time.Minute || got[0].LiveState != domain.LiveNone {
		t.Errorf("vid1 = %+v", got[0])
	}
	if !got[0].MadeForKids {
		t.Error("vid1 MadeForKids = false, want true")
	}
	if got[0].ThumbnailURL != "https://i.ytimg.com/max.jpg" {
		t.Errorf("vid1 thumbnail = %q, want the maxres one", got[0].ThumbnailURL)
	}

	// The case a single-field live check misses.
	if got[1].LiveState != domain.LiveArchived {
		t.Errorf("vid2 LiveState = %q, want archived", got[1].LiveState)
	}

	if !got[2].IsShort || got[2].ShortSource != domain.ShortSourceDuration {
		t.Errorf("vid3 = %+v, want a duration-sourced short", got[2])
	}
	if got[0].IsShort {
		t.Error("vid1 IsShort = true, want false for a ten minute video")
	}
}

func TestBatchLimitIsEnforced(t *testing.T) {
	h := newHarness(t, jsonHandler(`{"items":[]}`))

	tooMany := make([]string, MaxIDsPerCall+1)
	for i := range tooMany {
		tooMany[i] = "vid"
	}

	if _, err := h.client.Videos(context.Background(), tooMany, domain.PurposeFeed); err == nil {
		t.Error("Videos() with an oversized batch = nil error, want a rejection")
	}
	if _, err := h.client.Channels(context.Background(), tooMany, domain.PurposeFeed); err == nil {
		t.Error("Channels() with an oversized batch = nil error, want a rejection")
	}
	if *h.calls != 0 {
		t.Errorf("http calls = %d, want oversized batches rejected before the network", *h.calls)
	}
}

func TestEmptyBatchesCostNothing(t *testing.T) {
	h := newHarness(t, jsonHandler(`{"items":[]}`))
	ctx := context.Background()

	if got, err := h.client.Videos(ctx, nil, domain.PurposeFeed); err != nil || got != nil {
		t.Errorf("Videos(nil) = (%v, %v)", got, err)
	}
	if got, err := h.client.Channels(ctx, nil, domain.PurposeFeed); err != nil || got != nil {
		t.Errorf("Channels(nil) = (%v, %v)", got, err)
	}
	if *h.calls != 0 {
		t.Errorf("http calls = %d, want 0", *h.calls)
	}
}

// A 403 from Google means different things depending on the reason, and the
// quota case has to reach callers as ErrBudgetExhausted.
func TestQuotaExceededMapsToBudgetError(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"quota","errors":[{"reason":"quotaExceeded"}]}}`))
	})

	_, err := h.client.Channels(context.Background(),
		[]string{"UCabcdefghijklmnopqrstuv"}, domain.PurposeFeed)
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("error = %v, want ErrBudgetExhausted", err)
	}
}

func TestOtherAPIErrorsSurfaceTheReason(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"API key not valid","errors":[{"reason":"badRequest"}]}}`))
	})

	_, err := h.client.Channels(context.Background(),
		[]string{"UCabcdefghijklmnopqrstuv"}, domain.PurposeFeed)
	if err == nil {
		t.Fatal("error = nil, want a failure")
	}
	if errors.Is(err, ErrBudgetExhausted) {
		t.Errorf("error = %v, want it not classified as a budget problem", err)
	}
}

// A failed call must not be cached, or one transient error would persist for
// the whole TTL.
func TestFailedCallsAreNotCached(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	_, _ = h.client.Channels(context.Background(),
		[]string{"UCabcdefghijklmnopqrstuv"}, domain.PurposeFeed)

	if h.cache.puts != 0 {
		t.Errorf("cache puts = %d, want a failed call left uncached", h.cache.puts)
	}
	if h.ledger.records != 0 {
		t.Errorf("ledger records = %d, want no spend recorded for a failed call", h.ledger.records)
	}
}

func TestChannelFeed(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(sampleFeed))
	})
	ctx := context.Background()

	entries, err := h.client.ChannelFeed(ctx, "UCabcdefghijklmnopqrstuv")
	if err != nil {
		t.Fatalf("ChannelFeed() error = %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	// The feed is free, so it spends nothing.
	if h.ledger.records != 0 {
		t.Errorf("ledger records = %d, want the feed to cost no quota", h.ledger.records)
	}

	if _, err := h.client.ChannelFeed(ctx, "UCabcdefghijklmnopqrstuv"); err != nil {
		t.Fatalf("second ChannelFeed() error = %v", err)
	}
	if *h.calls != 1 {
		t.Errorf("http calls = %d, want the second read served from cache", *h.calls)
	}
}

func TestChannelFeedRejectsBadChannelID(t *testing.T) {
	h := newHarness(t, jsonHandler(""))
	if _, err := h.client.ChannelFeed(context.Background(), "nonsense"); err == nil {
		t.Error("ChannelFeed() = nil error, want a rejection")
	}
}

func TestNewRequiresDependencies(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "no api key", cfg: Config{Cache: newFakeCache(), Ledger: newFakeLedger()}},
		{name: "no cache", cfg: Config{APIKey: "k", Ledger: newFakeLedger()}},
		{name: "no ledger", cfg: Config{APIKey: "k", Cache: newFakeCache()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.cfg); err == nil {
				t.Error("New() = nil error, want a validation failure")
			}
		})
	}
}

func TestQuotaDayUsesPacific(t *testing.T) {
	// 06:00 UTC is still the previous day in Pacific, which is when Google
	// resets. Using UTC here would roll the ledger over eight hours early.
	utcMorning := time.Date(2026, 8, 15, 6, 0, 0, 0, time.UTC)
	if got := QuotaDay(utcMorning); got != "2026-08-14" {
		t.Errorf("QuotaDay(%v) = %q, want 2026-08-14", utcMorning, got)
	}

	utcEvening := time.Date(2026, 8, 15, 20, 0, 0, 0, time.UTC)
	if got := QuotaDay(utcEvening); got != "2026-08-15" {
		t.Errorf("QuotaDay(%v) = %q, want 2026-08-15", utcEvening, got)
	}
}

// The server ships in a scratch container with no system tzdata, so the zone
// must come from the embedded database. Without it every ledger key silently
// falls back to fixed PST and drifts by an hour for eight months of the year.
func TestQuotaLocationIsTheRealZone(t *testing.T) {
	if quotaLocation.String() != "America/Los_Angeles" {
		t.Fatalf("quotaLocation = %q, want America/Los_Angeles from embedded tzdata",
			quotaLocation.String())
	}

	// August is PDT, so the offset must be -7 rather than the -8 fallback.
	summer := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC).In(quotaLocation)
	if _, offset := summer.Zone(); offset != -7*60*60 {
		t.Errorf("August offset = %ds, want -25200 (PDT)", offset)
	}
}

func TestNextQuotaResetIsInTheFuture(t *testing.T) {
	now := time.Date(2026, 8, 15, 20, 0, 0, 0, time.UTC)
	reset := NextQuotaReset(now)
	if !reset.After(now) {
		t.Fatalf("NextQuotaReset(%v) = %v, want a later time", now, reset)
	}
	if reset.Sub(now) > 24*time.Hour {
		t.Errorf("NextQuotaReset() is %v away, want within a day", reset.Sub(now))
	}
}

func TestTTLsNeverGoBelowTheFloor(t *testing.T) {
	ttls := TTLs{Default: time.Second, Channel: time.Minute, Search: 48 * time.Hour}

	if got := ttls.For(endpointChannels); got != CacheFloor {
		t.Errorf("For(channels) = %v, want it clamped to %v", got, CacheFloor)
	}
	if got := ttls.For("anything else"); got != CacheFloor {
		t.Errorf("For(unknown) = %v, want the floor", got)
	}
	if got := ttls.For(endpointSearch); got != 48*time.Hour {
		t.Errorf("For(search) = %v, want the configured 48h", got)
	}
}

func TestCacheKeyIsOrderIndependent(t *testing.T) {
	a := url.Values{}
	a.Set("part", "snippet")
	a.Set("id", "abc")

	b := url.Values{}
	b.Set("id", "abc")
	b.Set("part", "snippet")

	if CacheKey(endpointVideos, a) != CacheKey(endpointVideos, b) {
		t.Error("CacheKey() differs by insertion order, want a stable key")
	}
	if CacheKey(endpointVideos, a) == CacheKey(endpointChannels, a) {
		t.Error("CacheKey() collides across endpoints")
	}
}

func TestNormalizeQuery(t *testing.T) {
	tests := []struct{ in, want string }{
		{in: "  Mark   Rober ", want: "mark rober"},
		{in: "MARK ROBER", want: "mark rober"},
		{in: "mark rober", want: "mark rober"},
		{in: "   ", want: ""},
		{in: "", want: ""},
	}
	for _, tt := range tests {
		if got := NormalizeQuery(tt.in); got != tt.want {
			t.Errorf("NormalizeQuery(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
