package feed

import (
	"testing"

	"github.com/nerdswhofish/coop/internal/rank"
)

func TestRecommendationCursorRoundTrip(t *testing.T) {
	cursor := encodeRecommendationCursor("video:with punctuation")
	videoID, err := decodeRecommendationCursor(cursor)
	if err != nil {
		t.Fatalf("decodeRecommendationCursor() error = %v", err)
	}
	if videoID != "video:with punctuation" {
		t.Fatalf("video ID = %q, want %q", videoID, "video:with punctuation")
	}
}

func TestRecommendationStartContinuesAfterCursor(t *testing.T) {
	ranked := []rank.Recommendation{
		{Candidate: rank.Candidate{ID: "first"}},
		{Candidate: rank.Candidate{ID: "second"}},
		{Candidate: rank.Candidate{ID: "third"}},
	}

	start, err := recommendationStart(ranked, encodeRecommendationCursor("second"))
	if err != nil {
		t.Fatalf("recommendationStart() error = %v", err)
	}
	if start != 2 {
		t.Fatalf("start = %d, want 2", start)
	}
}

func TestRecommendationStartRejectsStaleCursor(t *testing.T) {
	_, err := recommendationStart(nil, encodeRecommendationCursor("gone"))
	if err == nil {
		t.Fatal("recommendationStart() error = nil, want stale cursor error")
	}
}
