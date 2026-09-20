//go:build integration

package store

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/nerdswhofish/coop/internal/telemetry"
)

func TestCanceledDatabaseQueryLog(t *testing.T) {
	db := testDB(t)
	var output bytes.Buffer
	db.Logger = newDatabaseLogger(slog.New(telemetry.LogHandler(slog.NewJSONHandler(&output, nil))))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var value string
	err := db.WithContext(ctx).Raw("SELECT 'private-value'").Scan(&value).Error
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("query returned %v", err)
	}
	if !strings.Contains(output.String(), `"msg":"database query canceled"`) || !strings.Contains(output.String(), `"level":"INFO"`) {
		t.Fatalf("missing cancellation log: %s", &output)
	}
	if strings.Contains(output.String(), "private-value") {
		t.Fatal("query value leaked into cancellation log")
	}
}
