package api

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nerdswhofish/coop/internal/telemetry"
	"go.opentelemetry.io/otel/trace"
)

func TestFailureLogsKeepTraceContextWithoutPrivateDetails(t *testing.T) {
	for _, panicking := range []bool{false, true} {
		var output bytes.Buffer
		logger := slog.New(telemetry.LogHandler(slog.NewJSONHandler(&output, nil)))
		request := httptest.NewRequest(http.MethodGet, "/private-value?token=private-value", nil)
		request.Pattern = "GET /{id}"
		span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}})
		request = request.WithContext(trace.ContextWithSpanContext(request.Context(), span))
		response := httptest.NewRecorder()
		if panicking {
			server := &Server{deps: Deps{Logger: logger}}
			server.recoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				panic("private-value")
			})).ServeHTTP(response, request)
		} else {
			writeError(response, request, logger, internal(errors.New("private-value")))
		}
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("expected failure response, got %d", response.Code)
		}
		if strings.Contains(output.String(), "private-value") {
			t.Fatal("private request or error content leaked into logs")
		}
		if !strings.Contains(output.String(), span.TraceID().String()) || !strings.Contains(output.String(), span.SpanID().String()) {
			t.Fatal("failure log lost trace correlation")
		}
		if !strings.Contains(output.String(), "GET /{id}") {
			t.Fatal("failure log lost the safe route template")
		}
	}
}
