package store

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
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
		tags := pq.StringArray(v.Tags)
		if tags == nil {
			tags = pq.StringArray{}
		}
		shortSource := v.ShortSource
		if shortSource == "" {
			shortSource = domain.ShortSourceDuration
		}
		liveState := v.LiveState
		if liveState == "" {
			liveState = domain.LiveNone
		}
		rows[i] = Video{
			ID:              v.ID,
			ChannelID:       v.ChannelID,
			Title:           v.Title,
			Description:     v.Description,
			Tags:            tags,
			DurationSeconds: int(v.Duration.Seconds()),
			PublishedAt:     v.PublishedAt,
			ThumbnailURL:    v.ThumbnailURL,
			IsShort:         v.IsShort,
			ShortSource:     shortSource,
			LiveState:       liveState,
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

// VideosByID reads cached videos in the caller's order. YouTube detail calls
// may omit deleted or private results, so missing IDs are skipped.
func (c *Catalog) VideosByID(ctx context.Context, ids []string) ([]Video, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	var rows []Video
	if err := c.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("reading videos: %w", err)
	}

	byID := make(map[string]Video, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}

	out := make([]Video, 0, len(rows))
	for _, id := range ids {
		if row, ok := byID[id]; ok {
			out = append(out, row)
		}
	}
	return out, nil
}

// StaleChannelIDs lists cached channels whose uploads are due a refresh.
func (c *Catalog) StaleChannelIDs(ctx context.Context, olderThan time.Time, limit int) ([]string, error) {
	var ids []string
	err := c.db.WithContext(ctx).Model(&Channel{}).
		Where("uploads_fetched_at IS NULL OR uploads_fetched_at < ?", olderThan).
		Order("uploads_fetched_at NULLS FIRST, id").
		Limit(limit).
		Pluck("id", &ids).Error
	return ids, wrap(err, "listing stale channels")
}

// StaleApprovedChannelIDs lists only channels that at least one child in the
// family may watch. Search results and explicitly blocked channels must not
// consume the family's feed budget merely because they exist in the cache.
func (c *Catalog) StaleApprovedChannelIDs(ctx context.Context, familyID uuid.UUID,
	olderThan time.Time, limit int) ([]string, error) {

	if limit <= 0 {
		limit = 500
	}

	var ids []string
	err := c.db.WithContext(ctx).Raw(`
		SELECT channel.id
		FROM channel
		WHERE (channel.uploads_fetched_at IS NULL OR channel.uploads_fetched_at < ?)
		  AND NOT EXISTS (
		      SELECT 1 FROM block_channel
		      WHERE block_channel.family_id = ?
		        AND block_channel.channel_id = channel.id
		  )
		  AND (
		      EXISTS (
		          SELECT 1 FROM allow_global
		          WHERE allow_global.family_id = ?
		            AND allow_global.channel_id = channel.id
		            AND EXISTS (
		                SELECT 1 FROM child
		                WHERE child.family_id = ?
		                  AND NOT EXISTS (
		                      SELECT 1 FROM deny_child
		                      WHERE deny_child.child_id = child.id
		                        AND deny_child.channel_id = channel.id
		                  )
		            )
		      )
		      OR EXISTS (
		          SELECT 1
		          FROM allow_child
		          JOIN child ON child.id = allow_child.child_id
		          WHERE child.family_id = ?
		            AND allow_child.channel_id = channel.id
		      )
		  )
		ORDER BY channel.uploads_fetched_at NULLS FIRST, channel.id
		LIMIT ?`, olderThan, familyID, familyID, familyID, familyID, limit).
		Scan(&ids).Error
	return ids, wrap(err, "listing stale approved channels")
}

// MarkChannelRefreshed advances only the uploads clock. Metadata writes use a
// separate timestamp because approving a freshly searched channel must still
// trigger its first ingest immediately.
func (c *Catalog) MarkChannelRefreshed(ctx context.Context, channelID string) error {
	now := c.now()
	result := c.db.WithContext(ctx).Model(&Channel{}).
		Where("id = ?", channelID).
		Updates(map[string]any{"uploads_fetched_at": now, "updated_at": now})
	if result.Error != nil {
		return fmt.Errorf("marking channel refreshed: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
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
	if q.Limit <= 0 || q.Limit > 2000 {
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
