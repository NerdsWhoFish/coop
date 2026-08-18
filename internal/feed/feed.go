// Package feed assembles what a child sees, composing the cached catalog with
// the policy engine and recording what was filtered.
package feed

import (
	"context"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/domain"
	"github.com/nerdswhofish/coop/internal/policy"
	"github.com/nerdswhofish/coop/internal/rank"
	"github.com/nerdswhofish/coop/internal/store"
)

// maxRefillRounds bounds how hard a page tries to fill itself after keyword
// suppression removes rows. Without a bound, a child whose filters exclude
// almost everything would walk their entire history on one request.
const maxRefillRounds = 4

// shortsPoolSize caps how many Shorts take part in the shuffle. Large enough
// that the feed does not feel repetitive, small enough to shuffle per request.
const shortsPoolSize = 2000

const (
	recommendationPoolSize = 2000
	rankingHistoryWindow   = 90 * 24 * time.Hour
)

// Service builds feeds for one family's children.
type Service struct {
	catalog  *store.Catalog
	rules    *store.Rules
	activity *store.Activity
}

// New builds the service.
func New(catalog *store.Catalog, rules *store.Rules, activity *store.Activity) *Service {
	return &Service{catalog: catalog, rules: rules, activity: activity}
}

// Page is one page of videos.
type Page struct {
	Videos     []store.Video
	NextCursor string
}

// Recommendation is one ranked video with the explanation shown to parents.
type Recommendation struct {
	Video      store.Video
	Score      float64
	Reason     string
	ReasonKind rank.ReasonKind
}

// RecommendationPage is the explainable form of a Home page.
type RecommendationPage struct {
	Recommendations []Recommendation
	NextCursor      string
}

// SearchVideo is a search tile plus whether playback must wait for approval.
type SearchVideo struct {
	Video  store.Video
	Locked bool
}

// Search filters freshly fetched video matches for one child. Blocked
// channels and live content disappear, requestable channels remain as locked
// tiles, and keyword hits on allowed channels are logged for parent review.
func (s *Service) Search(ctx context.Context, familyID, childID uuid.UUID,
	videos []store.Video) ([]SearchVideo, error) {

	evaluator, err := s.rules.Evaluator(ctx, familyID, childID)
	if err != nil {
		return nil, err
	}

	out := make([]SearchVideo, 0, len(videos))
	suppressed := make([]policy.Suppression, 0)
	for _, video := range videos {
		decision := evaluator.SearchVideo(toPolicyVideo(video))
		switch decision.Verdict {
		case policy.VerdictServe:
			out = append(out, SearchVideo{Video: video})
		case policy.VerdictLocked:
			out = append(out, SearchVideo{Video: video, Locked: true})
		case policy.VerdictSuppressed:
			suppressed = append(suppressed, policy.Suppression{
				VideoID:   video.ID,
				KeywordID: decision.Match.KeywordID,
				Term:      decision.Match.Term,
				Scope:     decision.Match.Scope,
				Field:     decision.Match.Field,
			})
		}
	}

	if err := s.logSuppressions(ctx, childID, suppressed); err != nil {
		return nil, err
	}
	return out, nil
}

// Home returns a locally ranked, diverse page from approved channels. Each
// fresh visit draws a new exploration seed, so the order varies between
// visits while one scroll stays coherent through the cursor.
func (s *Service) Home(ctx context.Context, familyID, childID uuid.UUID,
	limit int, cursor string) (Page, error) {

	page, err := s.recommendations(ctx, familyID, childID, limit, cursor, uuid.NewString())
	if err != nil {
		return Page{}, err
	}

	videos := make([]store.Video, 0, len(page.Recommendations))
	for _, recommendation := range page.Recommendations {
		videos = append(videos, recommendation.Video)
	}
	return Page{Videos: videos, NextCursor: page.NextCursor}, nil
}

// Recommendations ranks a bounded local catalog pool and exposes why each
// video landed where it did. It never calls YouTube, and it applies no
// exploration jitter, so parents tune against reproducible scores.
func (s *Service) Recommendations(ctx context.Context, familyID, childID uuid.UUID,
	limit int, cursor string) (RecommendationPage, error) {

	return s.recommendations(ctx, familyID, childID, limit, cursor, "")
}

func (s *Service) recommendations(ctx context.Context, familyID, childID uuid.UUID,
	limit int, cursor, seed string) (RecommendationPage, error) {

	if limit <= 0 || limit > 100 {
		limit = 30
	}
	cursorVideoID := ""
	if cursor != "" {
		// The cursor's seed wins so every page of one scroll shares one order.
		cursorSeed, videoID, err := decodeRecommendationCursor(cursor)
		if err != nil {
			return RecommendationPage{}, err
		}
		seed, cursorVideoID = cursorSeed, videoID
	}
	channelIDs, evaluator, err := s.watchableChannels(ctx, familyID, childID, false)
	if err != nil {
		return RecommendationPage{}, err
	}
	if len(channelIDs) == 0 {
		return RecommendationPage{}, nil
	}

	pool, err := s.catalog.Videos(ctx, store.FeedQuery{
		ChannelIDs:    channelIDs,
		ExcludeShorts: true,
		Limit:         recommendationPoolSize,
	})
	if err != nil {
		return RecommendationPage{}, err
	}
	served, suppressed := evaluator.Filter(toPolicyVideos(pool.Videos))
	if err := s.logSuppressions(ctx, childID, suppressed); err != nil {
		return RecommendationPage{}, err
	}
	videos := selectByID(pool.Videos, served)
	if len(videos) == 0 {
		return RecommendationPage{}, nil
	}

	now := time.Now()
	watches, err := s.activity.RankingWatches(ctx, childID, now.Add(-rankingHistoryWindow))
	if err != nil {
		return RecommendationPage{}, err
	}
	reactions, err := s.activity.Reactions(ctx, childID)
	if err != nil {
		return RecommendationPage{}, err
	}
	subscriptions, err := s.activity.SubscribedChannelIDs(ctx, childID)
	if err != nil {
		return RecommendationPage{}, err
	}
	weights, err := s.activity.ChannelWeights(ctx, childID)
	if err != nil {
		return RecommendationPage{}, err
	}

	ranked := rank.Videos(rank.Input{
		Candidates:         rankCandidates(videos),
		Watches:            rankWatches(watches),
		Reactions:          rankReactions(reactions),
		SubscribedChannels: stringSet(subscriptions),
		ChannelWeights:     weights,
		Now:                now,
		PageSize:           limit,
		Seed:               seed,
	})
	start, err := recommendationStart(ranked, cursorVideoID)
	if err != nil {
		return RecommendationPage{}, err
	}
	end := min(start+limit, len(ranked))

	byID := make(map[string]store.Video, len(videos))
	for _, video := range videos {
		byID[video.ID] = video
	}
	page := RecommendationPage{Recommendations: make([]Recommendation, 0, end-start)}
	for _, item := range ranked[start:end] {
		page.Recommendations = append(page.Recommendations, Recommendation{
			Video:      byID[item.Candidate.ID],
			Score:      item.Score,
			Reason:     item.Reason,
			ReasonKind: item.ReasonKind,
		})
	}
	if end < len(ranked) {
		page.NextCursor = encodeRecommendationCursor(seed, ranked[end-1].Candidate.ID)
	}
	return page, nil
}

// ChannelWeights returns the parent's non-neutral feed preferences.
func (s *Service) ChannelWeights(ctx context.Context, childID uuid.UUID) (map[string]int, error) {
	return s.activity.ChannelWeights(ctx, childID)
}

// SetChannelWeight changes a soft preference only for an approved channel.
func (s *Service) SetChannelWeight(ctx context.Context, familyID, childID uuid.UUID,
	channelID string, weight int, actorID uuid.UUID) error {

	evaluator, err := s.rules.Evaluator(ctx, familyID, childID)
	if err != nil {
		return err
	}
	if evaluator.Channel(channelID) != domain.StateAllowed {
		return store.ErrNotFound
	}
	return s.activity.SetChannelWeight(ctx, childID, channelID, weight, actorID)
}

func rankCandidates(videos []store.Video) []rank.Candidate {
	out := make([]rank.Candidate, 0, len(videos))
	for _, video := range videos {
		out = append(out, rank.Candidate{
			ID: video.ID, ChannelID: video.ChannelID, PublishedAt: video.PublishedAt,
		})
	}
	return out
}

func rankWatches(watches []store.RankingWatch) []rank.Watch {
	out := make([]rank.Watch, 0, len(watches))
	for _, watch := range watches {
		out = append(out, rank.Watch{
			VideoID: watch.VideoID, ChannelID: watch.ChannelID,
			StartedAt: watch.StartedAt, CompletionFraction: watch.CompletionFraction,
		})
	}
	return out
}

func rankReactions(reactions []store.Reaction) []rank.Reaction {
	out := make([]rank.Reaction, 0, len(reactions))
	for _, reaction := range reactions {
		out = append(out, rank.Reaction{VideoID: reaction.VideoID, Kind: reaction.Kind})
	}
	return out
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func recommendationStart(ranked []rank.Recommendation, videoID string) (int, error) {
	if videoID == "" {
		return 0, nil
	}
	index := slices.IndexFunc(ranked, func(item rank.Recommendation) bool {
		return item.Candidate.ID == videoID
	})
	if index < 0 {
		return 0, fmt.Errorf("recommendation cursor no longer exists")
	}
	return index + 1, nil
}

func encodeRecommendationCursor(seed, videoID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte("recommendation:" + seed + ":" + videoID))
}

// decodeRecommendationCursor also accepts the seedless form written before
// exploration existed, so a scroll in flight across a deploy keeps working.
func decodeRecommendationCursor(cursor string) (seed, videoID string, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", fmt.Errorf("malformed recommendation cursor: %w", err)
	}
	rest, found := strings.CutPrefix(string(raw), "recommendation:")
	if !found || rest == "" {
		return "", "", fmt.Errorf("malformed recommendation cursor")
	}
	seed, videoID, seeded := strings.Cut(rest, ":")
	if !seeded {
		return "", rest, nil
	}
	if videoID == "" {
		return "", "", fmt.Errorf("malformed recommendation cursor")
	}
	return seed, videoID, nil
}

// Shorts returns a shuffled, looping feed of short videos. It pages by offset
// rather than cursor because it wraps: a cursor has no meaning once the pool
// starts again from the top.
func (s *Service) Shorts(ctx context.Context, familyID, childID uuid.UUID,
	seed string, limit, offset int) (Page, error) {

	if limit <= 0 || limit > 50 {
		limit = 10
	}

	channelIDs, evaluator, err := s.watchableChannels(ctx, familyID, childID, false)
	if err != nil {
		return Page{}, err
	}
	if len(channelIDs) == 0 {
		return Page{}, nil
	}

	pool, err := s.catalog.Videos(ctx, store.FeedQuery{
		ChannelIDs: channelIDs,
		ShortsOnly: true,
		Limit:      shortsPoolSize,
	})
	if err != nil {
		return Page{}, err
	}

	served, suppressed := evaluator.Filter(toPolicyVideos(pool.Videos))
	if err := s.logSuppressions(ctx, childID, suppressed); err != nil {
		return Page{}, err
	}

	videos := selectByID(pool.Videos, served)
	if len(videos) == 0 {
		return Page{}, nil
	}

	shuffleDeterministically(videos, seed)

	// Wrapping rather than ending: a Shorts feed that stops is broken, and the
	// pool is finite by design.
	page := make([]store.Video, 0, limit)
	for i := range limit {
		page = append(page, videos[(offset+i)%len(videos)])
	}

	return Page{Videos: page}, nil
}

// ChannelView is a channel page as one child sees it.
type ChannelView struct {
	Channel        store.Channel
	State          domain.ChannelState
	Subscribed     bool
	PendingRequest bool
	Videos         []store.Video
	NextCursor     string
}

// Channel returns a channel page. Blocked reports ErrNotFound, the same as a
// channel that does not exist. Requestable returns branding but no videos,
// which is what makes the ask meaningful.
func (s *Service) Channel(ctx context.Context, familyID, childID uuid.UUID,
	channelID string, limit int, cursor string) (ChannelView, error) {

	evaluator, err := s.rules.Evaluator(ctx, familyID, childID)
	if err != nil {
		return ChannelView{}, err
	}

	state := evaluator.Channel(channelID)
	if state == domain.StateBlocked {
		return ChannelView{}, store.ErrNotFound
	}

	channel, err := s.catalog.Channel(ctx, channelID)
	if err != nil {
		return ChannelView{}, err
	}

	subscribed, err := s.activity.IsSubscribed(ctx, childID, channelID)
	if err != nil {
		return ChannelView{}, err
	}

	view := ChannelView{
		Channel:    channel,
		State:      state,
		Subscribed: subscribed,
	}

	if state != domain.StateAllowed {
		pending, err := s.hasPendingRequest(ctx, childID, channelID)
		if err != nil {
			return ChannelView{}, err
		}
		view.PendingRequest = pending
		return view, nil
	}

	page, err := s.filteredPage(ctx, childID, evaluator, store.FeedQuery{
		ChannelIDs: []string{channelID},
		Limit:      limit,
		Cursor:     cursor,
	})
	if err != nil {
		return ChannelView{}, err
	}
	view.Videos = page.Videos
	view.NextCursor = page.NextCursor
	return view, nil
}

// Watchable reports whether a child may play a video, and returns it.
func (s *Service) Watchable(ctx context.Context, familyID, childID uuid.UUID,
	videoID string) (store.Video, error) {

	video, err := s.catalog.Video(ctx, videoID)
	if err != nil {
		return store.Video{}, err
	}

	evaluator, err := s.rules.Evaluator(ctx, familyID, childID)
	if err != nil {
		return store.Video{}, err
	}

	if !evaluator.Video(toPolicyVideo(video)).Served() {
		// Every refusal reads as "no such video", so a child cannot map out
		// what exists by probing IDs.
		return store.Video{}, store.ErrNotFound
	}
	return video, nil
}

// watchableChannels resolves which channels a feed may draw from.
func (s *Service) watchableChannels(ctx context.Context, familyID, childID uuid.UUID,
	subscribedOnly bool) ([]string, *policy.Evaluator, error) {

	evaluator, err := s.rules.Evaluator(ctx, familyID, childID)
	if err != nil {
		return nil, nil, err
	}

	var candidates []string
	if subscribedOnly {
		candidates, err = s.activity.SubscribedChannelIDs(ctx, childID)
		if err != nil {
			return nil, nil, err
		}
	} else {
		candidates, err = s.allowedChannelIDs(ctx, familyID, childID)
		if err != nil {
			return nil, nil, err
		}
	}

	allowed := make([]string, 0, len(candidates))
	for _, id := range candidates {
		if evaluator.Channel(id) == domain.StateAllowed {
			allowed = append(allowed, id)
		}
	}
	return allowed, evaluator, nil
}

func (s *Service) allowedChannelIDs(ctx context.Context, familyID, childID uuid.UUID) ([]string, error) {
	global, err := s.rules.GlobalAllowlist(ctx, familyID)
	if err != nil {
		return nil, err
	}
	perChild, err := s.rules.ChildAllowlist(ctx, childID)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(global)+len(perChild))
	ids := make([]string, 0, len(global)+len(perChild))
	add := func(id string) {
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for _, row := range global {
		add(row.ChannelID)
	}
	for _, row := range perChild {
		add(row.ChannelID)
	}
	return ids, nil
}

// filteredPage refills a page when keyword suppression thins it. A short page
// reads to the client as the end of the feed, which is how active filters turn
// into an apparently empty app.
func (s *Service) filteredPage(ctx context.Context, childID uuid.UUID,
	evaluator *policy.Evaluator, query store.FeedQuery) (Page, error) {

	limit := query.Limit
	if limit <= 0 || limit > 100 {
		limit = 30
	}

	out := make([]store.Video, 0, limit)
	cursor := query.Cursor

	for round := 0; round < maxRefillRounds && len(out) < limit; round++ {
		q := query
		q.Cursor = cursor
		// Over-fetch so one round usually suffices even with active filters.
		q.Limit = min((limit-len(out))*2, 100)

		page, err := s.catalog.Videos(ctx, q)
		if err != nil {
			return Page{}, err
		}
		if len(page.Videos) == 0 {
			return Page{Videos: out}, nil
		}

		served, suppressed := evaluator.Filter(toPolicyVideos(page.Videos))
		if err := s.logSuppressions(ctx, childID, suppressed); err != nil {
			return Page{}, err
		}

		out = append(out, selectByID(page.Videos, served)...)
		cursor = page.NextCursor
		if cursor == "" {
			return Page{Videos: trim(out, limit)}, nil
		}
	}

	if len(out) > limit {
		// The extra rows are dropped rather than served, so the cursor stays
		// aligned with what the client actually received.
		out = out[:limit]
		cursor = encodeFrom(out)
	}
	return Page{Videos: out, NextCursor: cursor}, nil
}

func (s *Service) logSuppressions(ctx context.Context, childID uuid.UUID,
	suppressed []policy.Suppression) error {

	if len(suppressed) == 0 {
		return nil
	}
	if err := s.activity.LogSuppressions(ctx, childID, suppressed); err != nil {
		return fmt.Errorf("logging suppressions: %w", err)
	}
	return nil
}

func (s *Service) hasPendingRequest(ctx context.Context, childID uuid.UUID, channelID string) (bool, error) {
	requests, err := s.activity.ChildRequests(ctx, childID, 100)
	if err != nil {
		return false, err
	}
	for _, r := range requests {
		if r.ChannelID == channelID && r.Status == domain.RequestPending {
			return true, nil
		}
	}
	return false, nil
}

func toPolicyVideo(v store.Video) policy.Video {
	return policy.Video{
		ID:          v.ID,
		ChannelID:   v.ChannelID,
		Title:       v.Title,
		Description: v.Description,
		Tags:        v.Tags,
		LiveState:   v.LiveState,
	}
}

func toPolicyVideos(videos []store.Video) []policy.Video {
	out := make([]policy.Video, len(videos))
	for i, v := range videos {
		out[i] = toPolicyVideo(v)
	}
	return out
}

// selectByID keeps the store rows the evaluator served, in their original
// order, so ranking and pagination stay stable.
func selectByID(rows []store.Video, served []policy.Video) []store.Video {
	if len(served) == 0 {
		return nil
	}
	keep := make(map[string]struct{}, len(served))
	for _, v := range served {
		keep[v.ID] = struct{}{}
	}

	out := make([]store.Video, 0, len(served))
	for _, row := range rows {
		if _, ok := keep[row.ID]; ok {
			out = append(out, row)
		}
	}
	return out
}

func trim(videos []store.Video, limit int) []store.Video {
	if len(videos) > limit {
		return videos[:limit]
	}
	return videos
}

func encodeFrom(videos []store.Video) string {
	if len(videos) == 0 {
		return ""
	}
	last := videos[len(videos)-1]
	return store.EncodeCursor(last.PublishedAt, last.ID)
}

// shuffleDeterministically orders videos by a seed, so one browsing session
// keeps a stable order while different sessions differ.
func shuffleDeterministically(videos []store.Video, seed string) {
	// Sorting first makes the shuffle depend only on the seed, not on whatever
	// order the database happened to return.
	slices.SortFunc(videos, func(a, b store.Video) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})

	h := fnv.New64a()
	_, _ = h.Write([]byte(seed))
	sum := h.Sum64()

	rng := rand.New(rand.NewPCG(sum, sum>>32))
	rng.Shuffle(len(videos), func(i, j int) {
		videos[i], videos[j] = videos[j], videos[i]
	})
}
