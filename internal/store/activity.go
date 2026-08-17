package store

import (
	"context"
	"errors"
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

// StartPlayback opens or replaces the current playback lease for a device.
func (a *Activity) StartPlayback(ctx context.Context, deviceID, childID uuid.UUID, videoID string) error {
	now := a.now()
	row := PlaybackSession{
		DeviceID: deviceID, ChildID: childID, VideoID: videoID, StartedAt: now, UpdatedAt: now, Active: true,
	}
	err := a.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "device_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"video_id": videoID, "started_at": now, "updated_at": now, "active": true,
		}),
	}).Create(&row).Error
	return wrap(err, "starting playback lease")
}

// RenewPlayback keeps a matching active lease alive.
func (a *Activity) RenewPlayback(ctx context.Context, deviceID, childID uuid.UUID, videoID string) error {
	result := a.db.WithContext(ctx).Model(&PlaybackSession{}).
		Where("device_id = ? AND video_id = ? AND active = TRUE", deviceID, videoID).
		Update("updated_at", a.now())
	if result.Error != nil {
		return fmt.Errorf("renewing playback lease: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return a.StartPlayback(ctx, deviceID, childID, videoID)
	}
	return nil
}

// StopPlayback closes only the matching lease, so a stale page cannot stop a
// newer video that replaced it.
func (a *Activity) StopPlayback(ctx context.Context, deviceID uuid.UUID, videoID string) error {
	return wrap(a.db.WithContext(ctx).Model(&PlaybackSession{}).
		Where("device_id = ? AND video_id = ? AND active = TRUE", deviceID, videoID).
		Updates(map[string]any{"active": false, "updated_at": a.now()}).Error,
		"stopping playback lease")
}

// StopChildPlayback closes every device lease for one blocked child/video pair.
func (a *Activity) StopChildPlayback(ctx context.Context, childID uuid.UUID, videoID string) error {
	return wrap(a.db.WithContext(ctx).Model(&PlaybackSession{}).
		Where("child_id = ? AND video_id = ? AND active = TRUE", childID, videoID).
		Updates(map[string]any{"active": false, "updated_at": a.now()}).Error,
		"stopping child playback leases")
}

// ActivePlaybacks returns leases renewed after cutoff for the requested children.
func (a *Activity) ActivePlaybacks(ctx context.Context, childIDs []uuid.UUID,
	cutoff time.Time) ([]PlaybackSession, error) {
	if len(childIDs) == 0 {
		return nil, nil
	}
	var rows []PlaybackSession
	err := a.db.WithContext(ctx).
		Where("child_id IN ? AND active = TRUE AND updated_at > ?", childIDs, cutoff).
		Order("child_id, started_at").Find(&rows).Error
	return rows, wrap(err, "listing active playback leases")
}

// RankingWatch includes channel identity so a completion can improve later
// uploads from the same channel without another catalog lookup.
type RankingWatch struct {
	VideoID            string
	ChannelID          string
	StartedAt          time.Time
	CompletionFraction float64
}

// RankingWatches returns recent local viewing signals for the ranker.
func (a *Activity) RankingWatches(ctx context.Context, childID uuid.UUID,
	since time.Time) ([]RankingWatch, error) {

	var rows []RankingWatch
	err := a.db.WithContext(ctx).Model(&WatchEvent{}).
		Select("watch_event.video_id, video.channel_id, watch_event.started_at, watch_event.completion_fraction").
		Joins("JOIN video ON video.id = watch_event.video_id").
		Where("watch_event.child_id = ? AND watch_event.started_at >= ?", childID, since).
		Order("watch_event.started_at DESC").
		Scan(&rows).Error
	return rows, wrap(err, "listing ranking watches")
}

// Reactions returns all explicit local preferences for one child.
func (a *Activity) Reactions(ctx context.Context, childID uuid.UUID) ([]Reaction, error) {
	var rows []Reaction
	err := a.db.WithContext(ctx).Where("child_id = ?", childID).Find(&rows).Error
	return rows, wrap(err, "listing reactions")
}

// ChannelWeights returns non-neutral parent preferences keyed by channel.
func (a *Activity) ChannelWeights(ctx context.Context, childID uuid.UUID) (map[string]int, error) {
	var rows []ChannelWeight
	err := a.db.WithContext(ctx).Where("child_id = ?", childID).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("listing channel weights: %w", err)
	}

	weights := make(map[string]int, len(rows))
	for _, row := range rows {
		weights[row.ChannelID] = row.Weight
	}
	return weights, nil
}

// SetChannelWeight stores a soft parent preference. Neutral removes the row.
func (a *Activity) SetChannelWeight(ctx context.Context, childID uuid.UUID,
	channelID string, weight int, actorID uuid.UUID) error {

	if weight < -2 || weight > 2 {
		return fmt.Errorf("setting channel weight: weight must be between -2 and 2")
	}
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		familyID, err := familyIDForChild(tx, childID)
		if err != nil {
			return err
		}
		before := 0
		var existing ChannelWeight
		result := tx.First(&existing, "child_id = ? AND channel_id = ?", childID, channelID)
		if result.Error == nil {
			before = existing.Weight
		} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return fmt.Errorf("reading channel weight: %w", result.Error)
		}
		if before == weight {
			return nil
		}

		if weight == 0 {
			if err := tx.Delete(&ChannelWeight{},
				"child_id = ? AND channel_id = ?", childID, channelID).Error; err != nil {
				return fmt.Errorf("clearing channel weight: %w", err)
			}
		} else {
			row := ChannelWeight{ChildID: childID, ChannelID: channelID, Weight: weight, UpdatedAt: a.now()}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "child_id"}, {Name: "channel_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"weight", "updated_at"}),
			}).Create(&row).Error; err != nil {
				return fmt.Errorf("setting channel weight: %w", err)
			}
		}
		return appendAudit(tx, auditChange{
			FamilyID: familyID, ActorParentID: &actorID, ChildID: &childID,
			Action: "recommendation.channel_weight", TargetType: "channel", TargetID: channelID,
			Before: map[string]any{"weight": before}, After: map[string]any{"weight": weight},
			CreatedAt: a.now(),
		})
	})
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

	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var request Request
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&request, "id = ? AND status = ?", requestID, domain.RequestPending).Error; err != nil {
			return wrap(err, "reading pending request")
		}
		familyID, err := familyIDForChild(tx, request.ChildID)
		if err != nil {
			return err
		}
		now := a.now()
		if err := tx.Model(&request).Updates(map[string]any{
			"status": status, "decided_by": parentID, "decided_at": now,
			"decision_note": note, "updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("deciding request: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: familyID, ActorParentID: &parentID, ChildID: &request.ChildID,
			Action: "request.decide", TargetType: "request", TargetID: request.ID.String(),
			Before: map[string]any{"status": domain.RequestPending},
			After:  map[string]any{"status": status, "note": note}, CreatedAt: now,
		})
	})
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

// PendingRequestChannelIDs returns the requestable channels already waiting
// for a decision, so discovery cards remain idempotent across app launches.
func (a *Activity) PendingRequestChannelIDs(ctx context.Context,
	childID uuid.UUID) (map[string]bool, error) {

	var ids []string
	err := a.db.WithContext(ctx).Model(&Request{}).
		Where("child_id = ? AND status = ?", childID, domain.RequestPending).
		Pluck("channel_id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("listing pending request channels: %w", err)
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
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
