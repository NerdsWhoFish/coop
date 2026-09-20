package store

import (
	"context"
	"log/slog"
	"time"

	"gorm.io/gorm/logger"

	"github.com/nerdswhofish/coop/internal/cancellation"
)

type databaseLogger struct {
	logger.Interface
	log   *slog.Logger
	level logger.LogLevel
}

func newDatabaseLogger(log *slog.Logger) logger.Interface {
	return databaseLogger{Interface: logger.Default.LogMode(logger.Warn), log: log, level: logger.Warn}
}

func (l databaseLogger) LogMode(level logger.LogLevel) logger.Interface {
	l.Interface = l.Interface.LogMode(level)
	l.level = level
	return l
}

func (l databaseLogger) Trace(ctx context.Context, begin time.Time, query func() (string, int64), err error) {
	if cancellation.Is(ctx, err) {
		if l.level > logger.Silent {
			l.log.InfoContext(ctx, "database query canceled", "duration", time.Since(begin))
		}
		return
	}
	l.Interface.Trace(ctx, begin, query, err)
}
