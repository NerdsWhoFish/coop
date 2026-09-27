package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm/logger"

	"github.com/nerdswhofish/coop/internal/telemetry"
)

func TestDatabaseLoggerCancellation(t *testing.T) {
	var output, failures bytes.Buffer
	structured := slog.New(telemetry.LogHandler(slog.NewJSONHandler(&output, nil)))
	l := databaseLogger{
		Interface: logger.New(log.New(&failures, "", 0), logger.Config{LogLevel: logger.Warn, SlowThreshold: time.Millisecond}),
		log:       structured, level: logger.Warn,
	}
	span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}})
	ctx, cancel := context.WithCancel(trace.ContextWithSpanContext(t.Context(), span))
	cancel()
	query := func() (string, int64) {
		t.Fatal("canceled SQL must not be rendered")
		return "", 0
	}
	l.Trace(ctx, time.Now().Add(-time.Second), query, errors.Join(context.Canceled))
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"level": "INFO", "msg": "database query canceled",
		"trace_id": span.TraceID().String(), "span_id": span.SpanID().String(),
	} {
		if record[key] != want {
			t.Errorf("%s = %v, want %s", key, record[key], want)
		}
	}
	if failures.Len() != 0 {
		t.Fatalf("cancellation logged as failure: %s", &failures)
	}
	output.Reset()
	l.LogMode(logger.Silent).Trace(ctx, time.Now(), query, context.Canceled)
	if output.Len() != 0 || failures.Len() != 0 {
		t.Fatal("silent logger emitted cancellation")
	}
	l.LogMode(logger.Info).Trace(ctx, time.Now(), query, context.Canceled)
	if output.Len() == 0 {
		t.Fatal("LogMode lost cancellation handling")
	}
}

func TestDatabaseLoggerRetainsFailuresAndSlowQueries(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tc := range []struct {
		name string
		ctx  context.Context
		err  error
	}{
		{"deadline", ctx, context.DeadlineExceeded},
		{"mixed", ctx, errors.Join(context.Canceled, errors.New("connection refused"))},
		{"unrelated", ctx, errors.New("connection refused")},
		{"active", t.Context(), context.Canceled},
		{"slow", t.Context(), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			l := databaseLogger{
				Interface: logger.New(log.New(&output, "", 0), logger.Config{LogLevel: logger.Warn, SlowThreshold: time.Millisecond}),
				log:       slog.New(slog.NewJSONHandler(&output, nil)), level: logger.Warn,
			}
			l.Trace(tc.ctx, time.Now().Add(-time.Second), func() (string, int64) { return "SELECT 1", 1 }, tc.err)
			if !strings.Contains(output.String(), "SELECT 1") || strings.Contains(output.String(), "database query canceled") {
				t.Fatalf("lost original diagnostic: %s", &output)
			}
		})
	}
}
