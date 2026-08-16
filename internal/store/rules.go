package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nerdswhofish/coop/internal/policy"
)

// Rules holds the allowlists, block list, keywords and per-video overrides
// that decide what a child may see.
type Rules struct {
	db  *DB
	now func() time.Time
}

// NewRules builds the repository. A nil now defaults to time.Now.
func NewRules(db *DB, now func() time.Time) *Rules {
	if now == nil {
		now = time.Now
	}
	return &Rules{db: db, now: now}
}

// Evaluator loads a child's complete rule set, ready to judge videos. One call
// rather than four, so a caller cannot build an evaluator missing the block
// list, which would fail open.
func (r *Rules) Evaluator(ctx context.Context, familyID, childID uuid.UUID) (*policy.Evaluator, error) {
	channels, err := r.channelRules(ctx, familyID, childID)
	if err != nil {
		return nil, err
	}
	keywords, err := r.Keywords(ctx, familyID, childID)
	if err != nil {
		return nil, err
	}
	overrides, err := r.overrideVideoIDs(ctx, familyID, childID)
	if err != nil {
		return nil, err
	}
	return policy.NewEvaluator(channels, keywords, overrides), nil
}

func (r *Rules) channelRules(ctx context.Context, familyID, childID uuid.UUID) (policy.ChannelRules, error) {
	var rules policy.ChannelRules

	load := func(dest *policy.ChannelSet, model any, where string, args ...any) error {
		var ids []string
		if err := r.db.WithContext(ctx).Model(model).
			Where(where, args...).Pluck("channel_id", &ids).Error; err != nil {
			return fmt.Errorf("loading channel rules: %w", err)
		}
		*dest = policy.NewChannelSet(ids...)
		return nil
	}

	if err := load(&rules.Blocked, &BlockChannel{}, "family_id = ?", familyID); err != nil {
		return rules, err
	}
	if err := load(&rules.AllowGlobal, &AllowGlobal{}, "family_id = ?", familyID); err != nil {
		return rules, err
	}
	if err := load(&rules.AllowChild, &AllowChild{}, "child_id = ?", childID); err != nil {
		return rules, err
	}
	if err := load(&rules.DenyChild, &DenyChild{}, "child_id = ?", childID); err != nil {
		return rules, err
	}
	return rules, nil
}

// Keywords loads the family keywords plus this child's, which are additive.
func (r *Rules) Keywords(ctx context.Context, familyID, childID uuid.UUID) ([]policy.Keyword, error) {
	var rows []Keyword
	err := r.db.WithContext(ctx).
		Where("family_id = ? AND (child_id IS NULL OR child_id = ?)", familyID, childID).
		Order("created_at").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("loading keywords: %w", err)
	}

	out := make([]policy.Keyword, len(rows))
	for i, row := range rows {
		scope := policy.ScopeFamily
		if row.ChildID != nil {
			scope = policy.ScopeChild
		}
		out[i] = policy.Keyword{
			ID:               row.ID.String(),
			Term:             row.Term,
			Scope:            scope,
			MatchTitle:       row.MatchTitle,
			MatchTags:        row.MatchTags,
			MatchDescription: row.MatchDescription,
			WholeWord:        row.WholeWord,
		}
	}
	return out, nil
}

func (r *Rules) overrideVideoIDs(ctx context.Context, familyID, childID uuid.UUID) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&VideoOverride{}).
		Where("family_id = ? AND (child_id IS NULL OR child_id = ?)", familyID, childID).
		Pluck("video_id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("loading video overrides: %w", err)
	}
	return ids, nil
}

// AllowGlobally approves a channel for every child in a family.
func (r *Rules) AllowGlobally(ctx context.Context, familyID uuid.UUID, channelID string,
	approvedBy uuid.UUID) error {

	return r.policyMutation(ctx, familyID, nil, approvedBy, "channel.allow_global",
		"channel", channelID, map[string]any{}, map[string]any{"allowed": true},
		func(tx *gorm.DB) (bool, error) {
			row := AllowGlobal{FamilyID: familyID, ChannelID: channelID, ApprovedBy: approvedBy}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
			return result.RowsAffected > 0, wrap(result.Error, "approving channel globally")
		})
}

// DisallowGlobally removes a channel from the family allowlist.
func (r *Rules) DisallowGlobally(ctx context.Context, familyID uuid.UUID, channelID string,
	actorID uuid.UUID) error {
	return r.policyMutation(ctx, familyID, nil, actorID, "channel.disallow_global",
		"channel", channelID, map[string]any{"allowed": true}, map[string]any{},
		func(tx *gorm.DB) (bool, error) {
			result := tx.Delete(&AllowGlobal{}, "family_id = ? AND channel_id = ?", familyID, channelID)
			return result.RowsAffected > 0, wrap(result.Error, "removing global approval")
		})
}

// AllowForChild approves a channel for one child.
func (r *Rules) AllowForChild(ctx context.Context, childID uuid.UUID, channelID string,
	approvedBy uuid.UUID) error {

	return r.childPolicyMutation(ctx, childID, approvedBy, "channel.allow_child",
		"channel", channelID, map[string]any{}, map[string]any{"allowed": true},
		func(tx *gorm.DB) (bool, error) {
			row := AllowChild{ChildID: childID, ChannelID: channelID, ApprovedBy: approvedBy}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
			return result.RowsAffected > 0, wrap(result.Error, "approving channel for child")
		})
}

// DisallowForChild removes a channel from one child's allowlist.
func (r *Rules) DisallowForChild(ctx context.Context, childID uuid.UUID, channelID string,
	actorID uuid.UUID) error {
	return r.childPolicyMutation(ctx, childID, actorID, "channel.disallow_child",
		"channel", channelID, map[string]any{"allowed": true}, map[string]any{},
		func(tx *gorm.DB) (bool, error) {
			result := tx.Delete(&AllowChild{}, "child_id = ? AND channel_id = ?", childID, channelID)
			return result.RowsAffected > 0, wrap(result.Error, "removing child approval")
		})
}

// DenyForChild subtracts a globally approved channel from one child.
func (r *Rules) DenyForChild(ctx context.Context, childID uuid.UUID, channelID string,
	actorID uuid.UUID) error {
	return r.childPolicyMutation(ctx, childID, actorID, "channel.deny_child",
		"channel", channelID, map[string]any{}, map[string]any{"denied": true},
		func(tx *gorm.DB) (bool, error) {
			row := DenyChild{ChildID: childID, ChannelID: channelID}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
			return result.RowsAffected > 0, wrap(result.Error, "denying channel for child")
		})
}

// UndenyForChild removes a per-child denial.
func (r *Rules) UndenyForChild(ctx context.Context, childID uuid.UUID, channelID string,
	actorID uuid.UUID) error {
	return r.childPolicyMutation(ctx, childID, actorID, "channel.undeny_child",
		"channel", channelID, map[string]any{"denied": true}, map[string]any{},
		func(tx *gorm.DB) (bool, error) {
			result := tx.Delete(&DenyChild{}, "child_id = ? AND channel_id = ?", childID, channelID)
			return result.RowsAffected > 0, wrap(result.Error, "removing child denial")
		})
}

// BlockChannelForFamily hides a channel and clears any approval it carried.
// The clearing matters on unblock: policy already ranks Blocked above allows,
// but stale rows would re-approve the channel the moment a block was lifted.
func (r *Rules) BlockChannelForFamily(ctx context.Context, familyID uuid.UUID,
	channelID, reason string, actorID uuid.UUID) error {

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		beforeState := map[string]any{}
		var existing BlockChannel
		err := tx.First(&existing, "family_id = ? AND channel_id = ?", familyID, channelID).Error
		if err == nil {
			beforeState = map[string]any{"blocked": true, "reason": existing.Reason}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("reading channel block: %w", err)
		}
		block := BlockChannel{FamilyID: familyID, ChannelID: channelID, Reason: reason}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "family_id"}, {Name: "channel_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"reason"}),
		}).Create(&block).Error; err != nil {
			return fmt.Errorf("blocking channel: %w", err)
		}

		if err := tx.Delete(&AllowGlobal{},
			"family_id = ? AND channel_id = ?", familyID, channelID).Error; err != nil {
			return fmt.Errorf("clearing global approval: %w", err)
		}

		// Per-child allows are keyed by child, so they are cleared through the
		// family's children rather than directly.
		err = tx.Exec(`DELETE FROM allow_child
		                WHERE channel_id = ?
		                  AND child_id IN (SELECT id FROM child WHERE family_id = ?)`,
			channelID, familyID).Error
		if err != nil {
			return fmt.Errorf("clearing child approvals: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: familyID, ActorParentID: &actorID,
			Action: "channel.block", TargetType: "channel", TargetID: channelID,
			Before: beforeState, After: map[string]any{"blocked": true, "reason": reason},
			CreatedAt: r.now(),
		})
	})
}

// UnblockChannel removes a family-wide block.
func (r *Rules) UnblockChannel(ctx context.Context, familyID uuid.UUID, channelID string,
	actorID uuid.UUID) error {
	return r.policyMutation(ctx, familyID, nil, actorID, "channel.unblock",
		"channel", channelID, map[string]any{"blocked": true}, map[string]any{},
		func(tx *gorm.DB) (bool, error) {
			result := tx.Delete(&BlockChannel{}, "family_id = ? AND channel_id = ?", familyID, channelID)
			return result.RowsAffected > 0, wrap(result.Error, "unblocking channel")
		})
}

// BlockedChannels lists a family's block list.
func (r *Rules) BlockedChannels(ctx context.Context, familyID uuid.UUID) ([]BlockChannel, error) {
	var rows []BlockChannel
	err := r.db.WithContext(ctx).
		Where("family_id = ?", familyID).
		Order("created_at DESC").
		Find(&rows).Error
	return rows, wrap(err, "listing blocked channels")
}

// GlobalAllowlist lists channels approved for the whole family.
func (r *Rules) GlobalAllowlist(ctx context.Context, familyID uuid.UUID) ([]AllowGlobal, error) {
	var rows []AllowGlobal
	err := r.db.WithContext(ctx).
		Where("family_id = ?", familyID).
		Order("created_at DESC").
		Find(&rows).Error
	return rows, wrap(err, "listing global allowlist")
}

// ChildAllowlist lists channels approved for one child alone.
func (r *Rules) ChildAllowlist(ctx context.Context, childID uuid.UUID) ([]AllowChild, error) {
	var rows []AllowChild
	err := r.db.WithContext(ctx).
		Where("child_id = ?", childID).
		Order("created_at DESC").
		Find(&rows).Error
	return rows, wrap(err, "listing child allowlist")
}

// CreateKeyword adds a negative keyword.
func (r *Rules) CreateKeyword(ctx context.Context, row Keyword, actorID uuid.UUID) (Keyword, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("creating keyword: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: row.FamilyID, ActorParentID: &actorID, ChildID: row.ChildID,
			Action: "keyword.create", TargetType: "keyword", TargetID: row.ID.String(),
			Before: map[string]any{}, After: row, CreatedAt: r.now(),
		})
	})
	return row, err
}

// UpdateKeyword replaces a keyword's matching settings.
func (r *Rules) UpdateKeyword(ctx context.Context, familyID, keywordID uuid.UUID,
	updates map[string]any, actorID uuid.UUID) error {

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before Keyword
		if err := tx.First(&before, "id = ? AND family_id = ?", keywordID, familyID).Error; err != nil {
			return wrap(err, "reading keyword before update")
		}
		updates["updated_at"] = r.now()
		result := tx.Model(&Keyword{}).
			Where("id = ? AND family_id = ?", keywordID, familyID).
			Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("updating keyword: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		var after Keyword
		if err := tx.First(&after, "id = ?", keywordID).Error; err != nil {
			return fmt.Errorf("reading keyword after update: %w", err)
		}
		return appendAudit(tx, auditChange{
			FamilyID: familyID, ActorParentID: &actorID, ChildID: before.ChildID,
			Action: "keyword.update", TargetType: "keyword", TargetID: keywordID.String(),
			Before: before, After: after, CreatedAt: r.now(),
		})
	})
}

// DeleteKeyword removes a keyword and, by cascade, its suppression log.
func (r *Rules) DeleteKeyword(ctx context.Context, familyID, keywordID, actorID uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before Keyword
		if err := tx.First(&before, "id = ? AND family_id = ?", keywordID, familyID).Error; err != nil {
			return wrap(err, "reading keyword before delete")
		}
		result := tx.Delete(&Keyword{}, "id = ? AND family_id = ?", keywordID, familyID)
		if result.Error != nil {
			return fmt.Errorf("deleting keyword: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return appendAudit(tx, auditChange{
			FamilyID: familyID, ActorParentID: &actorID, ChildID: before.ChildID,
			Action: "keyword.delete", TargetType: "keyword", TargetID: keywordID.String(),
			Before: before, After: map[string]any{}, CreatedAt: r.now(),
		})
	})
}

// ListKeywords returns raw keyword rows for the parent app.
func (r *Rules) ListKeywords(ctx context.Context, familyID uuid.UUID, childID *uuid.UUID) ([]Keyword, error) {
	query := r.db.WithContext(ctx).Where("family_id = ?", familyID)
	if childID == nil {
		query = query.Where("child_id IS NULL")
	} else {
		query = query.Where("child_id = ?", *childID)
	}

	var rows []Keyword
	err := query.Order("created_at").Find(&rows).Error
	return rows, wrap(err, "listing keywords")
}

// CreateOverride re-allows a video a keyword suppressed.
func (r *Rules) CreateOverride(ctx context.Context, row VideoOverride) error {
	return r.policyMutation(ctx, row.FamilyID, row.ChildID, row.CreatedBy,
		"video.override", "video", row.VideoID, map[string]any{}, map[string]any{"allowed": true},
		func(tx *gorm.DB) (bool, error) {
			result := tx.Create(&row)
			return result.RowsAffected > 0, wrap(result.Error, "creating video override")
		})
}

type policyMutationFunc func(*gorm.DB) (bool, error)

func (r *Rules) childPolicyMutation(ctx context.Context, childID, actorID uuid.UUID,
	action, targetType, targetID string, before, after any, mutate policyMutationFunc) error {

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		familyID, err := familyIDForChild(tx, childID)
		if err != nil {
			return err
		}
		return r.policyMutationTx(tx, familyID, &childID, actorID,
			action, targetType, targetID, before, after, mutate)
	})
}

func (r *Rules) policyMutation(ctx context.Context, familyID uuid.UUID, childID *uuid.UUID,
	actorID uuid.UUID, action, targetType, targetID string, before, after any,
	mutate policyMutationFunc) error {

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return r.policyMutationTx(tx, familyID, childID, actorID,
			action, targetType, targetID, before, after, mutate)
	})
}

func (r *Rules) policyMutationTx(tx *gorm.DB, familyID uuid.UUID, childID *uuid.UUID,
	actorID uuid.UUID, action, targetType, targetID string, before, after any,
	mutate policyMutationFunc) error {

	changed, err := mutate(tx)
	if err != nil || !changed {
		return err
	}
	return appendAudit(tx, auditChange{
		FamilyID: familyID, ActorParentID: &actorID, ChildID: childID,
		Action: action, TargetType: targetType, TargetID: targetID,
		Before: before, After: after, CreatedAt: r.now(),
	})
}
