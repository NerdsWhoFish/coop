//go:build integration

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/auth"
	"github.com/nerdswhofish/coop/internal/config"
	"github.com/nerdswhofish/coop/internal/crypto"
	"github.com/nerdswhofish/coop/internal/feed"
	"github.com/nerdswhofish/coop/internal/store"
	"github.com/nerdswhofish/coop/internal/youtube"
	"github.com/nerdswhofish/coop/internal/youtubeclient"
)

func TestChildSearchReturnsPolicyFilteredVideos(t *testing.T) {
	const (
		allowedChannel     = "UCaaaaaaaaaaaaaaaaaaaaaa"
		requestableChannel = "UCbbbbbbbbbbbbbbbbbbbbbb"
		blockedChannel     = "UCcccccccccccccccccccccc"
	)

	dsn := os.Getenv("COOP_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("COOP_TEST_DATABASE_DSN not set")
	}

	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	db, err := store.Open(ctx, config.Database{
		DSN:             dsn,
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	clock := func() time.Time { return now }
	accounts := store.NewAccounts(db, clock)
	catalog := store.NewCatalog(db, clock)
	rules := store.NewRules(db, clock)
	activity := store.NewActivity(db, clock)
	cache := store.NewAPICacheStore(db, clock)
	quota := store.NewQuotaStore(db, clock)

	family, parent, err := accounts.CreateFamily(ctx, "Search", "UTC", uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	child, err := accounts.CreateChild(ctx, family.ID, "Cooper", "")
	if err != nil {
		t.Fatal(err)
	}

	channelIDs := []string{allowedChannel, requestableChannel, blockedChannel}
	t.Cleanup(func() {
		if err := db.Delete(&store.AllowGlobal{}, "family_id = ?", family.ID).Error; err != nil {
			t.Errorf("cleaning global allows: %v", err)
		}
		if err := db.Delete(&store.BlockChannel{}, "family_id = ?", family.ID).Error; err != nil {
			t.Errorf("cleaning channel blocks: %v", err)
		}
		if err := db.Delete(&store.Family{}, "id = ?", family.ID).Error; err != nil {
			t.Errorf("cleaning family: %v", err)
		}
		if err := db.Delete(&store.Channel{}, "id IN ?", channelIDs).Error; err != nil {
			t.Errorf("cleaning channels: %v", err)
		}
	})

	channels := []youtube.Channel{
		{ID: allowedChannel, Title: "Allowed"},
		{ID: requestableChannel, Title: "Requestable"},
		{ID: blockedChannel, Title: "Blocked"},
	}
	if err := catalog.UpsertChannels(ctx, channels); err != nil {
		t.Fatal(err)
	}
	if err := rules.AllowGlobally(ctx, family.ID, allowedChannel, parent.ID); err != nil {
		t.Fatal(err)
	}
	if err := rules.BlockChannelForFamily(ctx, family.ID, blockedChannel, "test"); err != nil {
		t.Fatal(err)
	}
	keyword, err := rules.CreateKeyword(ctx, store.Keyword{
		FamilyID:   family.ID,
		Term:       "scary",
		MatchTitle: true,
		MatchTags:  true,
		WholeWord:  true,
	})
	if err != nil {
		t.Fatal(err)
	}

	sealer, err := crypto.NewSealer(make([]byte, crypto.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	sealedKey, err := sealer.SealString("cached-test-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := accounts.SetAPIKey(ctx, family.ID, sealedKey); err != nil {
		t.Fatal(err)
	}

	videoIDs := []string{"allowed-good", "allowed-scary", "requestable-scary", "blocked", "archived", "not-embeddable"}
	putSearchCache(t, cache, channelIDs, videoIDs, allowedChannel, requestableChannel, blockedChannel)

	cfg := config.Defaults()
	cfg.Server.PublicURL = "https://coop.example"
	factory, err := youtubeclient.NewFactory(cfg.YouTube, accounts, cache, quota, sealer, clock)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(Deps{
		Config:   cfg,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Accounts: accounts,
		Rules:    rules,
		Catalog:  catalog,
		Activity: activity,
		Feed:     feed.New(catalog, rules, activity),
		Quota:    quota,
		Sealer:   sealer,
		YouTube:  factory,
		DB:       db,
		Now:      clock,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/v1/child/search?q=Birds+Here", nil)
	recorder := httptest.NewRecorder()
	if err := server.handleChildSearch(recorder, req, auth.Child{ID: child.ID, FamilyID: family.ID}); err != nil {
		t.Fatal(err)
	}

	var body struct {
		Channels []channelDTO `json:"channels"`
		Videos   []videoDTO   `json:"videos"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Channels) != 2 || body.Channels[0].ID != allowedChannel || body.Channels[1].ID != requestableChannel {
		t.Errorf("channels = %+v, want allowed and requestable with blocked omitted", body.Channels)
	}
	if len(body.Videos) != 2 {
		t.Fatalf("videos = %+v, want one playable and one locked", body.Videos)
	}
	if body.Videos[0].ID != "allowed-good" || body.Videos[0].Locked {
		t.Errorf("first video = %+v, want unlocked allowed-good", body.Videos[0])
	}
	if body.Videos[1].ID != "requestable-scary" || !body.Videos[1].Locked {
		t.Errorf("second video = %+v, want locked requestable-scary", body.Videos[1])
	}

	count, err := activity.SearchCount(ctx, child.ID, youtube.QuotaDay(now))
	if err != nil || count != 1 {
		t.Errorf("search count = %d, error = %v, want one", count, err)
	}
	suppressions, err := activity.Suppressions(ctx, child.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(suppressions) != 1 || suppressions[0].VideoID != "allowed-scary" || suppressions[0].KeywordID != keyword.ID {
		t.Errorf("suppressions = %+v, want only allowed-scary", suppressions)
	}
	if _, err := catalog.Video(ctx, "not-embeddable"); err != store.ErrNotFound {
		t.Errorf("non-embeddable video lookup error = %v, want ErrNotFound", err)
	}
}

func putSearchCache(t *testing.T, cache *store.APICacheStore, channelIDs, videoIDs []string,
	allowedChannel, requestableChannel, blockedChannel string) {

	t.Helper()
	ctx := context.Background()
	put := func(endpoint string, params url.Values, body string) {
		t.Helper()
		if err := cache.Put(ctx, youtube.CacheKey(endpoint, params), endpoint, []byte(body), time.Hour); err != nil {
			t.Fatal(err)
		}
	}

	searchParams := url.Values{
		"part":       {"snippet"},
		"type":       {"channel,video"},
		"q":          {"birds here"},
		"maxResults": {"25"},
	}
	put("search.list", searchParams, fmt.Sprintf(`{"items":[
        {"id":{"channelId":%q},"snippet":{}},
        {"id":{"channelId":%q},"snippet":{}},
        {"id":{"channelId":%q},"snippet":{}},
        {"id":{"videoId":"allowed-good"},"snippet":{"channelId":%q}},
        {"id":{"videoId":"allowed-scary"},"snippet":{"channelId":%q}},
        {"id":{"videoId":"requestable-scary"},"snippet":{"channelId":%q}},
        {"id":{"videoId":"blocked"},"snippet":{"channelId":%q}},
        {"id":{"videoId":"archived"},"snippet":{"channelId":%q}},
        {"id":{"videoId":"not-embeddable"},"snippet":{"channelId":%q}}
    ]}`, allowedChannel, requestableChannel, blockedChannel, allowedChannel, allowedChannel,
		requestableChannel, blockedChannel, allowedChannel, allowedChannel))

	channelParams := url.Values{
		"part":       {"snippet,statistics,brandingSettings"},
		"id":         {channelIDs[0] + "," + channelIDs[1] + "," + channelIDs[2]},
		"maxResults": {"50"},
	}
	put("channels.list", channelParams, fmt.Sprintf(`{"items":[
        {"id":%q,"snippet":{"title":"Allowed","thumbnails":{}},"statistics":{},"brandingSettings":{}},
        {"id":%q,"snippet":{"title":"Requestable","thumbnails":{}},"statistics":{},"brandingSettings":{}},
        {"id":%q,"snippet":{"title":"Blocked","thumbnails":{}},"statistics":{},"brandingSettings":{}}
    ]}`, allowedChannel, requestableChannel, blockedChannel))

	videoParams := url.Values{
		"part":       {"snippet,contentDetails,status,liveStreamingDetails"},
		"id":         {videoIDs[0] + "," + videoIDs[1] + "," + videoIDs[2] + "," + videoIDs[3] + "," + videoIDs[4] + "," + videoIDs[5]},
		"maxResults": {"50"},
	}
	video := func(id, channelID, title, status, liveDetails string) string {
		return fmt.Sprintf(`{"id":%q,"snippet":{"channelId":%q,"channelTitle":"Channel","title":%q,"publishedAt":"2026-08-01T12:00:00Z","liveBroadcastContent":"none","thumbnails":{}},"contentDetails":{"duration":"PT5M"},"status":%s%s}`,
			id, channelID, title, status, liveDetails)
	}
	put("videos.list", videoParams, `{"items":[`+
		video("allowed-good", allowedChannel, "Birds", `{"embeddable":true}`, "")+","+
		video("allowed-scary", allowedChannel, "Scary birds", `{"embeddable":true}`, "")+","+
		video("requestable-scary", requestableChannel, "Scary request", `{"embeddable":true}`, "")+","+
		video("blocked", blockedChannel, "Blocked", `{"embeddable":true}`, "")+","+
		video("archived", allowedChannel, "Archived", `{"embeddable":true}`, `,"liveStreamingDetails":{}`)+","+
		video("not-embeddable", allowedChannel, "No embed", `{"embeddable":false}`, "")+
		`]}`)
}
