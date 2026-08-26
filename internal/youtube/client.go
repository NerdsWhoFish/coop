package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/domain"
)

const (
	endpointChannels      = "channels.list"
	endpointPlaylistItems = "playlistItems.list"
	endpointVideos        = "videos.list"
	endpointSearch        = "search.list"
	endpointFeed          = "feed"
)

// MaxIDsPerCall is the batch size Google accepts for id-list endpoints. Cost
// is per call, not per id, so batching to 50 is a 50x saving.
const MaxIDsPerCall = 50

const (
	defaultAPIBase  = "https://www.googleapis.com/youtube/v3"
	defaultFeedBase = "https://www.youtube.com/feeds/videos.xml"
)

// ErrNotFound reports that YouTube returned no item for an id.
var ErrNotFound = errors.New("not found on youtube")

type apiResponseError struct {
	status  int
	reason  string
	message string
}

func (e *apiResponseError) Retryable() bool {
	if e.status == http.StatusNotFound {
		return e.reason == "" && e.message == ""
	}
	return e.status == http.StatusRequestTimeout ||
		e.status == http.StatusTooManyRequests ||
		e.status >= http.StatusInternalServerError
}

type transportError struct {
	cause error
}

func (e *transportError) Error() string   { return "calling youtube: " + e.cause.Error() }
func (e *transportError) Retryable() bool { return true }

func (e *apiResponseError) Error() string {
	if e.message != "" {
		return fmt.Sprintf("youtube api %d (%s): %s", e.status, e.reason, e.message)
	}
	return fmt.Sprintf("youtube api returned %d", e.status)
}

// IsRetryable reports whether another attempt may succeed without changing
// the request. Structured 404s are permanent; bare 404s have proven transient
// at YouTube's edge and are safe to defer and retry.
func IsRetryable(err error) bool {
	var retryable interface{ Retryable() bool }
	return errors.As(err, &retryable) && retryable.Retryable()
}

// Channel is the channel metadata Coop stores.
type Channel struct {
	ID                string
	Title             string
	Description       string
	ThumbnailURL      string
	BannerURL         string
	SubscriberCount   int64
	UploadsPlaylistID string
}

// Video is the video metadata Coop stores.
type Video struct {
	ID           string
	ChannelID    string
	ChannelTitle string
	Title        string
	Description  string
	Tags         []string
	Duration     time.Duration
	PublishedAt  time.Time
	ThumbnailURL string
	LiveState    domain.LiveState
	MadeForKids  bool
	Embeddable   bool
	IsShort      bool
	ShortSource  domain.ShortSource
}

// SearchResults is one mixed channel-and-video search.
// RelatedChannels contains metadata for every direct channel match and every
// channel owning a video match, which lets callers satisfy video foreign keys
// without exposing unrelated channels as direct search results.
type SearchResults struct {
	Channels        []Channel
	RelatedChannels []Channel
	Videos          []Video
}

// Config builds a Client. One Client serves one family, because the API key
// and the spend ledger are both per-family.
type Config struct {
	APIKey   string
	FamilyID uuid.UUID
	Cache    Cache
	Ledger   Ledger
	Budget   Budget
	TTLs     TTLs

	HTTP        *http.Client
	Now         func() time.Time
	APIBaseURL  string
	FeedBaseURL string
}

// Client reads YouTube through a cache and a daily spend ledger.
type Client struct {
	cfg Config
}

// New validates cfg and applies defaults.
func New(cfg Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("youtube: an API key is required")
	}
	if cfg.Cache == nil {
		return nil, errors.New("youtube: a cache is required")
	}
	if cfg.Ledger == nil {
		return nil, errors.New("youtube: a ledger is required")
	}

	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 20 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = defaultAPIBase
	}
	if cfg.FeedBaseURL == "" {
		cfg.FeedBaseURL = defaultFeedBase
	}
	if cfg.TTLs == (TTLs{}) {
		cfg.TTLs = DefaultTTLs()
	}
	return &Client{cfg: cfg}, nil
}

// Usage reports today's spend against each budget.
func (c *Client) Usage(ctx context.Context) (map[domain.QuotaPurpose]Spend, error) {
	return c.cfg.Ledger.Usage(ctx, c.cfg.FamilyID, QuotaDay(c.cfg.Now()))
}

// Budget reports the configured ceilings.
func (c *Client) Budget() Budget { return c.cfg.Budget }

// Channels fetches metadata for up to MaxIDsPerCall channels in one call.
func (c *Client) Channels(ctx context.Context, ids []string, purpose domain.QuotaPurpose) ([]Channel, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > MaxIDsPerCall {
		return nil, fmt.Errorf("youtube: %d channel ids exceeds the %d per call limit", len(ids), MaxIDsPerCall)
	}

	params := url.Values{}
	params.Set("part", "snippet,statistics,brandingSettings")
	params.Set("id", strings.Join(ids, ","))
	params.Set("maxResults", strconv.Itoa(MaxIDsPerCall))

	body, err := c.fetch(ctx, endpointChannels, params, purpose, 1)
	if err != nil {
		return nil, err
	}

	var resp channelListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding channels.list: %w", err)
	}

	out := make([]Channel, 0, len(resp.Items))
	for _, item := range resp.Items {
		uploads, err := UploadsPlaylistID(item.ID)
		if err != nil {
			// A channel ID Google returned that we cannot parse is a bug on
			// one side, and skipping it beats failing the whole batch.
			continue
		}
		out = append(out, Channel{
			ID:                item.ID,
			Title:             item.Snippet.Title,
			Description:       item.Snippet.Description,
			ThumbnailURL:      item.Snippet.Thumbnails.best(),
			BannerURL:         item.BrandingSettings.Image.BannerExternalURL,
			SubscriberCount:   parseCount(item.Statistics.SubscriberCount),
			UploadsPlaylistID: uploads,
		})
	}
	return out, nil
}

// UploadIDs lists a channel's most recent uploads, newest first. The uploads
// playlist is derived rather than fetched, so a channel's first refresh costs
// one call instead of two.
func (c *Client) UploadIDs(ctx context.Context, channelID string, limit int,
	purpose domain.QuotaPurpose) ([]string, error) {

	playlist, err := UploadsPlaylistID(channelID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > MaxIDsPerCall {
		limit = MaxIDsPerCall
	}

	params := url.Values{}
	params.Set("part", "contentDetails")
	params.Set("playlistId", playlist)
	params.Set("maxResults", strconv.Itoa(limit))

	body, err := c.fetch(ctx, endpointPlaylistItems, params, purpose, 1)
	if err != nil {
		return nil, err
	}

	var resp playlistItemsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding playlistItems.list: %w", err)
	}

	ids := make([]string, 0, len(resp.Items))
	for _, item := range resp.Items {
		if item.ContentDetails.VideoID != "" {
			ids = append(ids, item.ContentDetails.VideoID)
		}
	}
	return ids, nil
}

// Videos fetches metadata for up to MaxIDsPerCall videos in one call.
// liveStreamingDetails is requested because liveBroadcastContent alone lets
// finished streams through. Shorts are guessed here; ChannelFeed is truth.
func (c *Client) Videos(ctx context.Context, ids []string, purpose domain.QuotaPurpose) ([]Video, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > MaxIDsPerCall {
		return nil, fmt.Errorf("youtube: %d video ids exceeds the %d per call limit", len(ids), MaxIDsPerCall)
	}

	params := url.Values{}
	params.Set("part", "snippet,contentDetails,status,liveStreamingDetails")
	params.Set("id", strings.Join(ids, ","))
	params.Set("maxResults", strconv.Itoa(MaxIDsPerCall))

	body, err := c.fetch(ctx, endpointVideos, params, purpose, 1)
	if err != nil {
		return nil, err
	}

	var resp videoListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding videos.list: %w", err)
	}

	out := make([]Video, 0, len(resp.Items))
	for _, item := range resp.Items {
		duration, _ := ParseISODuration(item.ContentDetails.Duration)

		v := Video{
			ID:           item.ID,
			ChannelID:    item.Snippet.ChannelID,
			ChannelTitle: item.Snippet.ChannelTitle,
			Title:        item.Snippet.Title,
			Description:  item.Snippet.Description,
			Tags:         item.Snippet.Tags,
			Duration:     duration,
			ThumbnailURL: item.Snippet.Thumbnails.best(),
			MadeForKids:  item.Status.MadeForKids,
			Embeddable:   item.Status.Embeddable,
			LiveState: ClassifyLive(
				item.Snippet.LiveBroadcastContent,
				item.LiveStreamingDetails != nil,
			),
			IsShort:     ClassifyShortFromDuration(duration),
			ShortSource: domain.ShortSourceDuration,
		}
		if t, err := time.Parse(time.RFC3339, item.Snippet.PublishedAt); err == nil {
			v.PublishedAt = t
		}
		out = append(out, v)
	}
	return out, nil
}

// Search finds both channels and videos with one search.list call, preserving
// the scarce search bucket. The cheap detail calls are required before a video
// can pass live-state and keyword policy or be stored safely.
func (c *Client) Search(ctx context.Context, query string) (SearchResults, error) {
	return c.search(ctx, query, "")
}

// SearchChannel finds videos matching query within one channel.
func (c *Client) SearchChannel(ctx context.Context, query, channelID string) (SearchResults, error) {
	if !ValidChannelID(channelID) {
		return SearchResults{}, fmt.Errorf("not a channel ID: %q", channelID)
	}
	return c.search(ctx, query, channelID)
}

func (c *Client) search(ctx context.Context, query, channelID string) (SearchResults, error) {
	normalized := NormalizeQuery(query)
	if normalized == "" {
		return SearchResults{}, nil
	}

	params := url.Values{}
	params.Set("part", "snippet")
	if channelID == "" {
		params.Set("type", "channel,video")
	} else {
		params.Set("type", "video")
		params.Set("channelId", channelID)
	}
	params.Set("q", normalized)
	params.Set("maxResults", "25")

	body, err := c.fetch(ctx, endpointSearch, params, domain.PurposeSearch, 1)
	if err != nil {
		return SearchResults{}, err
	}

	var resp searchListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return SearchResults{}, fmt.Errorf("decoding search.list: %w", err)
	}

	directChannels := make([]string, 0, len(resp.Items))
	channelIDs := make([]string, 0, len(resp.Items))
	videoIDs := make([]string, 0, len(resp.Items))
	seenChannels := make(map[string]struct{}, len(resp.Items))
	seenDirectChannels := make(map[string]struct{}, len(resp.Items))
	seenVideos := make(map[string]struct{}, len(resp.Items))

	addChannel := func(id string) {
		if !ValidChannelID(id) {
			return
		}
		if _, seen := seenChannels[id]; seen {
			return
		}
		seenChannels[id] = struct{}{}
		channelIDs = append(channelIDs, id)
	}

	for _, item := range resp.Items {
		if id := item.ID.ChannelID; ValidChannelID(id) {
			if _, seen := seenDirectChannels[id]; !seen {
				seenDirectChannels[id] = struct{}{}
				directChannels = append(directChannels, id)
			}
			addChannel(id)
		}
		if id := item.ID.VideoID; id != "" && ValidChannelID(item.Snippet.ChannelID) {
			addChannel(item.Snippet.ChannelID)
			if _, seen := seenVideos[id]; !seen {
				seenVideos[id] = struct{}{}
				videoIDs = append(videoIDs, id)
			}
		}
	}

	channels, err := c.Channels(ctx, channelIDs, domain.PurposeFeed)
	if err != nil {
		return SearchResults{}, fmt.Errorf("hydrating search channels: %w", err)
	}
	videos, err := c.Videos(ctx, videoIDs, domain.PurposeFeed)
	if err != nil {
		return SearchResults{}, fmt.Errorf("hydrating search videos: %w", err)
	}

	byID := make(map[string]Channel, len(channels))
	for _, channel := range channels {
		byID[channel.ID] = channel
	}

	videoByID := make(map[string]Video, len(videos))
	for _, video := range videos {
		videoByID[video.ID] = video
	}
	orderedVideos := make([]Video, 0, len(videos))
	// A result the official player cannot embed is a dead tile. A video whose
	// channel disappeared between calls also cannot satisfy storage's channel
	// foreign key. Neither reaches storage or policy evaluation.
	for _, id := range videoIDs {
		video, ok := videoByID[id]
		if !ok {
			continue
		}
		_, channelExists := byID[video.ChannelID]
		if video.Embeddable && channelExists {
			orderedVideos = append(orderedVideos, video)
		}
	}

	matches := make([]Channel, 0, len(directChannels))
	for _, id := range directChannels {
		if channel, ok := byID[id]; ok {
			matches = append(matches, channel)
		}
	}

	return SearchResults{
		Channels:        matches,
		RelatedChannels: channels,
		Videos:          orderedVideos,
	}, nil
}

// SearchChannels finds channels by name. It spends from the search bucket,
// which Google meters separately at SearchCallsPerDay.
func (c *Client) SearchChannels(ctx context.Context, query string) ([]Channel, error) {
	normalized := NormalizeQuery(query)
	if normalized == "" {
		return nil, nil
	}

	params := url.Values{}
	params.Set("part", "snippet")
	params.Set("type", "channel")
	params.Set("q", normalized)
	params.Set("maxResults", "25")

	body, err := c.fetch(ctx, endpointSearch, params, domain.PurposeSearch, 1)
	if err != nil {
		return nil, err
	}

	var resp searchListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding search.list: %w", err)
	}

	out := make([]Channel, 0, len(resp.Items))
	for _, item := range resp.Items {
		id := item.ID.ChannelID
		if !ValidChannelID(id) {
			continue
		}
		uploads, err := UploadsPlaylistID(id)
		if err != nil {
			continue
		}
		out = append(out, Channel{
			ID:                id,
			Title:             item.Snippet.Title,
			Description:       item.Snippet.Description,
			ThumbnailURL:      item.Snippet.Thumbnails.best(),
			UploadsPlaylistID: uploads,
		})
	}
	return out, nil
}

// ChannelFeed reads a channel's Atom feed, which costs no API quota and is the
// authoritative source for whether a video is a Short.
func (c *Client) ChannelFeed(ctx context.Context, channelID string) ([]FeedEntry, error) {
	if !ValidChannelID(channelID) {
		return nil, fmt.Errorf("not a channel ID: %q", channelID)
	}

	params := url.Values{}
	params.Set("channel_id", channelID)
	key := CacheKey(endpointFeed, params)

	if body, ok, err := c.cfg.Cache.Get(ctx, key); err == nil && ok {
		return ParseFeed(strings.NewReader(string(body)))
	}

	body, err := c.get(ctx, c.cfg.FeedBaseURL+"?"+params.Encode())
	if err != nil {
		return nil, err
	}

	// A cache write failure costs a repeat fetch, not correctness, and the
	// feed is free, so it must not fail the request.
	_ = c.cfg.Cache.Put(ctx, key, endpointFeed, body, c.cfg.TTLs.For(endpointFeed))

	return ParseFeed(strings.NewReader(string(body)))
}

// fetch serves from cache when possible, and otherwise checks the budget,
// calls Google, records the spend, and caches the result.
func (c *Client) fetch(ctx context.Context, endpoint string, params url.Values,
	purpose domain.QuotaPurpose, cost int) ([]byte, error) {

	key := CacheKey(endpoint, params)
	if body, ok, err := c.cfg.Cache.Get(ctx, key); err == nil && ok {
		return body, nil
	}

	now := c.cfg.Now()
	day := QuotaDay(now)

	usage, err := c.cfg.Ledger.Usage(ctx, c.cfg.FamilyID, day)
	if err != nil {
		return nil, fmt.Errorf("reading quota usage: %w", err)
	}
	if err := check(usage, c.cfg.Budget, purpose, cost, now); err != nil {
		return nil, err
	}

	withKey := make(url.Values, len(params)+1)
	maps.Copy(withKey, params)
	withKey.Set("key", c.cfg.APIKey)

	path, _ := strings.CutSuffix(endpoint, ".list")
	body, err := c.get(ctx, c.cfg.APIBaseURL+"/"+path+"?"+withKey.Encode())
	if err != nil {
		return nil, err
	}

	// Search is metered by call in its own bucket; everything else in units.
	units, calls := cost, 1
	if purpose == domain.PurposeSearch {
		units = 0
	}
	if err := c.cfg.Ledger.Record(ctx, c.cfg.FamilyID, day, purpose, units, calls); err != nil {
		return nil, fmt.Errorf("recording quota spend: %w", err)
	}

	_ = c.cfg.Cache.Put(ctx, key, endpoint, body, c.cfg.TTLs.For(endpoint))
	return body, nil
}

func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.cfg.HTTP.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		cause := err
		for {
			urlErr, ok := cause.(*url.Error)
			if !ok {
				break
			}
			cause = urlErr.Err
		}
		return nil, &transportError{cause: cause}
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp.StatusCode, body)
	}
	return body, nil
}

// apiError turns Google's error envelope into something actionable, since
// "403" alone does not distinguish a bad key from an exhausted quota.
func apiError(status int, body []byte) error {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Errors  []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &envelope)

	reason := ""
	if len(envelope.Error.Errors) > 0 {
		reason = envelope.Error.Errors[0].Reason
	}
	if reason == "quotaExceeded" || reason == "rateLimitExceeded" {
		return fmt.Errorf("%w: google reports %s", ErrBudgetExhausted, reason)
	}
	return &apiResponseError{status: status, reason: reason, message: envelope.Error.Message}
}

func parseCount(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

type thumbnails struct {
	Default  thumbnail `json:"default"`
	Medium   thumbnail `json:"medium"`
	High     thumbnail `json:"high"`
	Standard thumbnail `json:"standard"`
	Maxres   thumbnail `json:"maxres"`
}

type thumbnail struct {
	URL string `json:"url"`
}

// best picks the largest thumbnail YouTube supplied, since not every video
// carries every size.
func (t thumbnails) best() string {
	for _, candidate := range []thumbnail{t.Maxres, t.Standard, t.High, t.Medium, t.Default} {
		if candidate.URL != "" {
			return candidate.URL
		}
	}
	return ""
}

type channelListResponse struct {
	Items []struct {
		ID      string `json:"id"`
		Snippet struct {
			Title       string     `json:"title"`
			Description string     `json:"description"`
			Thumbnails  thumbnails `json:"thumbnails"`
		} `json:"snippet"`
		Statistics struct {
			SubscriberCount string `json:"subscriberCount"`
		} `json:"statistics"`
		BrandingSettings struct {
			Image struct {
				BannerExternalURL string `json:"bannerExternalUrl"`
			} `json:"image"`
		} `json:"brandingSettings"`
	} `json:"items"`
}

type playlistItemsResponse struct {
	Items []struct {
		ContentDetails struct {
			VideoID string `json:"videoId"`
		} `json:"contentDetails"`
	} `json:"items"`
	NextPageToken string `json:"nextPageToken"`
}

type videoListResponse struct {
	Items []struct {
		ID      string `json:"id"`
		Snippet struct {
			ChannelID            string     `json:"channelId"`
			ChannelTitle         string     `json:"channelTitle"`
			Title                string     `json:"title"`
			Description          string     `json:"description"`
			Tags                 []string   `json:"tags"`
			PublishedAt          string     `json:"publishedAt"`
			Thumbnails           thumbnails `json:"thumbnails"`
			LiveBroadcastContent string     `json:"liveBroadcastContent"`
		} `json:"snippet"`
		ContentDetails struct {
			Duration string `json:"duration"`
		} `json:"contentDetails"`
		Status struct {
			MadeForKids bool `json:"madeForKids"`
			Embeddable  bool `json:"embeddable"`
		} `json:"status"`
		LiveStreamingDetails *struct{} `json:"liveStreamingDetails"`
	} `json:"items"`
}

type searchListResponse struct {
	Items []struct {
		ID struct {
			ChannelID string `json:"channelId"`
			VideoID   string `json:"videoId"`
		} `json:"id"`
		Snippet struct {
			ChannelID    string     `json:"channelId"`
			ChannelTitle string     `json:"channelTitle"`
			Title        string     `json:"title"`
			Description  string     `json:"description"`
			Thumbnails   thumbnails `json:"thumbnails"`
		} `json:"snippet"`
	} `json:"items"`
}
