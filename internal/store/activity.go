package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nerdswhofish/coop/internal/domain"
	"github.com/nerdswhofish/coop/internal/policy"
)

// Activity holds what a child does: subscriptions, reactions, watch history,
// requests, and the log of what a keyword hid from them.
type Activity struct {
	db  *DB
	now func() time.Time
}

// NewActivity builds the repository. A nil now defaults to time.Now.
func NewActivity(db *DB, now func() time.Time) *Activity {
	if now == nil {
		now = time.Now
	}
	return &Activity{db: db, now: now}
}

// Subscribe records a local subscription. Nothing is written to YouTube.
func (a *Activity) Subscribe(ctx context.Context, childID uuid.UUID, channelID string) error {
	row := Subscription{ChildID: childID, ChannelID: channelID}
	return wrap(a.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&row).Error, "subscribing")
}

// Unsubscribe removes a local subscription.
func (a *Activity) Unsubscribe(ctx context.Context, childID uuid.UUID, channelID string) error {
	return wrap(a.db.WithContext(ctx).
		Delete(&Subscription{}, "child_id = ? AND channel_id = ?", childID, channelID).Error,
		"unsubscribing")
}

// SubscribedChannelIDs lists what a child follows.
func (a *Activity) SubscribedChannelIDs(ctx context.Context, childID uuid.UUID) ([]string, error) {
	var ids []string
	err := a.db.WithContext(ctx).Model(&Subscription{}).
		Where("child_id = ?", childID).
		Pluck("channel_id", &ids).Error
	return ids, wrap(err, "listing subscriptions")
}

// IsSubscribed reports whether a child follows a channel.
func (a *Activity) IsSubscribed(ctx context.Context, childID uuid.UUID, channelID string) (bool, error) {
	var n int64
	err := a.db.WithContext(ctx).Model(&Subscription{}).
		Where("child_id = ? AND channel_id = ?", childID, channelID).
		Count(&n).Error
	return n > 0, wrap(err, "checking subscription")
}

// SetReaction records a local like or dislike.
func (a *Activity) SetReaction(ctx context.Context, childID uuid.UUID, videoID string,
	kind domain.ReactionKind) error {

	row := Reaction{ChildID: childID, VideoID: videoID, Kind: kind}
	err := a.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "child_id"}, {Name: "video_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"kind", "updated_at"}),
	}).Create(&row).Error
	return wrap(err, "setting reaction")
}

// ClearReaction removes a like or dislike.
func (a *Activity) ClearReaction(ctx context.Context, childID uuid.UUID, videoID string) error {
	return wrap(a.db.WithContext(ctx).
		Delete(&Reaction{}, "child_id = ? AND video_id = ?", childID, videoID).Error,
		"clearing reaction")
}

// Reaction reads a child's reaction to one video, if any.
func (a *Activity) Reaction(ctx context.Context, childID uuid.UUID, videoID string) (domain.ReactionKind, bool, error) {
	var row Reaction
	err := a.db.WithContext(ctx).
		First(&row, "child_id = ? AND video_id = ?", childID, videoID).Error
	if err != nil {
		if wrapped := wrap(err, "reading reaction"); wrapped == ErrNotFound {
			return "", false, nil
		}
		return "", false, fmt.Errorf("reading reaction: %w", err)
	}
	return row.Kind, true, nil
}

// RecordWatch stores viewing progress for the ranker. The completion fraction
// is derived here rather than trusted from the client, so a device cannot
// inflate its own ranking signal.
func (a *Activity) RecordWatch(ctx context.Context, childID uuid.UUID, videoID string,
	startedAt time.Time, secondsWatched, durationSeconds int) error {

	if secondsWatched < 0 {
		secondsWatched = 0
	}

	fraction := 0.0
	if durationSeconds > 0 {
		fraction = min(float64(secondsWatched)/float64(durationSeconds), 1)
	}

	row := WatchEvent{
		ChildID:            childID,
		VideoID:            videoID,
		StartedAt:          startedAt,
		SecondsWatched:     secondsWatched,
		CompletionFraction: fraction,
	}
	return wrap(a.db.WithContext(ctx).Create(&row).Error, "recording watch event")
}

// RaiseRequest records a child asking for a channel. A pending ask is updated
// rather than duplicated, matching the partial unique index, so re-asking
// cannot flood the parent's queue.
func (a *Activity) RaiseRequest(ctx context.Context, childID uuid.UUID, channelID string,
	promptedByVideoID *string) (Request, error) {

	row := Request{
		ChildID:           childID,
		ChannelID:         channelID,
		PromptedByVideoID: promptedByVideoID,
		Status:            domain.RequestPending,
	}

	err := a.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:     []clause.Column{{Name: "child_id"}, {Name: "channel_id"}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Eq{Column: "status", Value: string(domain.RequestPending)}}},
		DoUpdates:   clause.AssignmentColumns([]string{"prompted_by_video_id", "updated_at"}),
	}).Create(&row).Error
	if err != nil {
		return Request{}, fmt.Errorf("raising request: %w", err)
	}
	return row, nil
}

// Request reads one request.
func (a *Activity) Request(ctx context.Context, requestID uuid.UUID) (Request, error) {
	var row Request
	err := a.db.WithContext(ctx).First(&row, "id = ?", requestID).Error
	return row, wrap(err, "reading request")
}

// RequestQuery selects requests for the parent queue.
type RequestQuery struct {
	ChildIDs []uuid.UUID
	Status   domain.RequestStatus
	Limit    int
}

// Requests lists requests from children the caller may see, newest first.
func (a *Activity) Requests(ctx context.Context, q RequestQuery) ([]Request, error) {
	if len(q.ChildIDs) == 0 {
		return nil, nil
	}
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 50
	}

	query := a.db.WithContext(ctx).Where("child_id IN ?", q.ChildIDs)
	if q.Status != "" {
		query = query.Where("status = ?", q.Status)
	}

	var rows []Request
	err := query.Order("created_at DESC").Limit(q.Limit).Find(&rows).Error
	return rows, wrap(err, "listing requests")
}

// ChildRequests lists a child's own asks and their outcomes.
func (a *Activity) ChildRequests(ctx context.Context, childID uuid.UUID, limit int) ([]Request, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var rows []Request
	err := a.db.WithContext(ctx).
		Where("child_id = ?", childID).
		Order("created_at DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, wrap(err, "listing child requests")
}

// DecideRequest closes a request. Only a pending request can be decided, so a
// second approval racing a denial cannot overwrite the first decision.
func (a *Activity) DecideRequest(ctx context.Context, requestID, parentID uuid.UUID,
	status domain.RequestStatus, note string) error {

	now := a.now()
	result := a.db.WithContext(ctx).Model(&Request{}).
		Where("id = ? AND status = ?", requestID, domain.RequestPending).
		Updates(map[string]any{
			"status":        status,
			"decided_by":    parentID,
			"decided_at":    now,
			"decision_note": note,
			"updated_at":    now,
		})
	if result.Error != nil {
		return fmt.Errorf("deciding request: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// PendingRequestCounts reports how many asks each child has waiting.
func (a *Activity) PendingRequestCounts(ctx context.Context, childIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	if len(childIDs) == 0 {
		return map[uuid.UUID]int{}, nil
	}

	var rows []struct {
		ChildID uuid.UUID
		Count   int
	}
	err := a.db.WithContext(ctx).Model(&Request{}).
		Select("child_id, count(*) as count").
		Where("child_id IN ? AND status = ?", childIDs, domain.RequestPending).
		Group("child_id").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("counting pending requests: %w", err)
	}

	out := make(map[uuid.UUID]int, len(rows))
	for _, row := range rows {
		out[row.ChildID] = row.Count
	}
	return out, nil
}

// LogSuppressions records what a keyword hid, for the parent's review.
// Duplicates are ignored: a feed rebuild re-evaluates the same videos, and
// re-logging each time would grow the table without bound.
func (a *Activity) LogSuppressions(ctx context.Context, childID uuid.UUID,
	suppressions []policy.Suppression) error {

	if len(suppressions) == 0 {
		return nil
	}

	rows := make([]Suppression, 0, len(suppressions))
	for _, s := range suppressions {
		keywordID, err := uuid.Parse(s.KeywordID)
		if err != nil {
			continue
		}
		rows = append(rows, Suppression{
			ChildID:      childID,
			VideoID:      s.VideoID,
			KeywordID:    keywordID,
			MatchedField: string(s.Field),
			MatchedTerm:  s.Term,
		})
	}
	if len(rows) == 0 {
		return nil
	}

	err := a.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&rows).Error
	return wrap(err, "logging suppressions")
}

// Suppressions lists what was hidden from a child, newest first.
func (a *Activity) Suppressions(ctx context.Context, childID uuid.UUID, limit int) ([]Suppression, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var rows []Suppression
	err := a.db.WithContext(ctx).
		Where("child_id = ?", childID).
		Order("created_at DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, wrap(err, "listing suppressions")
}

// Suppression reads one suppression.
func (a *Activity) Suppression(ctx context.Context, id uuid.UUID) (Suppression, error) {
	var row Suppression
	err := a.db.WithContext(ctx).First(&row, "id = ?", id).Error
	return row, wrap(err, "reading suppression")
}

// SearchCount reports how many searches a child has run on a given day.
func (a *Activity) SearchCount(ctx context.Context, childID uuid.UUID, day string) (int, error) {
	var row ChildSearch
	err := a.db.WithContext(ctx).
		First(&row, "child_id = ? AND day = ?", childID, day).Error
	if err != nil {
		if wrapped := wrap(err, "counting searches"); wrapped == ErrNotFound {
			return 0, nil
		}
		return 0, fmt.Errorf("counting searches: %w", err)
	}
	return row.Count, nil
}

// RecordSearch increments a child's daily search count. The increment lives in
// the conflict clause so concurrent searches cannot lose each other.
func (a *Activity) RecordSearch(ctx context.Context, childID uuid.UUID, day string) error {
	row := ChildSearch{ChildID: childID, Day: day, Count: 1, UpdatedAt: a.now()}

	err := a.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "child_id"}, {Name: "day"}},
		DoUpdates: clause.Assignments(map[string]any{
			"count":      gorm.Expr("child_search.count + 1"),
			"updated_at": row.UpdatedAt,
		}),
	}).Create(&row).Error
	return wrap(err, "recording search")
}

// PurgeSearchesBefore drops search counts for days older than the given key.
func (a *Activity) PurgeSearchesBefore(ctx context.Context, day string) (int64, error) {
	result := a.db.WithContext(ctx).Where("day < ?", day).Delete(&ChildSearch{})
	if result.Error != nil {
		return 0, fmt.Errorf("purging search counts: %w", result.Error)
	}
	return result.RowsAffected, nil
}
