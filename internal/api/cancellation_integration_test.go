//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/nerdswhofish/coop/internal/auth"
	"github.com/nerdswhofish/coop/internal/config"
	"github.com/nerdswhofish/coop/internal/feed"
	"github.com/nerdswhofish/coop/internal/store"
	"github.com/nerdswhofish/coop/internal/telemetry"
	"github.com/nerdswhofish/coop/internal/testdb"
	"github.com/nerdswhofish/coop/internal/youtube"
)

func cancellationServer(t *testing.T) (*Server, *bytes.Buffer, string) {
	t.Helper()
	dsn := testdb.New(t)
	db, err := store.Open(t.Context(), config.Database{
		DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 2, ConnMaxLifetime: time.Minute,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	catalog := store.NewCatalog(db, time.Now)
	rules := store.NewRules(db, time.Now)
	activity := store.NewActivity(db, time.Now)
	channelID, videoID := uuid.NewString(), uuid.NewString()
	if err := catalog.UpsertChannels(t.Context(), []youtube.Channel{{ID: channelID, Title: "Private test channel"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Delete(&store.Video{}, "id = ?", videoID).Error; err != nil {
			t.Error(err)
		}
		if err := db.Delete(&store.Channel{}, "id = ?", channelID).Error; err != nil {
			t.Error(err)
		}
	})
	if err := catalog.UpsertVideos(t.Context(), []youtube.Video{{
		ID: videoID, ChannelID: channelID, Title: "Private test video",
		ThumbnailURL: "https://i.ytimg.com/vi/private-video/maxresdefault.jpg",
		PublishedAt:  time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(telemetry.LogHandler(slog.NewJSONHandler(&logs, nil)))
	return &Server{deps: Deps{
		DB: db, Catalog: catalog, Rules: rules, Activity: activity,
		Feed: feed.New(catalog, rules, activity), Logger: logger, Now: time.Now,
	}}, &logs, videoID
}

func cancellationRequest(ctx context.Context, method, route, body string) (*http.Request, trace.SpanContext) {
	span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}})
	r := httptest.NewRequest(method, "/private-path?token=private-token", strings.NewReader(body))
	r.Pattern = route
	return r.WithContext(trace.ContextWithSpanContext(ctx, span)), span
}

func assertCancellationLogs(t *testing.T, logs *bytes.Buffer, span trace.SpanContext, route string, wantError bool) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(logs.Bytes()))
	var foundRequest, foundError bool
	for {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		if record["level"] == "ERROR" {
			foundError = true
		}
		if !wantError && (record["level"] == "WARN" || record["level"] == "ERROR") {
			t.Errorf("cancellation logged as failure: %v", record)
		}
		if record["msg"] == "request" {
			foundRequest = true
			if record["trace_id"] != span.TraceID().String() || record["span_id"] != span.SpanID().String() || record["route"] != route {
				t.Errorf("request lost correlation or safe route: %v", record)
			}
		}
	}
	if !foundRequest || foundError != wantError {
		t.Errorf("request=%v error=%v, want request=true error=%v: %s", foundRequest, foundError, wantError, logs.String())
	}
	if strings.Contains(logs.String(), "private-") || strings.Contains(logs.String(), "Private test") {
		t.Errorf("private request or catalog data leaked: %s", logs.String())
	}
}

func TestCanceledPlaybackDatabaseRead(t *testing.T) {
	s, logs, videoID := cancellationServer(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	const route = "PUT /api/v1/child/playback"
	r, span := cancellationRequest(ctx, http.MethodPut, route, `{"videoId":"`+videoID+`","state":"heartbeat"}`)
	w := httptest.NewRecorder()
	s.logRequests(s.handle(func(w http.ResponseWriter, r *http.Request) error {
		return s.handlePlaybackLease(w, r, auth.Child{ID: uuid.New(), FamilyID: uuid.New(), DeviceID: uuid.New()})
	})).ServeHTTP(w, r)
	if w.Code != 499 || w.Body.Len() != 0 {
		t.Fatalf("canceled playback response=%d body=%q", w.Code, w.Body.String())
	}
	assertCancellationLogs(t, logs, span, route, false)
}

func TestThumbnailCancellationAndFailures(t *testing.T) {
	s, logs, videoID := cancellationServer(t)
	for _, scenario := range []string{
		"database_canceled", "upstream_canceled", "upstream_503", "upstream_deadline",
		"canceled_transport_failure", "canceled_transport_deadline", "canceled_upstream_503",
	} {
		t.Run(scenario, func(t *testing.T) {
			logs.Reset()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "database_canceled" {
				cancel()
			}
			if scenario == "upstream_deadline" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, time.Second)
				defer deadlineCancel()
			}
			previousTransport := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previousTransport })
			calls := 0
			http.DefaultTransport = thumbnailTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				switch scenario {
				case "canceled_transport_failure":
					cancel()
					return nil, errors.New("private-transport-failure")
				case "canceled_transport_deadline":
					cancel()
					return nil, context.DeadlineExceeded
				case "canceled_upstream_503":
					cancel()
					return thumbnailResponse(http.StatusServiceUnavailable, nil), nil
				}
				if scenario == "upstream_503" {
					return thumbnailResponse(http.StatusServiceUnavailable, nil), nil
				}
				if scenario == "upstream_canceled" {
					cancel()
				}
				<-r.Context().Done()
				return nil, r.Context().Err()
			})
			const route = "GET /api/v1/thumb/{videoId}"
			r, span := cancellationRequest(ctx, http.MethodGet, route, "")
			r.SetPathValue("videoId", videoID)
			w := httptest.NewRecorder()
			s.logRequests(http.HandlerFunc(s.handleThumbnail)).ServeHTTP(w, r)
			wantStatus, wantCalls := 499, 1
			wantError := scenario == "upstream_503" || scenario == "upstream_deadline" || strings.HasPrefix(scenario, "canceled_")
			if wantError {
				wantStatus = http.StatusBadGateway
			}
			if scenario == "database_canceled" {
				wantCalls = 0
			} else if scenario == "upstream_503" {
				wantCalls = 3
			}
			if w.Code != wantStatus || calls != wantCalls {
				t.Fatalf("status=%d calls=%d, want status=%d calls=%d", w.Code, calls, wantStatus, wantCalls)
			}
			assertCancellationLogs(t, logs, span, route, wantError)
		})
	}
}
