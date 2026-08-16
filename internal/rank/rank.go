// Package rank orders policy-approved videos from local activity signals.
// It performs no I/O and never imports the persistence layer.
package rank

import (
	"cmp"
	"slices"
	"strconv"
	"time"

	"github.com/nerdswhofish/coop/internal/domain"
)

const (
	defaultPageSize       = 30
	defaultMaxConsecutive = 2
	defaultForgottenAfter = 14 * 24 * time.Hour
)

// ReasonKind lets clients pair a stable visual treatment with an explanation.
type ReasonKind string

const (
	ReasonLiked               ReasonKind = "liked"
	ReasonDisliked            ReasonKind = "disliked"
	ReasonParentMore          ReasonKind = "parent_more"
	ReasonParentLess          ReasonKind = "parent_less"
	ReasonRewatched           ReasonKind = "rewatched"
	ReasonCompleted           ReasonKind = "completed"
	ReasonChannelSatisfaction ReasonKind = "channel_satisfaction"
	ReasonNewSubscription     ReasonKind = "new_subscription"
	ReasonUnwatched           ReasonKind = "unwatched"
	ReasonRecent              ReasonKind = "recent"
)

// Candidate is the metadata the scorer needs from one approved video.
type Candidate struct {
	ID          string
	ChannelID   string
	PublishedAt time.Time
}

// Watch is one locally recorded viewing event.
type Watch struct {
	VideoID            string
	ChannelID          string
	StartedAt          time.Time
	CompletionFraction float64
}

// Reaction is a local explicit preference.
type Reaction struct {
	VideoID string
	Kind    domain.ReactionKind
}

// Input is a complete, already policy-filtered ranking fixture.
type Input struct {
	Candidates         []Candidate
	Watches            []Watch
	Reactions          []Reaction
	SubscribedChannels map[string]bool
	ChannelWeights     map[string]int
	Now                time.Time
	PageSize           int
	MaxConsecutive     int
	ForgottenAfter     time.Duration
}

// Recommendation carries the order, score, and parent-facing explanation.
type Recommendation struct {
	Candidate  Candidate
	Score      float64
	Reason     string
	ReasonKind ReasonKind

	watched            bool
	channelLastWatched time.Time
}

type videoHistory struct {
	count      int
	completion float64
}

type channelHistory struct {
	completedThisWeek int
	lastWatched       time.Time
}

// Videos returns recommendations arranged into diverse pages.
func Videos(input Input) []Recommendation {
	if input.Now.IsZero() {
		input.Now = time.Now()
	}
	if input.PageSize <= 0 {
		input.PageSize = defaultPageSize
	}
	if input.MaxConsecutive <= 0 {
		input.MaxConsecutive = defaultMaxConsecutive
	}
	if input.ForgottenAfter <= 0 {
		input.ForgottenAfter = defaultForgottenAfter
	}

	videos, channels := histories(input.Watches, input.Now)
	reactions := make(map[string]domain.ReactionKind, len(input.Reactions))
	for _, reaction := range input.Reactions {
		reactions[reaction.VideoID] = reaction.Kind
	}

	out := make([]Recommendation, 0, len(input.Candidates))
	for _, candidate := range input.Candidates {
		out = append(out, score(candidate, videos[candidate.ID], channels[candidate.ChannelID],
			reactions[candidate.ID], input))
	}
	slices.SortFunc(out, compareRecommendations)
	return arrange(out, input)
}

func histories(watches []Watch, now time.Time) (map[string]videoHistory, map[string]channelHistory) {
	videos := make(map[string]videoHistory)
	channels := make(map[string]channelHistory)
	weekAgo := now.Add(-7 * 24 * time.Hour)
	for _, watch := range watches {
		completion := min(max(watch.CompletionFraction, 0), 1)
		video := videos[watch.VideoID]
		video.count++
		video.completion += completion
		videos[watch.VideoID] = video

		channel := channels[watch.ChannelID]
		if watch.StartedAt.After(channel.lastWatched) {
			channel.lastWatched = watch.StartedAt
		}
		if !watch.StartedAt.Before(weekAgo) && completion >= 0.8 {
			channel.completedThisWeek++
		}
		channels[watch.ChannelID] = channel
	}
	return videos, channels
}

func score(candidate Candidate, video videoHistory, channel channelHistory,
	reaction domain.ReactionKind, input Input) Recommendation {

	score := 0.0
	weight := max(-2, min(input.ChannelWeights[candidate.ChannelID], 2))
	score += float64(weight) * 7

	switch reaction {
	case domain.ReactionLike:
		score += 20
	case domain.ReactionDislike:
		score -= 20
	}

	averageCompletion := 0.0
	if video.count > 0 {
		averageCompletion = video.completion / float64(video.count)
		score += averageCompletion * 4
		if averageCompletion < 0.2 {
			score -= 4
		}
		score += float64(min(video.count-1, 3)) * 1.5
	} else {
		score++
	}
	score += float64(min(channel.completedThisWeek, 5)) * 1.2

	subscribed := input.SubscribedChannels[candidate.ChannelID]
	if subscribed {
		score += 2
		age := input.Now.Sub(candidate.PublishedAt)
		if age < 0 {
			age = 0
		}
		if age < 30*24*time.Hour {
			score += 2 * (1 - age.Hours()/(30*24))
		}
	}

	kind, reason := explanation(reaction, weight, video, averageCompletion, channel,
		subscribed, candidate, input.Now)
	return Recommendation{
		Candidate:          candidate,
		Score:              score,
		Reason:             reason,
		ReasonKind:         kind,
		watched:            video.count > 0,
		channelLastWatched: channel.lastWatched,
	}
}

func explanation(reaction domain.ReactionKind, weight int, video videoHistory,
	averageCompletion float64, channel channelHistory, subscribed bool,
	candidate Candidate, now time.Time) (ReasonKind, string) {

	switch {
	case reaction == domain.ReactionDislike:
		return ReasonDisliked, "They marked this video Not for me, so it ranks lower."
	case weight < 0:
		return ReasonParentLess, "You asked Coop to show this channel less often."
	case reaction == domain.ReactionLike:
		return ReasonLiked, "They liked this video."
	case weight > 0:
		return ReasonParentMore, "You asked Coop to show more from this channel."
	case video.count > 1:
		return ReasonRewatched, "They chose to watch this video more than once."
	case channel.completedThisWeek > 0:
		return ReasonChannelSatisfaction, completedReason(channel.completedThisWeek)
	case averageCompletion >= 0.8:
		return ReasonCompleted, "They finished this video."
	case subscribed && now.Sub(candidate.PublishedAt) <= 7*24*time.Hour:
		return ReasonNewSubscription, "A new upload from a channel they follow."
	case video.count == 0:
		return ReasonUnwatched, "Something new from an approved channel."
	default:
		return ReasonRecent, "A recent upload from an approved channel."
	}
}

func completedReason(count int) string {
	if count == 1 {
		return "They finished a video from this channel this week."
	}
	return "They finished " + strconv.Itoa(count) + " videos from this channel this week."
}

func compareRecommendations(a, b Recommendation) int {
	if scoreOrder := cmp.Compare(b.Score, a.Score); scoreOrder != 0 {
		return scoreOrder
	}
	if publishedOrder := b.Candidate.PublishedAt.Compare(a.Candidate.PublishedAt); publishedOrder != 0 {
		return publishedOrder
	}
	return cmp.Compare(a.Candidate.ID, b.Candidate.ID)
}

func arrange(ranked []Recommendation, input Input) []Recommendation {
	remaining := slices.Clone(ranked)
	result := make([]Recommendation, 0, len(ranked))
	lastChannel := ""
	consecutive := 0

	for len(remaining) > 0 {
		pageSize := min(input.PageSize, len(remaining))
		page, rest := reservePage(remaining, pageSize, input.Now.Add(-input.ForgottenAfter))
		remaining = rest
		page = avoidChannelTunnels(page, input.MaxConsecutive, lastChannel, consecutive)
		for _, recommendation := range page {
			if recommendation.Candidate.ChannelID == lastChannel {
				consecutive++
			} else {
				lastChannel = recommendation.Candidate.ChannelID
				consecutive = 1
			}
			result = append(result, recommendation)
		}
	}
	return result
}

func reservePage(ranked []Recommendation, pageSize int, forgottenBefore time.Time) (
	[]Recommendation, []Recommendation) {

	selected := make([]Recommendation, 0, pageSize)
	reservedIDs := map[string]bool{}

	if recommendation, ok := firstMatching(ranked, func(r Recommendation) bool {
		return !r.watched
	}); ok {
		selected = append(selected, recommendation)
		reservedIDs[recommendation.Candidate.ID] = true
	}
	if recommendation, ok := firstMatching(ranked, func(r Recommendation) bool {
		return !reservedIDs[r.Candidate.ID] &&
			(r.channelLastWatched.IsZero() || r.channelLastWatched.Before(forgottenBefore))
	}); ok {
		selected = append(selected, recommendation)
		reservedIDs[recommendation.Candidate.ID] = true
	}

	for _, recommendation := range ranked {
		if len(selected) == pageSize {
			break
		}
		if !reservedIDs[recommendation.Candidate.ID] {
			selected = append(selected, recommendation)
			reservedIDs[recommendation.Candidate.ID] = true
		}
	}
	slices.SortFunc(selected, compareRecommendations)

	rest := make([]Recommendation, 0, len(ranked)-len(selected))
	for _, recommendation := range ranked {
		if !reservedIDs[recommendation.Candidate.ID] {
			rest = append(rest, recommendation)
		}
	}
	return selected, rest
}

func firstMatching(items []Recommendation, match func(Recommendation) bool) (Recommendation, bool) {
	for _, item := range items {
		if match(item) {
			return item, true
		}
	}
	return Recommendation{}, false
}

func avoidChannelTunnels(page []Recommendation, maxConsecutive int,
	lastChannel string, consecutive int) []Recommendation {

	remaining := slices.Clone(page)
	out := make([]Recommendation, 0, len(page))
	for len(remaining) > 0 {
		index := 0
		if consecutive >= maxConsecutive {
			if alternative := slices.IndexFunc(remaining, func(r Recommendation) bool {
				return r.Candidate.ChannelID != lastChannel
			}); alternative >= 0 {
				index = alternative
			}
		}

		chosen := remaining[index]
		remaining = slices.Delete(remaining, index, index+1)
		out = append(out, chosen)
		if chosen.Candidate.ChannelID == lastChannel {
			consecutive++
		} else {
			lastChannel = chosen.Candidate.ChannelID
			consecutive = 1
		}
	}
	return out
}
