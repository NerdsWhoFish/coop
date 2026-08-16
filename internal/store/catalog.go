package store

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm/clause"

	"github.com/nerdswhofish/coop/internal/domain"
	"github.com/nerdswhofish/coop/internal/youtube"
)

// Catalog caches the channel and video metadata Coop has fetched.
type Catalog struct {
	db  *DB
	now func() time.Time
}

// NewCatalog builds the repository. A nil now defaults to time.Now.
func NewCatalog(db *DB, now func() time.Time) *Catalog {
	if now == nil {
		now = time.Now
	}
	return &Catalog{db: db, now: now}
}

// UpsertChannels stores freshly fetched channel metadata.
func (c *Catalog) UpsertChannels(ctx context.Context, channels []youtube.Channel) error {
	if len(channels) == 0 {
		return nil
	}

	now := c.now()
	rows := make([]Channel, len(channels))
	for i, ch := range channels {
		rows[i] = Channel{
			ID:                ch.ID,
			Title:             ch.Title,
			Description:       ch.Description,
			ThumbnailURL:      ch.ThumbnailURL,
			BannerURL:         ch.BannerURL,
			SubscriberCount:   ch.SubscriberCount,
			UploadsPlaylistID: ch.UploadsPlaylistID,
			FetchedAt:         now,
		}
	}

	err := c.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"title", "description", "thumbnail_url", "banner_url",
			"subscriber_count", "uploads_playlist_id", "fetched_at", "updated_at",
		}),
	}).Create(&rows).Error
	return wrap(err, "upserting channels")
}

// UpsertVideos stores freshly fetched video metadata. is_short is left alone
// on update: this path only carries a duration guess, and overwriting an
// authoritative RSS classification with one would be a downgrade.
func (c *Catalog) UpsertVideos(ctx context.Context, videos []youtube.Video) error {
	if len(videos) == 0 {
		return nil
	}

	now := c.now()
	rows := make([]Video, len(videos))
	for i, v := range videos {
		rows[i] = Video{
			ID:              v.ID,
			ChannelID:       v.ChannelID,
			Title:           v.Title,
			Description:     v.Description,
			Tags:            pq.StringArray(v.Tags),
			DurationSeconds: int(v.Duration.Seconds()),
			PublishedAt:     v.PublishedAt,
			ThumbnailURL:    v.ThumbnailURL,
			IsShort:         v.IsShort,
			ShortSource:     v.ShortSource,
			LiveState:       v.LiveState,
			MadeForKids:     v.MadeForKids,
			FetchedAt:       now,
		}
	}

	err := c.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"channel_id", "title", "description", "tags", "duration_seconds",
			"published_at", "thumbnail_url", "live_state", "made_for_kids",
			"fetched_at", "updated_at",
		}),
	}).Create(&rows).Error
	return wrap(err, "upserting videos")
}

// ApplyFeedClassification records the authoritative Shorts signal from a
// channel's Atom feed, which outranks any duration guess already stored.
func (c *Catalog) ApplyFeedClassification(ctx context.Context, entries []youtube.FeedEntry) error {
	if len(entries) == 0 {
		return nil
	}

	shorts := make([]string, 0, len(entries))
	regular := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsShort {
			shorts = append(shorts, e.VideoID)
		} else {
			regular = append(regular, e.VideoID)
		}
	}

	apply := func(ids []string, isShort bool) error {
		if len(ids) == 0 {
			return nil
		}
		return c.db.WithContext(ctx).Model(&Video{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"is_short":     isShort,
				"short_source": domain.ShortSourceRSS,
				"updated_at":   c.now(),
			}).Error
	}

	if err := apply(shorts, true); err != nil {
		return fmt.Errorf("marking shorts: %w", err)
	}
	if err := apply(regular, false); err != nil {
		return fmt.Errorf("marking regular videos: %w", err)
	}
	return nil
}

// Channel reads one cached channel.
func (c *Catalog) Channel(ctx context.Context, id string) (Channel, error) {
	var row Channel
	err := c.db.WithContext(ctx).First(&row, "id = ?", id).Error
	return row, wrap(err, "reading channel")
}

// ChannelsByID reads several cached channels, keyed for easy joining.
func (c *Catalog) ChannelsByID(ctx context.Context, ids []string) (map[string]Channel, error) {
	if len(ids) == 0 {
		return map[string]Channel{}, nil
	}

	var rows []Channel
	if err := c.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("reading channels: %w", err)
	}

	out := make(map[string]Channel, len(rows))
	for _, row := range rows {
		out[row.ID] = row
	}
	return out, nil
}

// Video reads one cached video.
func (c *Catalog) Video(ctx context.Context, id string) (Video, error) {
	var row Video
	err := c.db.WithContext(ctx).First(&row, "id = ?", id).Error
	return row, wrap(err, "reading video")
}

// StaleChannelIDs lists cached channels whose uploads are due a refresh.
func (c *Catalog) StaleChannelIDs(ctx context.Context, olderThan time.Time, limit int) ([]string, error) {
	var ids []string
	err := c.db.WithContext(ctx).Model(&Channel{}).
		Where("fetched_at < ?", olderThan).
		Order("fetched_at").
		Limit(limit).
		Pluck("id", &ids).Error
	return ids, wrap(err, "listing stale channels")
}

// FeedQuery selects videos for a feed page.
type FeedQuery struct {
	ChannelIDs []string
	// ShortsOnly and ExcludeShorts are mutually exclusive; leaving both false
	// returns everything, which is what the home feed wants.
	ShortsOnly    bool
	ExcludeShorts bool
	Limit         int
	Cursor        string
}

// FeedPage is one page of videos plus the cursor for the next.
type FeedPage struct {
	Videos     []Video
	NextCursor string
}

// Videos returns a page from the given channels, newest first. Live content is
// excluded in SQL, not in policy: no configuration should ever surface it, and
// filtering here keeps it out of every page count too.
func (c *Catalog) Videos(ctx context.Context, q FeedQuery) (FeedPage, error) {
	if len(q.ChannelIDs) == 0 {
		return FeedPage{}, nil
	}
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 30
	}

	query := c.db.WithContext(ctx).Model(&Video{}).
		Where("channel_id IN ?", q.ChannelIDs).
		Where("live_state = ?", domain.LiveNone)

	if q.ShortsOnly {
		query = query.Where("is_short = ?", true)
	}
	if q.ExcludeShorts {
		query = query.Where("is_short = ?", false)
	}

	if q.Cursor != "" {
		publishedAt, videoID, err := decodeCursor(q.Cursor)
		if err != nil {
			return FeedPage{}, err
		}
		// Keyset pagination on the same (published_at DESC, id DESC) ordering
		// the index provides, so a page boundary cannot skip or repeat a row
		// when new videos arrive mid-scroll.
		query = query.Where("(published_at, id) < (?, ?)", publishedAt, videoID)
	}

	var rows []Video
	err := query.
		Order("published_at DESC, id DESC").
		Limit(q.Limit + 1).
		Find(&rows).Error
	if err != nil {
		return FeedPage{}, fmt.Errorf("reading feed: %w", err)
	}

	page := FeedPage{}
	if len(rows) > q.Limit {
		last := rows[q.Limit-1]
		page.NextCursor = EncodeCursor(last.PublishedAt, last.ID)
		rows = rows[:q.Limit]
	}
	page.Videos = rows
	return page, nil
}

// EncodeCursor builds the keyset cursor for a page boundary. Exported so a
// caller that trims a page can keep the cursor aligned with what it served.
func EncodeCursor(publishedAt time.Time, videoID string) string {
	raw := strconv.FormatInt(publishedAt.UTC().UnixNano(), 10) + ":" + videoID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("malformed cursor: %w", err)
	}
	before, after, found := strings.Cut(string(raw), ":")
	if !found {
		return time.Time{}, "", fmt.Errorf("malformed cursor")
	}
	nanos, err := strconv.ParseInt(before, 10, 64)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("malformed cursor timestamp: %w", err)
	}
	return time.Unix(0, nanos).UTC(), after, nil
}
