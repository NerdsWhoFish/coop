package rank

import (
	"testing"
	"time"

	"github.com/nerdswhofish/coop/internal/domain"
)

var testNow = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

func candidate(id, channel string, age time.Duration) Candidate {
	return Candidate{ID: id, ChannelID: channel, PublishedAt: testNow.Add(-age)}
}

func TestExplicitLikeOutweighsInferredSignals(t *testing.T) {
	input := Input{
		Now: testNow,
		Candidates: []Candidate{
			candidate("liked", "quiet", 20*24*time.Hour),
			candidate("inferred", "favourite", time.Hour),
		},
		Reactions:          []Reaction{{VideoID: "liked", Kind: domain.ReactionLike}},
		SubscribedChannels: map[string]bool{"favourite": true},
		Watches: []Watch{
			{VideoID: "inferred", ChannelID: "favourite", StartedAt: testNow, CompletionFraction: 1},
			{VideoID: "inferred", ChannelID: "favourite", StartedAt: testNow, CompletionFraction: 1},
			{VideoID: "inferred", ChannelID: "favourite", StartedAt: testNow, CompletionFraction: 1},
		},
	}

	got := Videos(input)
	if got[0].Candidate.ID != "liked" || got[0].ReasonKind != ReasonLiked {
		t.Fatalf("first recommendation = %+v, want the explicitly liked video", got[0])
	}
}

func TestCompletionBeatsAbandonment(t *testing.T) {
	input := Input{
		Now: testNow,
		Candidates: []Candidate{
			candidate("finished", "one", 24*time.Hour),
			candidate("abandoned", "two", 24*time.Hour),
		},
		Watches: []Watch{
			{VideoID: "finished", ChannelID: "one", StartedAt: testNow.Add(-8 * 24 * time.Hour), CompletionFraction: 1},
			{VideoID: "abandoned", ChannelID: "two", StartedAt: testNow.Add(-8 * 24 * time.Hour), CompletionFraction: 0.05},
		},
	}

	got := Videos(input)
	if got[0].Candidate.ID != "finished" {
		t.Fatalf("first recommendation = %s, want finished", got[0].Candidate.ID)
	}
}

func TestParentChannelWeightChangesOrderAndExplanation(t *testing.T) {
	input := Input{
		Now: testNow,
		Candidates: []Candidate{
			candidate("ordinary", "ordinary", time.Hour),
			candidate("boosted", "boosted", 20*24*time.Hour),
		},
		ChannelWeights: map[string]int{"boosted": 1},
	}

	got := Videos(input)
	if got[0].Candidate.ID != "boosted" || got[0].ReasonKind != ReasonParentMore {
		t.Fatalf("first recommendation = %+v, want parent-boosted", got[0])
	}
}

func TestArrangementCapsConsecutiveChannels(t *testing.T) {
	candidates := []Candidate{
		candidate("a1", "a", time.Hour),
		candidate("a2", "a", 2*time.Hour),
		candidate("a3", "a", 3*time.Hour),
		candidate("b1", "b", 4*time.Hour),
		candidate("a4", "a", 5*time.Hour),
	}
	got := Videos(Input{
		Now:                testNow,
		Candidates:         candidates,
		SubscribedChannels: map[string]bool{"a": true},
		PageSize:           5,
		MaxConsecutive:     2,
	})

	for i := 2; i < len(got); i++ {
		if got[i].Candidate.ChannelID == got[i-1].Candidate.ChannelID &&
			got[i].Candidate.ChannelID == got[i-2].Candidate.ChannelID {
			t.Fatalf("three consecutive recommendations from %q: %+v", got[i].Candidate.ChannelID, got)
		}
	}
}

func TestEachPageReservesUnwatchedAndForgottenContent(t *testing.T) {
	candidates := []Candidate{
		candidate("hot1", "hot", time.Hour),
		candidate("hot2", "hot", 2*time.Hour),
		candidate("hot3", "hot", 3*time.Hour),
		candidate("never", "new", 40*24*time.Hour),
		candidate("forgotten", "old", 41*24*time.Hour),
	}
	watches := []Watch{
		{VideoID: "hot1", ChannelID: "hot", StartedAt: testNow, CompletionFraction: 1},
		{VideoID: "hot2", ChannelID: "hot", StartedAt: testNow, CompletionFraction: 1},
		{VideoID: "hot3", ChannelID: "hot", StartedAt: testNow, CompletionFraction: 1},
		{VideoID: "forgotten", ChannelID: "old", StartedAt: testNow.Add(-30 * 24 * time.Hour), CompletionFraction: 1},
	}

	got := Videos(Input{
		Now:            testNow,
		Candidates:     candidates,
		Watches:        watches,
		PageSize:       4,
		ForgottenAfter: 14 * 24 * time.Hour,
	})
	firstPage := map[string]bool{}
	for _, recommendation := range got[:4] {
		firstPage[recommendation.Candidate.ID] = true
	}
	if !firstPage["never"] || !firstPage["forgotten"] {
		t.Fatalf("first page IDs = %v, want never and forgotten reserved", firstPage)
	}
}

func TestTieBreaksAreDeterministic(t *testing.T) {
	input := Input{
		Now: testNow,
		Candidates: []Candidate{
			candidate("b", "one", time.Hour),
			candidate("a", "two", time.Hour),
		},
	}
	first := Videos(input)
	second := Videos(input)
	for i := range first {
		if first[i].Candidate.ID != second[i].Candidate.ID {
			t.Fatalf("ranking changed between runs: %v then %v", first, second)
		}
	}
}
