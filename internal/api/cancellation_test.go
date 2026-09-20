package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nerdswhofish/coop/internal/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRequestCancellationPreservesRealFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cancel bool
		err    error
		want   int
	}{
		{"client_canceled", true, fmt.Errorf("reading video: %w", context.Canceled), 499},
		{"wrapped_api_cancellation", true, internal(context.Canceled), 499},
		{"joined_cancellation", true, errors.Join(context.Canceled), 499},
		{"nested_cancellation", true, internal(errors.Join(context.Canceled, fmt.Errorf("lookup: %w", context.Canceled))), 499},
		{"independent_failure", true, errors.New("private-value"), 500},
		{"joined_failure", true, errors.Join(context.Canceled, errors.New("private-value")), 500},
		{"deadline", true, context.DeadlineExceeded, 500},
		{"active_request", false, context.Canceled, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			var output bytes.Buffer
			server := &Server{deps: Deps{Now: time.Now, Logger: slog.New(telemetry.LogHandler(slog.NewJSONHandler(&output, nil)))}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			request := httptest.NewRequestWithContext(ctx, http.MethodPut, "/private-value?token=private-value", nil)
			request.Pattern = "PUT /api/v1/child/playback"
			handler := otelhttp.NewHandler(server.logRequests(server.handle(func(http.ResponseWriter, *http.Request) error {
				if tc.cancel {
					cancel()
				}
				return tc.err
			})), "test", otelhttp.WithTracerProvider(provider))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status=%d want=%d", response.Code, tc.want)
			}
			spans := recorder.Ended()
			if len(spans) != 1 {
				t.Fatalf("spans=%d", len(spans))
			}
			if got := strings.Contains(output.String(), `"level":"ERROR"`); got != (tc.want == 500) {
				t.Fatalf("unexpected log severity: %s", &output)
			}
			if got := spans[0].Status().Code == codes.Error; got != (tc.want == 500) {
				t.Fatalf("unexpected span status: %v", spans[0].Status())
			}
			if !strings.Contains(output.String(), spans[0].SpanContext().TraceID().String()) {
				t.Fatal("request log lost trace correlation")
			}
			if strings.Contains(output.String(), "private-value") {
				t.Fatal("private data in log")
			}
			if tc.want == 499 && (response.Body.Len() != 0 || !strings.Contains(output.String(), `"status":499`)) {
				t.Fatalf("cancellation must have empty response and explicit status: %s", &output)
			}
		})
	}
}

func TestThumbnailCancellationSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	for _, mode := range []string{"fetch", "body", "body_failure", "queue", "deadline", "upstream"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "deadline" {
				var expire context.CancelFunc
				ctx, expire = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer expire()
			}
			if mode == "queue" {
				for range cap(thumbnailSlots) {
					thumbnailSlots <- struct{}{}
				}
				defer func() {
					for range cap(thumbnailSlots) {
						<-thumbnailSlots
					}
				}()
				cancel()
			}
			client := thumbnailClient(thumbnailTransport(func(r *http.Request) (*http.Response, error) {
				if mode == "upstream" {
					return thumbnailResponse(503, nil), nil
				}
				if mode == "body" || mode == "body_failure" {
					response := thumbnailResponse(200, nil)
					failure := error(context.Canceled)
					if mode == "body_failure" {
						failure = errors.New("private-value")
					}
					response.Body = &canceledThumbnailBody{cancel: cancel, err: failure}
					return response, nil
				}
				cancel()
				return nil, r.Context().Err()
			}))
			_, err := fetchThumbnail(ctx, client, testThumbnailURL, "abcdefghijk")
			if err == nil {
				t.Fatal("expected failure")
			}
			spans := recorder.Ended()
			span := spans[len(spans)-1]
			failure := mode == "deadline" || mode == "upstream" || mode == "body_failure"
			if (span.Status().Code == codes.Error) != failure {
				t.Fatalf("status=%v", span.Status())
			}
			if (len(span.Events()) > 0) != failure {
				t.Fatalf("events=%v", span.Events())
			}
			if strings.Contains(err.Error(), "private-value") {
				t.Fatal("private body error leaked")
			}
			if !failure {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error=%v", err)
				}
				canceled := false
				for _, attr := range span.Attributes() {
					if attr.Key == "coop.request.canceled" {
						canceled = attr.Value.AsBool()
					}
				}
				if !canceled {
					t.Fatal("missing cancellation attribute")
				}
			}
		})
	}
}

type canceledThumbnailBody struct {
	cancel context.CancelFunc
	err    error
}

func (b *canceledThumbnailBody) Read([]byte) (int, error) { b.cancel(); return 0, b.err }
func (b *canceledThumbnailBody) Close() error             { return nil }
