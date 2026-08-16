package feed

import (
	"fmt"
	"slices"
	"testing"

	"github.com/nerdswhofish/coop/internal/store"
)

func videos(n int) []store.Video {
	out := make([]store.Video, n)
	for i := range out {
		out[i] = store.Video{ID: fmt.Sprintf("vid%03d", i)}
	}
	return out
}

func ids(videos []store.Video) []string {
	out := make([]string, len(videos))
	for i, v := range videos {
		out[i] = v.ID
	}
	return out
}

// A session must keep a stable order, or scrolling back would reshuffle the
// feed under the child.
func TestShuffleIsStableForASeed(t *testing.T) {
	first := videos(50)
	second := videos(50)

	shuffleDeterministically(first, "session-abc")
	shuffleDeterministically(second, "session-abc")

	if !slices.Equal(ids(first), ids(second)) {
		t.Error("the same seed produced different orders")
	}
}

func TestShuffleDiffersBySeed(t *testing.T) {
	first := videos(50)
	second := videos(50)

	shuffleDeterministically(first, "session-abc")
	shuffleDeterministically(second, "session-xyz")

	if slices.Equal(ids(first), ids(second)) {
		t.Error("two seeds produced the same order")
	}
}

// The order must depend only on the seed, not on whatever order the database
// happened to return, or the same session would drift between requests.
func TestShuffleIgnoresInputOrder(t *testing.T) {
	forward := videos(50)
	reversed := videos(50)
	slices.Reverse(reversed)

	shuffleDeterministically(forward, "session-abc")
	shuffleDeterministically(reversed, "session-abc")

	if !slices.Equal(ids(forward), ids(reversed)) {
		t.Error("input order changed the shuffled result")
	}
}

func TestShuffleKeepsEveryVideo(t *testing.T) {
	original := videos(50)
	shuffled := videos(50)
	shuffleDeterministically(shuffled, "session-abc")

	got := ids(shuffled)
	want := ids(original)
	slices.Sort(got)
	slices.Sort(want)

	if !slices.Equal(got, want) {
		t.Error("the shuffle lost or duplicated videos")
	}
}

func TestShuffleActuallyReorders(t *testing.T) {
	shuffled := videos(50)
	shuffleDeterministically(shuffled, "session-abc")

	if slices.Equal(ids(shuffled), ids(videos(50))) {
		t.Error("the shuffle returned the input order unchanged")
	}
}

func TestShuffleHandlesTinyInputs(t *testing.T) {
	for _, n := range []int{0, 1, 2} {
		v := videos(n)
		shuffleDeterministically(v, "seed")
		if len(v) != n {
			t.Errorf("shuffling %d videos produced %d", n, len(v))
		}
	}
}

func TestSelectByIDPreservesOrder(t *testing.T) {
	rows := videos(5)
	served := toPolicyVideos([]store.Video{rows[3], rows[1]})

	got := selectByID(rows, served)
	if len(got) != 2 {
		t.Fatalf("got %d videos, want 2", len(got))
	}
	// Original order, not the order the evaluator returned them in.
	if got[0].ID != "vid001" || got[1].ID != "vid003" {
		t.Errorf("selectByID = %v, want the original ordering", ids(got))
	}
}

func TestSelectByIDEmpty(t *testing.T) {
	if got := selectByID(videos(3), nil); got != nil {
		t.Errorf("selectByID(_, nil) = %v, want nil", got)
	}
}
