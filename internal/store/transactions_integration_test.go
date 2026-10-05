//go:build integration

package store

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/nerdswhofish/coop/internal/cancellation"
	"github.com/nerdswhofish/coop/internal/telemetry"
)

func TestDefaultTransactionPreservesInflightCancellation(t *testing.T) {
	for _, operation := range []string{"create", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			db := testDB(t)
			if err := db.Migrate(); err != nil {
				t.Fatal(err)
			}
			ctx, stop := context.WithTimeout(t.Context(), 10*time.Second)
			defer stop()
			row := Channel{ID: "private-channel-" + uuid.NewString(), Title: "private-original-title", FetchedAt: time.Now()}
			if operation != "create" {
				if err := db.WithContext(ctx).Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() {
				if err := db.Delete(&Channel{}, "id = ?", row.ID).Error; err != nil {
					t.Error(err)
				}
			})
			blocker, err := db.SQL().BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = blocker.Rollback() }()
			var blockerPID int
			if err := blocker.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
				t.Fatal(err)
			}
			if operation == "create" {
				_, err = blocker.ExecContext(ctx, "INSERT INTO channel (id, title, fetched_at) VALUES ($1, $2, $3)", row.ID, row.Title, row.FetchedAt)
			} else {
				_, err = blocker.ExecContext(ctx, "SELECT id FROM channel WHERE id = $1 FOR UPDATE", row.ID)
			}
			if err != nil {
				t.Fatal(err)
			}

			var structured, fallback bytes.Buffer
			queryLogger := databaseLogger{
				Interface: logger.New(log.New(&fallback, "", 0), logger.Config{LogLevel: logger.Warn, SlowThreshold: time.Second}),
				log:       slog.New(telemetry.LogHandler(slog.NewJSONHandler(&structured, nil))), level: logger.Warn,
			}
			span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}})
			requestCtx, cancel := context.WithCancel(trace.ContextWithSpanContext(ctx, span))
			defer cancel()
			requestDB := db.WithContext(requestCtx).Session(&gorm.Session{Logger: queryLogger})
			result := make(chan error, 1)
			go func() {
				switch operation {
				case "create":
					result <- requestDB.Create(&row).Error
				case "update":
					result <- requestDB.Model(&Channel{}).Where("id = ?", row.ID).Update("title", "private-replacement-title").Error
				case "delete":
					result <- requestDB.Delete(&Channel{}, "id = ?", row.ID).Error
				}
			}()
			waitForTransactionLock(t, ctx, db, blockerPID)
			cancel()
			select {
			case err = <-result:
			case <-ctx.Done():
				t.Fatal("canceled database operation did not finish")
			}
			if !cancellation.Is(requestCtx, err) {
				t.Errorf("transaction cleanup lost cancellation: %v", err)
			}
			if fallback.Len() != 0 {
				t.Errorf("canceled transaction reached GORM failure logger: %s", &fallback)
			}
			var record map[string]any
			if err := json.Unmarshal(structured.Bytes(), &record); err != nil {
				t.Fatalf("expected one structured cancellation record: %v; log=%s", err, &structured)
			}
			for key, want := range map[string]string{
				"level": "INFO", "msg": "database query canceled",
				"trace_id": span.TraceID().String(), "span_id": span.SpanID().String(),
			} {
				if record[key] != want {
					t.Errorf("%s=%v, want %s", key, record[key], want)
				}
			}
			if strings.Contains(structured.String(), "private-") {
				t.Error("cancellation log exposed SQL parameters")
			}
			if err := blocker.Rollback(); err != nil {
				t.Fatal(err)
			}
			var remaining Channel
			err = db.WithContext(ctx).First(&remaining, "id = ?", row.ID).Error
			if operation == "create" {
				if err != gorm.ErrRecordNotFound {
					t.Fatalf("canceled insert persisted: row=%+v err=%v", remaining, err)
				}
			} else if err != nil || remaining.Title != row.Title {
				t.Fatalf("canceled mutation changed row: row=%+v err=%v", remaining, err)
			}
		})
	}
}

func waitForTransactionLock(t *testing.T, ctx context.Context, db *DB, blockerPID int) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := db.SQL().QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE $1 = ANY(pg_blocking_pids(pid)) AND wait_event_type = 'Lock'
			)`, blockerPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("database operation never blocked on the transaction lock")
		}
	}
}
