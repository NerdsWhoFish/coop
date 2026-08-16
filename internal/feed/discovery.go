package feed

import (
	"cmp"
	"context"
	"hash/fnv"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/domain"
	"github.com/nerdswhofish/coop/internal/store"
)

// DiscoverySeed is one child preference translated into a YouTube search.
// The API caches that search and exposes only requestable, policy-safe results.
type DiscoverySeed struct {
	Query  string
	Reason string
}

// DiscoverySeed rotates daily through explicit and high-confidence preference
// signals. It returns no seed until the child has expressed a preference.
func (s *Service) DiscoverySeed(ctx context.Context, childID uuid.UUID,
	now time.Time) (DiscoverySeed, error) {

	watches, err := s.activity.RankingWatches(ctx, childID, now.Add(-rankingHistoryWindow))
	if err != nil {
		return DiscoverySeed{}, err
	}
	reactions, err := s.activity.Reactions(ctx, childID)
	if err != nil {
		return DiscoverySeed{}, err
	}
	subscriptions, err := s.activity.SubscribedChannelIDs(ctx, childID)
	if err != nil {
		return DiscoverySeed{}, err
	}
	weights, err := s.activity.ChannelWeights(ctx, childID)
	if err != nil {
		return DiscoverySeed{}, err
	}

	videoIDs := make([]string, 0, len(reactions)+len(watches))
	for _, reaction := range reactions {
		if reaction.Kind == domain.ReactionLike {
			videoIDs = append(videoIDs, reaction.VideoID)
		}
	}
	for _, watch := range watches {
		if watch.CompletionFraction >= 0.8 {
			videoIDs = append(videoIDs, watch.VideoID)
		}
	}
	videos, err := s.catalog.VideosByID(ctx, uniqueStrings(videoIDs))
	if err != nil {
		return DiscoverySeed{}, err
	}
	videoByID := make(map[string]store.Video, len(videos))
	for _, video := range videos {
		videoByID[video.ID] = video
	}

	signals := make([]DiscoverySeed, 0, len(videos)+len(subscriptions)+len(weights))
	for _, reaction := range reactions {
		if reaction.Kind != domain.ReactionLike {
			continue
		}
		if video, ok := videoByID[reaction.VideoID]; ok {
			signals = append(signals, videoDiscoverySeed(video, "Because you liked "))
		}
	}
	completed := make(map[string]struct{}, len(watches))
	for _, watch := range watches {
		if watch.CompletionFraction < 0.8 {
			continue
		}
		if _, seen := completed[watch.VideoID]; seen {
			continue
		}
		completed[watch.VideoID] = struct{}{}
		if video, ok := videoByID[watch.VideoID]; ok {
			signals = append(signals, videoDiscoverySeed(video, "Because you finished "))
		}
	}

	channelIDs := append([]string{}, subscriptions...)
	for channelID, weight := range weights {
		if weight > 0 {
			channelIDs = append(channelIDs, channelID)
		}
	}
	channels, err := s.catalog.ChannelsByID(ctx, uniqueStrings(channelIDs))
	if err != nil {
		return DiscoverySeed{}, err
	}
	for channelID, weight := range weights {
		if weight <= 0 {
			continue
		}
		if channel, ok := channels[channelID]; ok && strings.TrimSpace(channel.Title) != "" {
			signals = append(signals, DiscoverySeed{
				Query: channel.Title, Reason: "Because your grown-up picked more " + channel.Title,
			})
		}
	}
	for _, channelID := range subscriptions {
		if channel, ok := channels[channelID]; ok && strings.TrimSpace(channel.Title) != "" {
			signals = append(signals, DiscoverySeed{
				Query: channel.Title, Reason: "Because you follow " + channel.Title,
			})
		}
	}

	signals = uniqueDiscoverySeeds(signals)
	if len(signals) == 0 {
		return DiscoverySeed{}, nil
	}
	slices.SortFunc(signals, func(a, b DiscoverySeed) int { return cmp.Compare(a.Query, b.Query) })
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(childID.String() + now.Format("2006-01-02")))
	return signals[int(hash.Sum32())%len(signals)], nil
}

func videoDiscoverySeed(video store.Video, reasonPrefix string) DiscoverySeed {
	queryParts := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, tag := range video.Tags {
		tag = strings.TrimSpace(tag)
		normalized := strings.ToLower(tag)
		if len(tag) < 3 || len(tag) > 48 || len(strings.Fields(tag)) > 5 {
			continue
		}
		if _, duplicate := seen[normalized]; duplicate {
			continue
		}
		seen[normalized] = struct{}{}
		queryParts = append(queryParts, tag)
		if len(queryParts) == 2 {
			break
		}
	}
	query := strings.Join(queryParts, " ")
	if query == "" {
		query = video.Title
	}
	return DiscoverySeed{Query: query, Reason: reasonPrefix + video.Title}
}

func uniqueDiscoverySeeds(seeds []DiscoverySeed) []DiscoverySeed {
	seen := make(map[string]struct{}, len(seeds))
	out := make([]DiscoverySeed, 0, len(seeds))
	for _, seed := range seeds {
		seed.Query = strings.Join(strings.Fields(seed.Query), " ")
		if seed.Query == "" {
			continue
		}
		key := strings.ToLower(seed.Query)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, seed)
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
