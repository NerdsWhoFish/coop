package feed

import (
	"reflect"
	"testing"

	"github.com/nerdswhofish/coop/internal/store"
)

func TestVideoDiscoverySeedPrefersUsefulTags(t *testing.T) {
	video := store.Video{
		Title: "How Birds Build Their Nests",
		Tags:  []string{" ", "Birds", "birds", "Nest building", "this tag is far too wordy to be a useful discovery search phrase for a child"},
	}

	got := videoDiscoverySeed(video, "Because you liked ")
	want := DiscoverySeed{
		Query:  "Birds Nest building",
		Reason: "Because you liked How Birds Build Their Nests",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("videoDiscoverySeed() = %+v, want %+v", got, want)
	}
}

func TestVideoDiscoverySeedFallsBackToTitle(t *testing.T) {
	video := store.Video{Title: "Why Volcanoes Erupt", Tags: []string{"", "an"}}

	got := videoDiscoverySeed(video, "Because you finished ")
	if got.Query != video.Title {
		t.Errorf("Query = %q, want title %q", got.Query, video.Title)
	}
}

func TestUniqueDiscoverySeedsNormalizesQueries(t *testing.T) {
	got := uniqueDiscoverySeeds([]DiscoverySeed{
		{Query: "  Birds   and nests ", Reason: "first"},
		{Query: "birds and NESTS", Reason: "duplicate"},
		{Query: "", Reason: "empty"},
		{Query: "Volcanoes", Reason: "second"},
	})
	want := []DiscoverySeed{
		{Query: "Birds and nests", Reason: "first"},
		{Query: "Volcanoes", Reason: "second"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("uniqueDiscoverySeeds() = %+v, want %+v", got, want)
	}
}
