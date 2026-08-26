// Package ingest keeps the local catalog populated from approved channels.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/domain"
	"github.com/nerdswhofish/coop/internal/store"
	"github.com/nerdswhofish/coop/internal/youtube"
)

const maxChannelsPerRefresh = 500

const (
	minChannelRetryDelay = 5 * time.Minute
	maxChannelRetryDelay = time.Hour
)

type channelRetryKey struct {
	familyID  uuid.UUID
	channelID string
}

type channelRetry struct {
	failures uint
	next     time.Time
}

type familySource interface {
	FamiliesWithAPIKeys(context.Context) ([]store.Family, error)
}

type catalog interface {
	StaleApprovedChannelIDs(context.Context, uuid.UUID, time.Time, int) ([]string, error)
	UpsertChannels(context.Context, []youtube.Channel) error
	UpsertVideos(context.Context, []youtube.Video) error
	ApplyFeedClassification(context.Context, []youtube.FeedEntry) error
	MarkChannelRefreshed(context.Context, string) error
}

// Client is the YouTube surface needed by the ingest loop.
type Client interface {
	Channels(context.Context, []string, domain.QuotaPurpose) ([]youtube.Channel, error)
	UploadIDs(context.Context, string, int, domain.QuotaPurpose) ([]string, error)
	Videos(context.Context, []string, domain.QuotaPurpose) ([]youtube.Video, error)
	ChannelFeed(context.Context, string) ([]youtube.FeedEntry, error)
}

// ClientForFamily resolves the current key immediately before a refresh.
type ClientForFamily func(context.Context, uuid.UUID) (Client, error)

// Service polls approved channels and writes their recent uploads locally.
type Service struct {
	families        familySource
	catalog         catalog
	client          ClientForFamily
	pollInterval    time.Duration
	refreshInterval time.Duration
	logger          *slog.Logger
	now             func() time.Time
	retries         map[channelRetryKey]channelRetry
	retryDelay      func(uint) time.Duration
}

// New builds an ingest service.
func New(families familySource, catalog catalog, client ClientForFamily,
	pollInterval, refreshInterval time.Duration, logger *slog.Logger,
	now func() time.Time) (*Service, error) {

	if families == nil {
		return nil, errors.New("ingest: family source is required")
	}
	if catalog == nil {
		return nil, errors.New("ingest: catalog is required")
	}
	if client == nil {
		return nil, errors.New("ingest: client factory is required")
	}
	if pollInterval <= 0 {
		return nil, errors.New("ingest: poll interval must be positive")
	}
	if refreshInterval <= 0 {
		return nil, errors.New("ingest: refresh interval must be positive")
	}
	if pollInterval > refreshInterval {
		return nil, errors.New("ingest: poll interval must not exceed refresh interval")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if now == nil {
		now = time.Now
	}

	return &Service{
		families:        families,
		catalog:         catalog,
		client:          client,
		pollInterval:    pollInterval,
		refreshInterval: refreshInterval,
		logger:          logger,
		now:             now,
		retries:         make(map[channelRetryKey]channelRetry),
		retryDelay: func(failures uint) time.Duration {
			return channelRetryDelay(failures, refreshInterval)
		},
	}, nil
}

// Run refreshes immediately, then repeats until the server context ends.
func (s *Service) Run(ctx context.Context) {
	s.refreshAndLog(ctx)

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshAndLog(ctx)
		}
	}
}

func (s *Service) refreshAndLog(ctx context.Context) {
	if err := s.Refresh(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Error("catalog refresh failed", "error", err)
	}
}

// Refresh performs one pass across every configured family.
func (s *Service) Refresh(ctx context.Context) error {
	families, err := s.families.FamiliesWithAPIKeys(ctx)
	if err != nil {
		return err
	}

	var errs []error
	for _, family := range families {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.refreshFamily(ctx, family.ID); err != nil {
			errs = append(errs, fmt.Errorf("refreshing family %s: %w", family.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) refreshFamily(ctx context.Context, familyID uuid.UUID) error {
	ids, err := s.catalog.StaleApprovedChannelIDs(ctx, familyID,
		s.now().Add(-s.refreshInterval), maxChannelsPerRefresh)
	if err != nil || len(ids) == 0 {
		return err
	}
	ids = s.channelsReadyForRetry(familyID, ids)
	if len(ids) == 0 {
		return nil
	}

	client, err := s.client(ctx, familyID)
	if err != nil {
		return err
	}

	var errs []error
	for start := 0; start < len(ids); start += youtube.MaxIDsPerCall {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(start+youtube.MaxIDsPerCall, len(ids))
		channels, err := client.Channels(ctx, ids[start:end], domain.PurposeFeed)
		if errors.Is(err, youtube.ErrBudgetExhausted) {
			s.logger.Info("catalog refresh paused at quota limit", "family_id", familyID)
			return errors.Join(errs...)
		}
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if youtube.IsRetryable(err) {
				retryIn := s.deferChannels(familyID, ids[start:end])
				err = fmt.Errorf("%w; retrying in %s", err, retryIn.Round(time.Second))
			}
			errs = append(errs, fmt.Errorf("refreshing channel metadata: %w", err))
			continue
		}
		if err := s.catalog.UpsertChannels(ctx, channels); err != nil {
			return err
		}

		for _, channel := range channels {
			if err := ctx.Err(); err != nil {
				return err
			}
			err := s.refreshChannel(ctx, client, channel.ID)
			if errors.Is(err, youtube.ErrBudgetExhausted) {
				s.logger.Info("catalog refresh paused at quota limit", "family_id", familyID)
				return errors.Join(errs...)
			}
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				if youtube.IsRetryable(err) {
					retryIn := s.deferChannel(familyID, channel.ID)
					err = fmt.Errorf("%w; retrying in %s", err, retryIn.Round(time.Second))
				}
				errs = append(errs, fmt.Errorf("refreshing channel %s: %w", channel.ID, err))
				continue
			}
			s.clearChannelRetry(familyID, channel.ID)
			if err := s.catalog.MarkChannelRefreshed(ctx, channel.ID); err != nil {
				errs = append(errs, fmt.Errorf("marking channel %s refreshed: %w", channel.ID, err))
			}
		}
	}

	return errors.Join(errs...)
}

func (s *Service) channelsReadyForRetry(familyID uuid.UUID, ids []string) []string {
	now := s.now()
	ready := make([]string, 0, len(ids))
	for _, channelID := range ids {
		retry, found := s.retries[channelRetryKey{familyID: familyID, channelID: channelID}]
		if !found || !now.Before(retry.next) {
			ready = append(ready, channelID)
		}
	}
	return ready
}

func (s *Service) deferChannels(familyID uuid.UUID, ids []string) time.Duration {
	var earliest time.Duration
	for _, channelID := range ids {
		delay := s.deferChannel(familyID, channelID)
		if earliest == 0 || delay < earliest {
			earliest = delay
		}
	}
	return earliest
}

func (s *Service) deferChannel(familyID uuid.UUID, channelID string) time.Duration {
	key := channelRetryKey{familyID: familyID, channelID: channelID}
	retry := s.retries[key]
	retry.failures++
	delay := s.retryDelay(retry.failures)
	retry.next = s.now().Add(delay)
	s.retries[key] = retry
	return delay
}

func (s *Service) clearChannelRetry(familyID uuid.UUID, channelID string) {
	delete(s.retries, channelRetryKey{familyID: familyID, channelID: channelID})
}

func channelRetryDelay(failures uint, refreshInterval time.Duration) time.Duration {
	ceiling := min(maxChannelRetryDelay, refreshInterval)
	delay := min(minChannelRetryDelay, ceiling)
	for attempt := uint(1); attempt < failures && delay < ceiling; attempt++ {
		delay = min(delay*2, ceiling)
	}

	spread := delay / 5
	if spread == 0 {
		return delay
	}
	return delay - spread + time.Duration(rand.Int64N(int64(spread)+1))
}

func (s *Service) refreshChannel(ctx context.Context, client Client, channelID string) error {
	ids, err := client.UploadIDs(ctx, channelID, youtube.MaxIDsPerCall, domain.PurposeFeed)
	if err != nil || len(ids) == 0 {
		return err
	}

	videos, err := client.Videos(ctx, ids, domain.PurposeFeed)
	if err != nil {
		return err
	}
	if err := s.catalog.UpsertVideos(ctx, videos); err != nil {
		return err
	}

	entries, err := client.ChannelFeed(ctx, channelID)
	if err != nil {
		return err
	}
	return s.catalog.ApplyFeedClassification(ctx, entries)
}
