package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"github.com/nerdswhofish/coop/internal/domain"
)

// SaveParentPushToken registers or refreshes one parent app installation.
// Re-registering an existing token reassigns it, because a signed-out device
// can sign back in as a different parent.
func (a *Accounts) SaveParentPushToken(ctx context.Context, familyID, parentID uuid.UUID,
	token string) error {

	row := PushToken{
		Token:    token,
		FamilyID: familyID,
		Audience: domain.PushAudienceParent,
		ParentID: &parentID,
	}
	err := a.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "token"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"family_id", "audience", "parent_id", "child_id", "device_id", "updated_at",
		}),
	}).Create(&row).Error
	return wrap(err, "saving parent push token")
}

// SaveChildPushToken registers or refreshes one child device installation.
func (a *Accounts) SaveChildPushToken(ctx context.Context, familyID, childID, deviceID uuid.UUID,
	token string) error {

	row := PushToken{
		Token:    token,
		FamilyID: familyID,
		Audience: domain.PushAudienceChild,
		ChildID:  &childID,
		DeviceID: &deviceID,
	}
	err := a.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "token"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"family_id", "audience", "parent_id", "child_id", "device_id", "updated_at",
		}),
	}).Create(&row).Error
	return wrap(err, "saving child push token")
}

// DeleteParentPushToken removes one registration at sign-out. Scoping to the
// parent keeps one account from unregistering another's device.
func (a *Accounts) DeleteParentPushToken(ctx context.Context, parentID uuid.UUID,
	token string) error {

	return wrap(a.db.WithContext(ctx).
		Where("token = ? AND parent_id = ?", token, parentID).
		Delete(&PushToken{}).Error, "deleting parent push token")
}

// ChildPushTokens lists the registrations for every device paired to a child.
func (a *Accounts) ChildPushTokens(ctx context.Context, childID uuid.UUID) ([]string, error) {
	var tokens []string
	err := a.db.WithContext(ctx).Model(&PushToken{}).
		Where("audience = ? AND child_id = ?", domain.PushAudienceChild, childID).
		Pluck("token", &tokens).Error
	if err != nil {
		return nil, fmt.Errorf("listing child push tokens: %w", err)
	}
	return tokens, nil
}

// ParentPushTokensForChild lists registrations for every parent allowed to act
// on the child: admins, plus scoped parents granted that child.
func (a *Accounts) ParentPushTokensForChild(ctx context.Context, familyID, childID uuid.UUID) (
	[]string, error) {

	var tokens []string
	err := a.db.WithContext(ctx).Model(&PushToken{}).
		Joins("JOIN parent ON parent.id = push_token.parent_id").
		Where("push_token.family_id = ? AND push_token.audience = ?",
			familyID, domain.PushAudienceParent).
		Where("parent.role = ? OR EXISTS (SELECT 1 FROM parent_scope"+
			" WHERE parent_scope.parent_id = parent.id AND parent_scope.child_id = ?)",
			domain.RoleAdmin, childID).
		Pluck("push_token.token", &tokens).Error
	if err != nil {
		return nil, fmt.Errorf("listing parent push tokens: %w", err)
	}
	return tokens, nil
}

// PrunePushTokens drops registrations Apple reported as gone.
func (a *Accounts) PrunePushTokens(ctx context.Context, tokens []string) error {
	if len(tokens) == 0 {
		return nil
	}
	return wrap(a.db.WithContext(ctx).
		Where("token IN ?", tokens).
		Delete(&PushToken{}).Error, "pruning push tokens")
}
