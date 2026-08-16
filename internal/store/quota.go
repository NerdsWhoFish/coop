package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nerdswhofish/coop/internal/domain"
	"github.com/nerdswhofish/coop/internal/youtube"
)

// QuotaStore is the daily API spend ledger behind the circuit breaker.
type QuotaStore struct {
	db  *DB
	now func() time.Time
}

// Compile-time proof that this satisfies the client's ledger contract.
var _ youtube.Ledger = (*QuotaStore)(nil)

// NewQuotaStore builds the ledger. A nil now defaults to time.Now.
func NewQuotaStore(db *DB, now func() time.Time) *QuotaStore {
	if now == nil {
		now = time.Now
	}
	return &QuotaStore{db: db, now: now}
}

// Record adds spend to today's row. The increment lives in the conflict clause
// so concurrent callers cannot lose each other's spend by reading the same
// total and writing it back.
func (s *QuotaStore) Record(ctx context.Context, familyID uuid.UUID, day string,
	purpose domain.QuotaPurpose, units, calls int) error {

	row := QuotaSpend{
		FamilyID:  familyID,
		Day:       day,
		Purpose:   purpose,
		Units:     units,
		Calls:     calls,
		UpdatedAt: s.now(),
	}

	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "family_id"}, {Name: "day"}, {Name: "purpose"}},
		DoUpdates: clause.Assignments(map[string]any{
			"units":      gorm.Expr("quota_spend.units + ?", units),
			"calls":      gorm.Expr("quota_spend.calls + ?", calls),
			"updated_at": row.UpdatedAt,
		}),
	}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("recording quota spend: %w", err)
	}
	return nil
}

// Usage reports today's spend per purpose. Purposes with no row are absent,
// and the caller reads those as zero.
func (s *QuotaStore) Usage(ctx context.Context, familyID uuid.UUID, day string) (
	map[domain.QuotaPurpose]youtube.Spend, error) {

	var rows []QuotaSpend
	err := s.db.WithContext(ctx).
		Where("family_id = ? AND day = ?", familyID, day).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("reading quota usage: %w", err)
	}

	out := make(map[domain.QuotaPurpose]youtube.Spend, len(rows))
	for _, row := range rows {
		out[row.Purpose] = youtube.Spend{Units: row.Units, Calls: row.Calls}
	}
	return out, nil
}

// PurgeBefore drops ledger rows for days older than the given key, so the
// table does not grow without bound.
func (s *QuotaStore) PurgeBefore(ctx context.Context, day string) (int64, error) {
	result := s.db.WithContext(ctx).Where("day < ?", day).Delete(&QuotaSpend{})
	if result.Error != nil {
		return 0, fmt.Errorf("purging quota ledger: %w", result.Error)
	}
	return result.RowsAffected, nil
}
