package api

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDatabaseReadinessRetriesTransientFailure(t *testing.T) {
	calls := 0
	err := databaseReady(context.Background(), func(context.Context) error {
		calls++
		if calls == 1 {
			return errors.New("connection reset")
		}
		return nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("error=%v attempts=%d", err, calls)
	}
}

func TestDatabaseReadinessReportsPersistentFailure(t *testing.T) {
	calls := 0
	failure := errors.New("database unavailable")
	err := databaseReady(context.Background(), func(context.Context) error {
		calls++
		return failure
	})
	if !errors.Is(err, failure) || calls != 3 {
		t.Fatalf("error=%v attempts=%d", err, calls)
	}
}

func TestDatabaseReadinessHonorsDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	calls := 0
	err := databaseReady(ctx, func(ctx context.Context) error {
		calls++
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
		t.Fatalf("error=%v attempts=%d", err, calls)
	}
}
