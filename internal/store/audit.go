package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type auditChange struct {
	FamilyID      uuid.UUID
	ActorParentID *uuid.UUID
	ChildID       *uuid.UUID
	Action        string
	TargetType    string
	TargetID      string
	Before        any
	After         any
	CreatedAt     time.Time
}

func appendAudit(tx *gorm.DB, change auditChange) error {
	before, err := json.Marshal(change.Before)
	if err != nil {
		return fmt.Errorf("encoding audit before state: %w", err)
	}
	after, err := json.Marshal(change.After)
	if err != nil {
		return fmt.Errorf("encoding audit after state: %w", err)
	}
	event := AuditEvent{
		FamilyID:      change.FamilyID,
		ActorParentID: change.ActorParentID,
		ChildID:       change.ChildID,
		Action:        change.Action,
		TargetType:    change.TargetType,
		TargetID:      change.TargetID,
		Before:        before,
		After:         after,
		CreatedAt:     change.CreatedAt,
	}
	if err := tx.Create(&event).Error; err != nil {
		return fmt.Errorf("appending audit event: %w", err)
	}
	return nil
}

func familyIDForChild(tx *gorm.DB, childID uuid.UUID) (uuid.UUID, error) {
	var child Child
	if err := tx.Select("family_id").First(&child, "id = ?", childID).Error; err != nil {
		return uuid.Nil, wrap(err, "reading audit child")
	}
	return child.FamilyID, nil
}

// Audit reads the retained policy and security history.
type Audit struct {
	db *DB
}

func NewAudit(db *DB) *Audit { return &Audit{db: db} }

type AuditQuery struct {
	FamilyID      uuid.UUID
	ChildIDs      []uuid.UUID
	IncludeGlobal bool
	Before        time.Time
	Limit         int
}

func (a *Audit) Events(ctx context.Context, query AuditQuery) ([]AuditEvent, error) {
	limit := query.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	db := a.db.WithContext(ctx).Where("family_id = ?", query.FamilyID)
	if !query.Before.IsZero() {
		db = db.Where("created_at < ?", query.Before)
	}
	if !query.IncludeGlobal {
		if len(query.ChildIDs) == 0 {
			return []AuditEvent{}, nil
		}
		db = db.Where("child_id IN ?", query.ChildIDs)
	}

	var events []AuditEvent
	err := db.Order("created_at DESC, id DESC").Limit(limit).Find(&events).Error
	return events, wrap(err, "listing audit events")
}
