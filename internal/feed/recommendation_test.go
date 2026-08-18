package feed

import (
	"encoding/base64"
	"testing"

	"github.com/nerdswhofish/coop/internal/rank"
)

func TestRecommendationCursorRoundTrip(t *testing.T) {
	cursor := encodeRecommendationCursor("session-seed", "video:with punctuation")
	seed, videoID, err := decodeRecommendationCursor(cursor)
	if err != nil {
		t.Fatalf("decodeRecommendationCursor() error = %v", err)
	}
	if seed != "session-seed" || videoID != "video:with punctuation" {
		t.Fatalf("cursor = %q %q, want %q %q", seed, videoID, "session-seed", "video:with punctuation")
	}
}

func TestRecommendationCursorRoundTripsEmptySeed(t *testing.T) {
	seed, videoID, err := decodeRecommendationCursor(encodeRecommendationCursor("", "video"))
	if err != nil {
		t.Fatalf("decodeRecommendationCursor() error = %v", err)
	}
	if seed != "" || videoID != "video" {
		t.Fatalf("cursor = %q %q, want empty seed and %q", seed, videoID, "video")
	}
}

func TestRecommendationCursorAcceptsSeedlessLegacyForm(t *testing.T) {
	legacy := base64.RawURLEncoding.EncodeToString([]byte("recommendation:dQw4w9WgXcQ"))
	seed, videoID, err := decodeRecommendationCursor(legacy)
	if err != nil {
		t.Fatalf("decodeRecommendationCursor() error = %v", err)
	}
	if seed != "" || videoID != "dQw4w9WgXcQ" {
		t.Fatalf("cursor = %q %q, want empty seed and the legacy video ID", seed, videoID)
	}
}

func TestRecommendationStartContinuesAfterCursor(t *testing.T) {
	ranked := []rank.Recommendation{
		{Candidate: rank.Candidate{ID: "first"}},
		{Candidate: rank.Candidate{ID: "second"}},
		{Candidate: rank.Candidate{ID: "third"}},
	}

	start, err := recommendationStart(ranked, "second")
	if err != nil {
		t.Fatalf("recommendationStart() error = %v", err)
	}
	if start != 2 {
		t.Fatalf("start = %d, want 2", start)
	}
}

func TestRecommendationStartRejectsStaleCursor(t *testing.T) {
	_, err := recommendationStart(nil, "gone")
	if err == nil {
		t.Fatal("recommendationStart() error = nil, want stale cursor error")
	}
}
