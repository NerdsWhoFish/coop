package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrAuthChallengeInvalid = errors.New("store: authentication challenge is invalid")
	ErrTOTPReplay           = errors.New("store: TOTP step was already used")
)

const (
	AuthPurposeLogin  = "login"
	AuthPurposeEnroll = "enroll"
)

// CreateAuthChallenge replaces any outstanding challenge of the same purpose
// for a parent, so interrupted enrollment can restart without leaving several
// valid TOTP secrets in flight.
func (a *Accounts) CreateAuthChallenge(ctx context.Context, challenge ParentAuthChallenge) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		if err := tx.Model(&ParentAuthChallenge{}).
			Where("parent_id = ? AND purpose = ? AND used_at IS NULL", challenge.ParentID, challenge.Purpose).
			Update("used_at", now).Error; err != nil {
			return fmt.Errorf("expiring authentication challenges: %w", err)
		}
		if err := tx.Create(&challenge).Error; err != nil {
			return fmt.Errorf("creating authentication challenge: %w", err)
		}
		return nil
	})
}

// AuthChallengeByToken reads one usable challenge. The caller still has to
// validate and atomically consume it through CompleteAuthChallenge.
func (a *Accounts) AuthChallengeByToken(ctx context.Context, tokenHash string,
	maxAttempts int) (ParentAuthChallenge, Parent, error) {

	var challenge ParentAuthChallenge
	err := a.db.WithContext(ctx).First(&challenge,
		"token_hash = ? AND used_at IS NULL AND expires_at > ? AND attempts < ?",
		tokenHash, a.now(), maxAttempts).Error
	if err != nil {
		return ParentAuthChallenge{}, Parent{}, ErrAuthChallengeInvalid
	}

	var parent Parent
	if err := a.db.WithContext(ctx).First(&parent, "id = ?", challenge.ParentID).Error; err != nil {
		return ParentAuthChallenge{}, Parent{}, wrap(err, "reading challenge parent")
	}
	return challenge, parent, nil
}

// FailAuthChallenge consumes one attempt without revealing whether the token,
// code, or account state was wrong.
func (a *Accounts) FailAuthChallenge(ctx context.Context, tokenHash string) error {
	return wrap(a.db.WithContext(ctx).Model(&ParentAuthChallenge{}).
		Where("token_hash = ? AND used_at IS NULL AND expires_at > ?", tokenHash, a.now()).
		UpdateColumn("attempts", gorm.Expr("attempts + 1")).Error,
		"recording authentication challenge failure")
}

// CompleteAuthChallenge installs the enrollment secret when necessary,
// rejects TOTP replay, consumes the challenge, and creates the session in one
// transaction.
func (a *Accounts) CompleteAuthChallenge(ctx context.Context, challengeID uuid.UUID,
	step int64, maxAttempts int, session ParentSession) (Parent, error) {

	var parent Parent
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var challenge ParentAuthChallenge
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&challenge,
			"id = ? AND used_at IS NULL AND expires_at > ? AND attempts < ?",
			challengeID, a.now(), maxAttempts).Error
		if err != nil {
			return ErrAuthChallengeInvalid
		}

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&parent, "id = ?", challenge.ParentID).Error; err != nil {
			return wrap(err, "reading challenge parent")
		}
		if parent.TOTPLastUsedStep != nil && step <= *parent.TOTPLastUsedStep {
			return ErrTOTPReplay
		}

		updates := map[string]any{
			"totp_last_used_step": step,
			"updated_at":          a.now(),
		}
		if challenge.Purpose == AuthPurposeEnroll {
			if len(challenge.EncryptedTOTPSecret) == 0 {
				return ErrAuthChallengeInvalid
			}
			updates["encrypted_totp_secret"] = challenge.EncryptedTOTPSecret
		}
		if err := tx.Model(&parent).Updates(updates).Error; err != nil {
			return fmt.Errorf("updating parent TOTP state: %w", err)
		}
		parent.TOTPLastUsedStep = &step
		if challenge.Purpose == AuthPurposeEnroll {
			parent.EncryptedTOTPSecret = challenge.EncryptedTOTPSecret
			if err := appendAudit(tx, auditChange{
				FamilyID: parent.FamilyID, ActorParentID: &parent.ID,
				Action: "auth.totp_enroll", TargetType: "parent", TargetID: parent.ID.String(),
				Before: map[string]any{"enrolled": false}, After: map[string]any{"enrolled": true},
				CreatedAt: a.now(),
			}); err != nil {
				return err
			}
		}

		session.ParentID = parent.ID
		if err := tx.Create(&session).Error; err != nil {
			return fmt.Errorf("creating parent session: %w", err)
		}
		usedAt := a.now()
		if err := tx.Model(&challenge).Update("used_at", usedAt).Error; err != nil {
			return fmt.Errorf("consuming authentication challenge: %w", err)
		}
		return nil
	})
	return parent, err
}

// AuthLocked reports whether any supplied bucket is currently locked.
func (a *Accounts) AuthLocked(ctx context.Context, action string, keyHashes []string) (time.Time, bool, error) {
	if len(keyHashes) == 0 {
		return time.Time{}, false, nil
	}
	var throttle AuthThrottle
	err := a.db.WithContext(ctx).
		Where("action = ? AND key_hash IN ? AND locked_until > ?", action, keyHashes, a.now()).
		Order("locked_until DESC").First(&throttle).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("checking authentication throttle: %w", err)
	}
	return *throttle.LockedUntil, true, nil
}

// RecordAuthFailure updates every relevant bucket under row locks so failures
// cannot be lost when requests arrive together.
func (a *Accounts) RecordAuthFailure(ctx context.Context, action string, keyHashes []string,
	maxFailures int, window, lockout time.Duration) error {

	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, keyHash := range uniqueStrings(keyHashes) {
			now := a.now()
			seed := AuthThrottle{
				KeyHash: keyHash, Action: action, WindowStartedAt: now, UpdatedAt: now,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
				return fmt.Errorf("creating authentication throttle: %w", err)
			}

			var throttle AuthThrottle
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				First(&throttle, "key_hash = ? AND action = ?", keyHash, action).Error; err != nil {
				return fmt.Errorf("locking authentication throttle: %w", err)
			}
			if now.Sub(throttle.WindowStartedAt) >= window {
				throttle.Failures = 0
				throttle.WindowStartedAt = now
			}
			throttle.Failures++
			throttle.UpdatedAt = now
			if throttle.Failures >= maxFailures {
				lockedUntil := now.Add(lockout)
				throttle.LockedUntil = &lockedUntil
			}
			if err := tx.Save(&throttle).Error; err != nil {
				return fmt.Errorf("updating authentication throttle: %w", err)
			}
		}
		return nil
	})
}

func (a *Accounts) ClearAuthThrottle(ctx context.Context, action string, keyHashes []string) error {
	if len(keyHashes) == 0 {
		return nil
	}
	return wrap(a.db.WithContext(ctx).
		Delete(&AuthThrottle{}, "action = ? AND key_hash IN ?", action, uniqueStrings(keyHashes)).Error,
		"clearing authentication throttle")
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
