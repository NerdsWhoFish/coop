// Package policy decides what a child may see.
//
// Everything here is a pure function over plain structs: no database, no
// network, no clock. That is what makes the rules exhaustively table-testable,
// and why this package must never import internal/store.
package policy

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nerdswhofish/coop/internal/domain"
)

// ChannelSet is a membership set of YouTube channel IDs.
type ChannelSet map[string]struct{}

// NewChannelSet builds a set from ids.
func NewChannelSet(ids ...string) ChannelSet {
	s := make(ChannelSet, len(ids))
	for _, id := range ids {
		s[id] = struct{}{}
	}
	return s
}

// Has reports membership. The zero ChannelSet is usable and reports false.
func (s ChannelSet) Has(id string) bool {
	_, ok := s[id]
	return ok
}

// ChannelRules is everything needed to resolve channel access for one child.
type ChannelRules struct {
	// Blocked hides channels family-wide. It outranks every allow.
	Blocked ChannelSet
	// AllowGlobal is the family default.
	AllowGlobal ChannelSet
	// AllowChild adds channels for this child alone.
	AllowChild ChannelSet
	// DenyChild subtracts a globally approved channel from this child.
	DenyChild ChannelSet
}

// State resolves effective visibility: Blocked wins outright, then DenyChild
// subtracts, then either allowlist grants, and anything left is requestable.
// The full set algebra and its rationale are in adr/0003.
func (r ChannelRules) State(channelID string) domain.ChannelState {
	if r.Blocked.Has(channelID) {
		return domain.StateBlocked
	}
	if r.DenyChild.Has(channelID) {
		return domain.StateRequestable
	}
	if r.AllowGlobal.Has(channelID) || r.AllowChild.Has(channelID) {
		return domain.StateAllowed
	}
	return domain.StateRequestable
}

// Video is the subset of a video's metadata that policy evaluates.
type Video struct {
	ID          string
	ChannelID   string
	Title       string
	Description string
	Tags        []string
	LiveState   domain.LiveState
}

// Keyword suppresses individual videos inside an otherwise-allowed channel.
type Keyword struct {
	ID    string
	Term  string
	Scope KeywordScope

	MatchTitle       bool
	MatchTags        bool
	MatchDescription bool

	// WholeWord stops "gun" from also matching "begun".
	WholeWord bool
}

// KeywordScope records whether a keyword came from the family or one child,
// so the parent app can explain which rule fired.
type KeywordScope string

const (
	ScopeFamily KeywordScope = "family"
	ScopeChild  KeywordScope = "child"
)

// Match describes which keyword suppressed a video, and where it hit.
type Match struct {
	KeywordID string
	Term      string
	Scope     KeywordScope
	Field     domain.MatchField
}

// Verdict is the outcome of evaluating one video.
type Verdict string

const (
	VerdictServe             Verdict = "serve"
	VerdictChannelNotAllowed Verdict = "channel_not_allowed"
	VerdictHidden            Verdict = "hidden"
	VerdictLocked            Verdict = "locked"
	VerdictLive              Verdict = "live"
	VerdictSuppressed        Verdict = "suppressed"
	VerdictVideoBlocked      Verdict = "video_blocked"
)

// Decision is a verdict plus, when suppressed, the keyword responsible.
type Decision struct {
	Verdict Verdict
	Match   *Match
}

// Served reports whether the video reaches the child.
func (d Decision) Served() bool { return d.Verdict == VerdictServe }

// Matcher evaluates a fixed keyword list against many videos. Terms are
// lowercased once here rather than per video, since a feed build runs every
// keyword against every candidate.
type Matcher struct {
	keywords []compiledKeyword
}

type compiledKeyword struct {
	keyword Keyword
	term    string
}

// NewMatcher compiles keywords, dropping blank terms that match everything.
func NewMatcher(keywords []Keyword) *Matcher {
	compiled := make([]compiledKeyword, 0, len(keywords))
	for _, k := range keywords {
		term := strings.ToLower(strings.TrimSpace(k.Term))
		if term == "" {
			continue
		}
		compiled = append(compiled, compiledKeyword{keyword: k, term: term})
	}
	return &Matcher{keywords: compiled}
}

// Match returns the first keyword that hits, or nil. Fields are checked title,
// then tags, then description, so the reported field is the most specific
// place a parent would look first.
func (m *Matcher) Match(v Video) *Match {
	if len(m.keywords) == 0 {
		return nil
	}

	title := strings.ToLower(v.Title)
	description := strings.ToLower(v.Description)
	tags := make([]string, len(v.Tags))
	for i, t := range v.Tags {
		tags[i] = strings.ToLower(t)
	}

	for _, c := range m.keywords {
		if c.keyword.MatchTitle && contains(title, c.term, c.keyword.WholeWord) {
			return c.match(domain.FieldTitle)
		}
		if c.keyword.MatchTags {
			for _, tag := range tags {
				if contains(tag, c.term, c.keyword.WholeWord) {
					return c.match(domain.FieldTags)
				}
			}
		}
		if c.keyword.MatchDescription && contains(description, c.term, c.keyword.WholeWord) {
			return c.match(domain.FieldDescription)
		}
	}
	return nil
}

func (c compiledKeyword) match(field domain.MatchField) *Match {
	return &Match{
		KeywordID: c.keyword.ID,
		Term:      c.keyword.Term,
		Scope:     c.keyword.Scope,
		Field:     field,
	}
}

// Evaluator applies one child's complete rule set to videos.
type Evaluator struct {
	rules     ChannelRules
	matcher   *Matcher
	overrides ChannelSet
	blocks    ChannelSet
}

// NewEvaluator builds an evaluator. overrides are video IDs a parent has
// re-allowed after a keyword caught them.
func NewEvaluator(rules ChannelRules, keywords []Keyword, overrides, blocks []string) *Evaluator {
	return &Evaluator{
		rules:     rules,
		matcher:   NewMatcher(keywords),
		overrides: NewChannelSet(overrides...),
		blocks:    NewChannelSet(blocks...),
	}
}

// Channel resolves a channel's effective state.
func (e *Evaluator) Channel(channelID string) domain.ChannelState {
	return e.rules.State(channelID)
}

// Video decides whether one video reaches the child. Order matters: live is
// checked before overrides so a re-allow cannot resurrect a livestream, and
// overrides precede keywords so they can undo a false positive.
func (e *Evaluator) Video(v Video) Decision {
	if e.blocks.Has(v.ID) {
		return Decision{Verdict: VerdictVideoBlocked}
	}
	if e.rules.State(v.ChannelID) != domain.StateAllowed {
		return Decision{Verdict: VerdictChannelNotAllowed}
	}
	if v.LiveState.IsLive() {
		return Decision{Verdict: VerdictLive}
	}
	if e.overrides.Has(v.ID) {
		return Decision{Verdict: VerdictServe}
	}
	if m := e.matcher.Match(v); m != nil {
		return Decision{Verdict: VerdictSuppressed, Match: m}
	}
	return Decision{Verdict: VerdictServe}
}

// SearchVideo preserves the distinction browsing needs between a blocked
// channel, which is invisible, and a requestable channel, whose videos appear
// locked with an ask affordance. Allowed videos still pass through the normal
// live and keyword rules.
func (e *Evaluator) SearchVideo(v Video) Decision {
	if e.blocks.Has(v.ID) {
		return Decision{Verdict: VerdictHidden}
	}
	switch e.rules.State(v.ChannelID) {
	case domain.StateBlocked:
		return Decision{Verdict: VerdictHidden}
	case domain.StateRequestable:
		return Decision{Verdict: VerdictLocked}
	default:
		return e.Video(v)
	}
}

// Suppression is a keyword hit worth recording for the parent's review.
type Suppression struct {
	VideoID   string
	KeywordID string
	Term      string
	Scope     KeywordScope
	Field     domain.MatchField
}

// Filter splits videos into what the child sees and what to log. Only keyword
// hits are logged: videos dropped for a disallowed channel or for being live
// are not something a parent needs to review.
func (e *Evaluator) Filter(videos []Video) (served []Video, suppressed []Suppression) {
	for _, v := range videos {
		d := e.Video(v)
		switch d.Verdict {
		case VerdictServe:
			served = append(served, v)
		case VerdictSuppressed:
			suppressed = append(suppressed, Suppression{
				VideoID:   v.ID,
				KeywordID: d.Match.KeywordID,
				Term:      d.Match.Term,
				Scope:     d.Match.Scope,
				Field:     d.Match.Field,
			})
		}
	}
	return served, suppressed
}

func contains(haystack, needle string, wholeWord bool) bool {
	if needle == "" {
		return false
	}
	if !wholeWord {
		return strings.Contains(haystack, needle)
	}
	return containsWord(haystack, needle)
}

// containsWord reports whether needle appears in haystack delimited by word
// boundaries; both must already be lowercased. A boundary is required only on
// an edge whose needle character is itself a word char, matching regexp \b.
func containsWord(haystack, needle string) bool {
	firstRune, _ := utf8.DecodeRuneInString(needle)
	lastRune, _ := utf8.DecodeLastRuneInString(needle)
	checkStart := isWordRune(firstRune)
	checkEnd := isWordRune(lastRune)

	for offset := 0; offset <= len(haystack)-len(needle); {
		i := strings.Index(haystack[offset:], needle)
		if i < 0 {
			return false
		}
		start := offset + i
		end := start + len(needle)

		startOK := !checkStart || boundaryBefore(haystack, start)
		endOK := !checkEnd || boundaryAfter(haystack, end)
		if startOK && endOK {
			return true
		}
		offset = start + 1
	}
	return false
}

func boundaryBefore(s string, i int) bool {
	if i == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return !isWordRune(r)
}

func boundaryAfter(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return !isWordRune(r)
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}
