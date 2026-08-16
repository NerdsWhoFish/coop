package youtube

import (
	"strings"
	"testing"
	"time"

	"github.com/nerdswhofish/coop/internal/domain"
)

func TestUploadsPlaylistID(t *testing.T) {
	tests := []struct {
		name    string
		channel string
		want    string
		wantErr bool
	}{
		{name: "valid", channel: "UCabcdefghijklmnopqrstuv", want: "UUabcdefghijklmnopqrstuv"},
		{name: "with dashes and underscores", channel: "UC_x5XG1OV2P6uZZ5FSM9Ttw", want: "UU_x5XG1OV2P6uZZ5FSM9Ttw"},
		{name: "wrong prefix", channel: "UXabcdefghijklmnopqrstuv", wantErr: true},
		{name: "too short", channel: "UCabc", wantErr: true},
		{name: "too long", channel: "UCabcdefghijklmnopqrstuvwx", wantErr: true},
		{name: "empty", channel: "", wantErr: true},
		{name: "playlist id is not a channel id", channel: "UUabcdefghijklmnopqrstuv", wantErr: true},
		{name: "illegal character", channel: "UCabcdefghijklmnopqrst!v", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UploadsPlaylistID(tt.channel)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("UploadsPlaylistID(%q) = %q, want an error", tt.channel, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("UploadsPlaylistID(%q) error = %v", tt.channel, err)
			}
			if got != tt.want {
				t.Errorf("UploadsPlaylistID(%q) = %q, want %q", tt.channel, got, tt.want)
			}
		})
	}
}

func TestClassifyLive(t *testing.T) {
	tests := []struct {
		name       string
		broadcast  string
		hasDetails bool
		want       domain.LiveState
	}{
		{name: "ordinary video", broadcast: "none", want: domain.LiveNone},
		{name: "currently live", broadcast: "live", want: domain.LiveLive},
		{name: "upcoming premiere", broadcast: "upcoming", want: domain.LiveUpcoming},
		// The case a single-field check misses: a finished stream reads as
		// "none" but keeps liveStreamingDetails.
		{name: "finished stream", broadcast: "none", hasDetails: true, want: domain.LiveArchived},
		{name: "live wins over details", broadcast: "live", hasDetails: true, want: domain.LiveLive},
		{name: "upcoming wins over details", broadcast: "upcoming", hasDetails: true, want: domain.LiveUpcoming},
		{name: "empty broadcast field", broadcast: "", want: domain.LiveNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyLive(tt.broadcast, tt.hasDetails)
			if got != tt.want {
				t.Errorf("ClassifyLive(%q, %v) = %q, want %q", tt.broadcast, tt.hasDetails, got, tt.want)
			}
			if got.IsLive() != (tt.want != domain.LiveNone) {
				t.Errorf("IsLive() disagrees with state %q", got)
			}
		})
	}
}

func TestClassifyShortFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{url: "https://www.youtube.com/shorts/abc123", want: true},
		{url: "https://www.youtube.com/watch?v=abc123", want: false},
		{url: "", want: false},
	}
	for _, tt := range tests {
		if got := ClassifyShortFromURL(tt.url); got != tt.want {
			t.Errorf("ClassifyShortFromURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestClassifyShortFromDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want bool
	}{
		{d: 30 * time.Second, want: true},
		{d: 3 * time.Minute, want: true},
		{d: 3*time.Minute + time.Second, want: false},
		{d: time.Hour, want: false},
		// A zero duration means unknown, not "a very short video".
		{d: 0, want: false},
	}
	for _, tt := range tests {
		if got := ClassifyShortFromDuration(tt.d); got != tt.want {
			t.Errorf("ClassifyShortFromDuration(%v) = %v, want %v", tt.d, got, tt.want)
		}
	}
}

func TestParseISODuration(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{in: "PT4M13S", want: 4*time.Minute + 13*time.Second},
		{in: "PT1H2M3S", want: time.Hour + 2*time.Minute + 3*time.Second},
		{in: "PT59S", want: 59 * time.Second},
		{in: "PT2M", want: 2 * time.Minute},
		{in: "PT1H", want: time.Hour},
		{in: "P1DT2H", want: 24*time.Hour + 2*time.Hour},
		{in: "P0D", want: 0},
		{in: "", wantErr: true},
		{in: "4M13S", wantErr: true},
		{in: "PT4X", wantErr: true},
		// Weeks are never emitted for videos, so accepting them would only
		// hide a surprise rather than handle one.
		{in: "P1W", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseISODuration(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseISODuration(%q) = %v, want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseISODuration(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseISODuration(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// The embed must never use youtube.com, or blocking that host on a child's
// device would also break playback inside Coop.
func TestEmbedURLUsesNocookieHost(t *testing.T) {
	got := EmbedURL("abc123", false)
	if !strings.HasPrefix(got, "https://www.youtube-nocookie.com/embed/abc123") {
		t.Errorf("EmbedURL() = %q, want a youtube-nocookie.com embed", got)
	}
	if strings.Contains(got, "autoplay=1") {
		t.Errorf("EmbedURL(autoplay=false) = %q, want no autoplay parameter", got)
	}

	auto := EmbedURL("abc123", true)
	if !strings.Contains(auto, "autoplay=1") {
		t.Errorf("EmbedURL(autoplay=true) = %q, want autoplay=1", auto)
	}
}

func TestShareAndReviewURLs(t *testing.T) {
	if got := WatchURL("abc123"); got != "https://www.youtube.com/watch?v=abc123" {
		t.Errorf("WatchURL() = %q", got)
	}
	if got := ChannelURL("UCabcdefghijklmnopqrstuv"); got != "https://www.youtube.com/channel/UCabcdefghijklmnopqrstuv" {
		t.Errorf("ChannelURL() = %q", got)
	}
}

const sampleFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015"
      xmlns="http://www.w3.org/2005/Atom">
  <title>Example Channel</title>
  <entry>
    <id>yt:video:vid00000001</id>
    <yt:videoId>vid00000001</yt:videoId>
    <yt:channelId>UCabcdefghijklmnopqrstuv</yt:channelId>
    <title>A regular video</title>
    <link rel="alternate" href="https://www.youtube.com/watch?v=vid00000001"/>
    <published>2026-08-01T12:00:00+00:00</published>
  </entry>
  <entry>
    <id>yt:video:vid00000002</id>
    <yt:videoId>vid00000002</yt:videoId>
    <yt:channelId>UCabcdefghijklmnopqrstuv</yt:channelId>
    <title>A short</title>
    <link rel="alternate" href="https://www.youtube.com/shorts/vid00000002"/>
    <published>2026-08-02T12:00:00+00:00</published>
  </entry>
  <entry>
    <id>yt:video:broken</id>
    <title>Entry with no video id</title>
    <link rel="alternate" href="https://www.youtube.com/watch?v=nope"/>
    <published>2026-08-03T12:00:00+00:00</published>
  </entry>
  <entry>
    <id>yt:video:vid00000004</id>
    <yt:videoId>vid00000004</yt:videoId>
    <title>Unparseable timestamp</title>
    <link rel="alternate" href="https://www.youtube.com/watch?v=vid00000004"/>
    <published>not a timestamp</published>
  </entry>
</feed>`

func TestParseFeed(t *testing.T) {
	entries, err := ParseFeed(strings.NewReader(sampleFeed))
	if err != nil {
		t.Fatalf("ParseFeed() error = %v", err)
	}

	// The entry with no video id is skipped; the other three survive.
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	if entries[0].VideoID != "vid00000001" || entries[0].IsShort {
		t.Errorf("entry 0 = %+v, want the regular video not marked as a short", entries[0])
	}
	if entries[0].ChannelID != "UCabcdefghijklmnopqrstuv" {
		t.Errorf("entry 0 channel = %q", entries[0].ChannelID)
	}
	if entries[0].Title != "A regular video" {
		t.Errorf("entry 0 title = %q", entries[0].Title)
	}
	want := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	if !entries[0].Published.Equal(want) {
		t.Errorf("entry 0 published = %v, want %v", entries[0].Published, want)
	}

	// This is the whole point of reading the feed: YouTube states the form.
	if !entries[1].IsShort {
		t.Errorf("entry 1 = %+v, want IsShort from the /shorts/ canonical URL", entries[1])
	}

	// A bad timestamp costs the timestamp, not the entry.
	if entries[2].VideoID != "vid00000004" {
		t.Errorf("entry 2 = %+v, want the entry with the bad timestamp kept", entries[2])
	}
	if !entries[2].Published.IsZero() {
		t.Errorf("entry 2 published = %v, want the zero time", entries[2].Published)
	}
}

func TestParseFeedRejectsGarbage(t *testing.T) {
	if _, err := ParseFeed(strings.NewReader("this is not xml at all <<<")); err == nil {
		t.Fatal("ParseFeed() = nil error, want a decode failure")
	}
}

func TestParseFeedEmpty(t *testing.T) {
	entries, err := ParseFeed(strings.NewReader(`<feed xmlns="http://www.w3.org/2005/Atom"></feed>`))
	if err != nil {
		t.Fatalf("ParseFeed() error = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}
