package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nerdswhofish/coop/internal/youtube"
)

// APICacheStore persists raw upstream responses.
type APICacheStore struct {
	db  *DB
	now func() time.Time
}

// Compile-time proof that this satisfies the client's cache contract.
var _ youtube.Cache = (*APICacheStore)(nil)

// NewAPICacheStore builds the cache. A nil now defaults to time.Now.
func NewAPICacheStore(db *DB, now func() time.Time) *APICacheStore {
	if now == nil {
		now = time.Now
	}
	return &APICacheStore{db: db, now: now}
}

// Get returns a live entry. An expired row reports a miss rather than an
// error, so the caller refetches and overwrites it.
func (s *APICacheStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	var row APICache
	err := s.db.WithContext(ctx).
		Where("key = ? AND expires_at > ?", key, s.now()).
		First(&row).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading api cache: %w", err)
	}
	return row.Response, true, nil
}

// Put stores or replaces an entry.
func (s *APICacheStore) Put(ctx context.Context, key, endpoint string,
	body []byte, ttl time.Duration) error {

	now := s.now()
	row := APICache{
		Key:       key,
		Endpoint:  endpoint,
		Response:  body,
		FetchedAt: now,
		ExpiresAt: now.Add(ttl),
	}

	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"endpoint", "response", "fetched_at", "expires_at"}),
	}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("writing api cache: %w", err)
	}
	return nil
}

// PurgeExpired deletes entries past their TTL and reports how many went.
func (s *APICacheStore) PurgeExpired(ctx context.Context) (int64, error) {
	result := s.db.WithContext(ctx).
		Where("expires_at <= ?", s.now()).
		Delete(&APICache{})
	if result.Error != nil {
		return 0, fmt.Errorf("purging api cache: %w", result.Error)
	}
	return result.RowsAffected, nil
}
