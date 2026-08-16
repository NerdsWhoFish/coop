package policy

import (
	"testing"

	"github.com/nerdswhofish/coop/internal/domain"
)

const (
	chAllowed = "UCallowed0000000000000000"
	chOther   = "UCother000000000000000000"
)

// Every combination of the four sets, so the algebra in adr/0003 is pinned
// rather than inferred from whichever cases happened to get written.
func TestChannelRulesState(t *testing.T) {
	tests := []struct {
		name    string
		blocked bool
		global  bool
		child   bool
		deny    bool
		want    domain.ChannelState
	}{
		{name: "nothing set", want: domain.StateRequestable},
		{name: "global allow", global: true, want: domain.StateAllowed},
		{name: "child allow", child: true, want: domain.StateAllowed},
		{name: "both allows", global: true, child: true, want: domain.StateAllowed},

		{name: "deny alone", deny: true, want: domain.StateRequestable},
		{name: "deny subtracts global", global: true, deny: true, want: domain.StateRequestable},
		// Contradictory input. Restrictive wins, which is the safe reading.
		{name: "deny subtracts child allow", child: true, deny: true, want: domain.StateRequestable},
		{name: "deny subtracts both", global: true, child: true, deny: true, want: domain.StateRequestable},

		{name: "blocked alone", blocked: true, want: domain.StateBlocked},
		{name: "block beats global", blocked: true, global: true, want: domain.StateBlocked},
		{name: "block beats child", blocked: true, child: true, want: domain.StateBlocked},
		{name: "block beats deny", blocked: true, deny: true, want: domain.StateBlocked},
		{name: "block beats everything", blocked: true, global: true, child: true, deny: true, want: domain.StateBlocked},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r ChannelRules
			if tt.blocked {
				r.Blocked = NewChannelSet(chAllowed)
			}
			if tt.global {
				r.AllowGlobal = NewChannelSet(chAllowed)
			}
			if tt.child {
				r.AllowChild = NewChannelSet(chAllowed)
			}
			if tt.deny {
				r.DenyChild = NewChannelSet(chAllowed)
			}

			if got := r.State(chAllowed); got != tt.want {
				t.Errorf("State() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The zero value must be usable, since a family with no rules yet is the state
// every new install starts in.
func TestZeroRulesAreRequestable(t *testing.T) {
	var r ChannelRules
	if got := r.State(chAllowed); got != domain.StateRequestable {
		t.Errorf("State() = %q, want %q", got, domain.StateRequestable)
	}
}

func allowedRules() ChannelRules {
	return ChannelRules{AllowGlobal: NewChannelSet(chAllowed)}
}

func TestEvaluatorVideo(t *testing.T) {
	scary := Keyword{
		ID: "kw-scary", Term: "scary", Scope: ScopeFamily,
		MatchTitle: true, MatchTags: true, WholeWord: true,
	}
	described := Keyword{
		ID: "kw-desc", Term: "sponsored", Scope: ScopeChild,
		MatchDescription: true, WholeWord: true,
	}

	tests := []struct {
		name      string
		video     Video
		keywords  []Keyword
		overrides []string
		want      Verdict
		wantField domain.MatchField
	}{
		{
			name:  "clean video from an allowed channel",
			video: Video{ID: "v1", ChannelID: chAllowed, Title: "Building a birdhouse"},
			want:  VerdictServe,
		},
		{
			name:  "channel not allowed",
			video: Video{ID: "v1", ChannelID: chOther, Title: "Building a birdhouse"},
			want:  VerdictChannelNotAllowed,
		},
		{
			name:  "live stream",
			video: Video{ID: "v1", ChannelID: chAllowed, LiveState: domain.LiveLive},
			want:  VerdictLive,
		},
		{
			name:  "upcoming premiere",
			video: Video{ID: "v1", ChannelID: chAllowed, LiveState: domain.LiveUpcoming},
			want:  VerdictLive,
		},
		{
			name:  "archived stream still counts as live",
			video: Video{ID: "v1", ChannelID: chAllowed, LiveState: domain.LiveArchived},
			want:  VerdictLive,
		},
		{
			name:      "an override cannot resurrect a livestream",
			video:     Video{ID: "v1", ChannelID: chAllowed, LiveState: domain.LiveArchived},
			overrides: []string{"v1"},
			want:      VerdictLive,
		},
		{
			name:      "keyword hits the title",
			video:     Video{ID: "v1", ChannelID: chAllowed, Title: "A scary monster"},
			keywords:  []Keyword{scary},
			want:      VerdictSuppressed,
			wantField: domain.FieldTitle,
		},
		{
			name:      "keyword hits a tag",
			video:     Video{ID: "v1", ChannelID: chAllowed, Title: "Fine", Tags: []string{"kids", "Scary"}},
			keywords:  []Keyword{scary},
			want:      VerdictSuppressed,
			wantField: domain.FieldTags,
		},
		{
			name: "description is not searched unless enabled",
			video: Video{
				ID: "v1", ChannelID: chAllowed,
				Title: "Fine", Description: "This is a scary description",
			},
			keywords: []Keyword{scary},
			want:     VerdictServe,
		},
		{
			name: "description is searched when enabled",
			video: Video{
				ID: "v1", ChannelID: chAllowed,
				Title: "Fine", Description: "This video is sponsored by someone",
			},
			keywords:  []Keyword{described},
			want:      VerdictSuppressed,
			wantField: domain.FieldDescription,
		},
		{
			name:      "an override undoes a keyword",
			video:     Video{ID: "v1", ChannelID: chAllowed, Title: "A scary monster"},
			keywords:  []Keyword{scary},
			overrides: []string{"v1"},
			want:      VerdictServe,
		},
		{
			name:      "an override for a different video does not apply",
			video:     Video{ID: "v1", ChannelID: chAllowed, Title: "A scary monster"},
			keywords:  []Keyword{scary},
			overrides: []string{"v2"},
			want:      VerdictSuppressed,
			wantField: domain.FieldTitle,
		},
		{
			name:      "a disallowed channel outranks an override",
			video:     Video{ID: "v1", ChannelID: chOther, Title: "Fine"},
			overrides: []string{"v1"},
			want:      VerdictChannelNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEvaluator(allowedRules(), tt.keywords, tt.overrides)
			got := e.Video(tt.video)

			if got.Verdict != tt.want {
				t.Fatalf("Verdict = %q, want %q", got.Verdict, tt.want)
			}
			if tt.want == VerdictSuppressed {
				if got.Match == nil {
					t.Fatal("Match = nil, want a match on a suppressed verdict")
				}
				if got.Match.Field != tt.wantField {
					t.Errorf("Match.Field = %q, want %q", got.Match.Field, tt.wantField)
				}
			} else if got.Match != nil {
				t.Errorf("Match = %+v, want nil for verdict %q", got.Match, got.Verdict)
			}
		})
	}
}

func TestEvaluatorSearchVideo(t *testing.T) {
	keyword := Keyword{ID: "kw", Term: "scary", MatchTitle: true, WholeWord: true}
	tests := []struct {
		name  string
		rules ChannelRules
		video Video
		want  Verdict
	}{
		{
			name:  "allowed video is playable",
			rules: allowedRules(),
			video: Video{ID: "allowed", ChannelID: chAllowed},
			want:  VerdictServe,
		},
		{
			name:  "requestable video is locked",
			video: Video{ID: "requestable", ChannelID: chOther},
			want:  VerdictLocked,
		},
		{
			name: "blocked video is hidden",
			rules: ChannelRules{
				Blocked: NewChannelSet(chOther),
			},
			video: Video{ID: "blocked", ChannelID: chOther},
			want:  VerdictHidden,
		},
		{
			name:  "live video from allowed channel is hidden",
			rules: allowedRules(),
			video: Video{ID: "live", ChannelID: chAllowed, LiveState: domain.LiveLive},
			want:  VerdictLive,
		},
		{
			name:  "keyword applies to allowed video",
			rules: allowedRules(),
			video: Video{ID: "keyword", ChannelID: chAllowed, Title: "a scary story"},
			want:  VerdictSuppressed,
		},
		{
			name:  "keyword does not hide a locked tile",
			video: Video{ID: "locked-keyword", ChannelID: chOther, Title: "a scary story"},
			want:  VerdictLocked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evaluator := NewEvaluator(tt.rules, []Keyword{keyword}, nil)
			if got := evaluator.SearchVideo(tt.video).Verdict; got != tt.want {
				t.Errorf("Verdict = %q, want %q", got, tt.want)
			}
		})
	}
}

// A parent reviewing a suppression needs to know which rule fired, so the
// keyword identity has to survive evaluation intact.
func TestSuppressionCarriesKeywordIdentity(t *testing.T) {
	kw := Keyword{
		ID: "kw-1", Term: "Scary", Scope: ScopeChild,
		MatchTitle: true, WholeWord: true,
	}
	e := NewEvaluator(allowedRules(), []Keyword{kw}, nil)

	d := e.Video(Video{ID: "v1", ChannelID: chAllowed, Title: "a SCARY thing"})
	if d.Verdict != VerdictSuppressed {
		t.Fatalf("Verdict = %q, want suppressed", d.Verdict)
	}
	if d.Match.KeywordID != "kw-1" {
		t.Errorf("KeywordID = %q, want kw-1", d.Match.KeywordID)
	}
	// The original casing is reported, not the lowercased match form.
	if d.Match.Term != "Scary" {
		t.Errorf("Term = %q, want Scary", d.Match.Term)
	}
	if d.Match.Scope != ScopeChild {
		t.Errorf("Scope = %q, want child", d.Match.Scope)
	}
}

func TestMatcherFieldPriority(t *testing.T) {
	kw := Keyword{
		ID: "kw-1", Term: "slime",
		MatchTitle: true, MatchTags: true, MatchDescription: true, WholeWord: true,
	}
	m := NewMatcher([]Keyword{kw})

	got := m.Match(Video{
		Title:       "slime time",
		Tags:        []string{"slime"},
		Description: "all about slime",
	})
	if got == nil {
		t.Fatal("Match() = nil, want a hit")
	}
	if got.Field != domain.FieldTitle {
		t.Errorf("Field = %q, want title to win when several match", got.Field)
	}
}

func TestMatcherFirstKeywordWins(t *testing.T) {
	m := NewMatcher([]Keyword{
		{ID: "first", Term: "alpha", MatchTitle: true, WholeWord: true},
		{ID: "second", Term: "beta", MatchTitle: true, WholeWord: true},
	})

	got := m.Match(Video{Title: "alpha and beta"})
	if got == nil {
		t.Fatal("Match() = nil, want a hit")
	}
	if got.KeywordID != "first" {
		t.Errorf("KeywordID = %q, want the first listed keyword", got.KeywordID)
	}
}

// A blank term would match every video ever, so it must never compile.
func TestMatcherDropsBlankTerms(t *testing.T) {
	m := NewMatcher([]Keyword{
		{ID: "empty", Term: "", MatchTitle: true},
		{ID: "spaces", Term: "   ", MatchTitle: true},
	})
	if got := m.Match(Video{Title: "anything at all"}); got != nil {
		t.Errorf("Match() = %+v, want nil for blank terms", got)
	}
}

func TestEmptyMatcherServesEverything(t *testing.T) {
	m := NewMatcher(nil)
	if got := m.Match(Video{Title: "anything"}); got != nil {
		t.Errorf("Match() = %+v, want nil", got)
	}
}

func TestContainsWholeWord(t *testing.T) {
	tests := []struct {
		name     string
		haystack string
		needle   string
		want     bool
	}{
		{name: "exact match", haystack: "gun", needle: "gun", want: true},
		{name: "surrounded by spaces", haystack: "the gun is", needle: "gun", want: true},
		{name: "at the start", haystack: "gun safety", needle: "gun", want: true},
		{name: "at the end", haystack: "a toy gun", needle: "gun", want: true},
		{name: "trailing punctuation", haystack: "a gun.", needle: "gun", want: true},
		{name: "leading punctuation", haystack: "(gun)", needle: "gun", want: true},

		{name: "suffix of a longer word", haystack: "we had begun", needle: "gun", want: false},
		{name: "prefix of a longer word", haystack: "gunpowder", needle: "gun", want: false},
		{name: "inside a longer word", haystack: "shotgunner", needle: "gun", want: false},
		{name: "separated by underscore", haystack: "toy_gun", needle: "gun", want: false},
		{name: "adjacent to a digit", haystack: "gun2", needle: "gun", want: false},

		// Exercises the offset advance: the first occurrence fails the
		// boundary check and the scan must continue rather than give up.
		{name: "second occurrence is a real word", haystack: "begun gun", needle: "gun", want: true},
		{name: "many failures then a hit", haystack: "gunpowder begun a gun", needle: "gun", want: true},
		{name: "many near misses and no hit", haystack: "gunpowder begun shotgun", needle: "gun", want: false},

		{name: "multi word term", haystack: "a scary monster show", needle: "scary monster", want: true},
		{name: "multi word term split up", haystack: "scary big monster", needle: "scary monster", want: false},

		{name: "needle longer than haystack", haystack: "gun", needle: "gunpowder", want: false},
		{name: "absent entirely", haystack: "birdhouse", needle: "gun", want: false},

		{name: "unicode word", haystack: "le café ouvert", needle: "café", want: true},
		{name: "unicode inside a word", haystack: "cafétéria", needle: "café", want: false},

		// A term whose edge is not a word character cannot require a boundary
		// there, matching regexp \b. The "c" edge still does, so a letter in
		// front of it is a miss even though the "++" end is unconstrained.
		{name: "non word edges still match", haystack: "i know c++ well", needle: "c++", want: true},
		{name: "trailing text after non word edge", haystack: "c++here", needle: "c++", want: true},
		{name: "word char before the word edge", haystack: "usec++here", needle: "c++", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsWord(tt.haystack, tt.needle); got != tt.want {
				t.Errorf("containsWord(%q, %q) = %v, want %v",
					tt.haystack, tt.needle, got, tt.want)
			}
		})
	}
}

func TestSubstringModeMatchesInsideWords(t *testing.T) {
	m := NewMatcher([]Keyword{
		{ID: "kw", Term: "gun", MatchTitle: true, WholeWord: false},
	})
	if got := m.Match(Video{Title: "we had begun"}); got == nil {
		t.Error("Match() = nil, want a hit in substring mode")
	}
}

func TestMatchingIsCaseInsensitive(t *testing.T) {
	m := NewMatcher([]Keyword{
		{ID: "kw", Term: "ScArY", MatchTitle: true, MatchTags: true, WholeWord: true},
	})

	if got := m.Match(Video{Title: "A SCARY THING"}); got == nil {
		t.Error("uppercase title: Match() = nil, want a hit")
	}
	if got := m.Match(Video{Tags: []string{"SCARY"}}); got == nil {
		t.Error("uppercase tag: Match() = nil, want a hit")
	}
}

func TestFilter(t *testing.T) {
	kw := Keyword{
		ID: "kw-1", Term: "scary", Scope: ScopeFamily,
		MatchTitle: true, WholeWord: true,
	}
	e := NewEvaluator(allowedRules(), []Keyword{kw}, nil)

	served, suppressed := e.Filter([]Video{
		{ID: "keep1", ChannelID: chAllowed, Title: "birdhouse"},
		{ID: "drop-channel", ChannelID: chOther, Title: "birdhouse"},
		{ID: "drop-live", ChannelID: chAllowed, Title: "birdhouse", LiveState: domain.LiveLive},
		{ID: "drop-keyword", ChannelID: chAllowed, Title: "a scary story"},
		{ID: "keep2", ChannelID: chAllowed, Title: "another birdhouse"},
	})

	if len(served) != 2 || served[0].ID != "keep1" || served[1].ID != "keep2" {
		t.Errorf("served = %v, want keep1 and keep2 in order", ids(served))
	}

	// Only keyword hits are logged. A disallowed channel and a livestream are
	// not decisions a parent needs to review.
	if len(suppressed) != 1 {
		t.Fatalf("suppressed = %d entries, want exactly 1", len(suppressed))
	}
	s := suppressed[0]
	if s.VideoID != "drop-keyword" || s.KeywordID != "kw-1" || s.Field != domain.FieldTitle {
		t.Errorf("suppression = %+v, want the keyword hit on drop-keyword", s)
	}
}

func TestFilterEmptyInput(t *testing.T) {
	e := NewEvaluator(allowedRules(), nil, nil)
	served, suppressed := e.Filter(nil)
	if served != nil || suppressed != nil {
		t.Errorf("Filter(nil) = (%v, %v), want (nil, nil)", served, suppressed)
	}
}

func ids(videos []Video) []string {
	out := make([]string, len(videos))
	for i, v := range videos {
		out[i] = v.ID
	}
	return out
}
