// Package cleanup removes expired operational data on a daily schedule.
package cleanup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nerdswhofish/coop/internal/youtube"
)

// Interval is the cadence for routine database cleanup.
const Interval = 24 * time.Hour

type accounts interface {
	PurgeExpiredSessions(context.Context) (int64, error)
	PurgeExpiredPairingCodes(context.Context) (int64, error)
	PurgeExpiredWebDeviceLinks(context.Context) (int64, error)
	PurgeParentInvitations(context.Context) (int64, error)
}

type cache interface {
	PurgeExpired(context.Context) (int64, error)
}

type quota interface {
	PurgeBefore(context.Context, string) (int64, error)
}

type activity interface {
	PurgeSearchesBefore(context.Context, string) (int64, error)
}

// Stats reports how many rows each cleanup pass removed.
type Stats struct {
	Cache        int64
	Sessions     int64
	PairingCodes int64
	WebLinks     int64
	Invitations  int64
	Quota        int64
	Searches     int64
}

// Service runs the independent purge operations without letting one failure
// prevent the others from reclaiming their rows.
type Service struct {
	accounts accounts
	cache    cache
	quota    quota
	activity activity
	logger   *slog.Logger
	now      func() time.Time
}

// New validates the cleanup repositories.
func New(accounts accounts, cache cache, quota quota, activity activity,
	logger *slog.Logger, now func() time.Time) (*Service, error) {

	if accounts == nil {
		return nil, errors.New("cleanup: accounts repository is required")
	}
	if cache == nil {
		return nil, errors.New("cleanup: cache repository is required")
	}
	if quota == nil {
		return nil, errors.New("cleanup: quota repository is required")
	}
	if activity == nil {
		return nil, errors.New("cleanup: activity repository is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if now == nil {
		now = time.Now
	}

	return &Service{
		accounts: accounts,
		cache:    cache,
		quota:    quota,
		activity: activity,
		logger:   logger,
		now:      now,
	}, nil
}

// Run cleans immediately, then repeats daily until shutdown.
func (s *Service) Run(ctx context.Context) {
	s.cleanAndLog(ctx)

	ticker := time.NewTicker(Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanAndLog(ctx)
		}
	}
}

func (s *Service) cleanAndLog(ctx context.Context) {
	stats, err := s.Clean(ctx)
	if errors.Is(err, context.Canceled) {
		return
	}
	s.logger.Info("database cleanup complete",
		"cache", stats.Cache,
		"sessions", stats.Sessions,
		"pairing_codes", stats.PairingCodes,
		"web_links", stats.WebLinks,
		"invitations", stats.Invitations,
		"quota", stats.Quota,
		"searches", stats.Searches,
	)
	if err != nil {
		s.logger.Error("database cleanup partially failed", "error", err)
	}
}

// Clean performs one independent pass over each bounded operational table.
func (s *Service) Clean(ctx context.Context) (Stats, error) {
	var stats Stats
	var errs []error

	run := func(name string, count *int64, purge func() (int64, error)) {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			return
		}
		removed, err := purge()
		if err != nil {
			errs = append(errs, fmt.Errorf("purging %s: %w", name, err))
			return
		}
		*count = removed
	}

	run("api cache", &stats.Cache, func() (int64, error) {
		return s.cache.PurgeExpired(ctx)
	})
	run("parent sessions", &stats.Sessions, func() (int64, error) {
		return s.accounts.PurgeExpiredSessions(ctx)
	})
	run("pairing codes", &stats.PairingCodes, func() (int64, error) {
		return s.accounts.PurgeExpiredPairingCodes(ctx)
	})
	run("web device links", &stats.WebLinks, func() (int64, error) {
		return s.accounts.PurgeExpiredWebDeviceLinks(ctx)
	})
	run("parent invitations", &stats.Invitations, func() (int64, error) {
		return s.accounts.PurgeParentInvitations(ctx)
	})

	day := youtube.QuotaDay(s.now())
	run("quota ledger", &stats.Quota, func() (int64, error) {
		return s.quota.PurgeBefore(ctx, day)
	})
	run("child search ledger", &stats.Searches, func() (int64, error) {
		return s.activity.PurgeSearchesBefore(ctx, day)
	})

	return stats, errors.Join(errs...)
}
