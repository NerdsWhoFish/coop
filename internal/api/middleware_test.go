package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	server := &Server{}
	handler := server.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/healthz", nil))

	want := map[string]string{
		"Strict-Transport-Security": "max-age=31536000",
		"X-Content-Type-Options":    "nosniff",
		"Referrer-Policy":           "no-referrer",
		"Content-Security-Policy":   "default-src 'none'; frame-ancestors 'none'",
	}
	for header, value := range want {
		if got := response.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}
}

func TestWebSecurityHeadersPermitOnlyEmbeddedPlayer(t *testing.T) {
	server := &Server{}
	handler := server.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := response.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
	csp := response.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "frame-src https://www.youtube-nocookie.com", "object-src 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("Content-Security-Policy = %q, missing %q", csp, want)
		}
	}
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Errorf("Content-Security-Policy permits inline scripts: %q", csp)
	}
}

func TestSameOriginIgnoresTrailingPathButNotSchemeOrPort(t *testing.T) {
	for _, test := range []struct {
		candidate  string
		configured string
		want       bool
	}{
		{"https://coop.example", "https://coop.example/", true},
		{"https://coop.example", "http://coop.example", false},
		{"https://coop.example:8443", "https://coop.example", false},
		{"", "https://coop.example", false},
	} {
		if got := sameOrigin(test.candidate, test.configured); got != test.want {
			t.Errorf("sameOrigin(%q, %q) = %v, want %v", test.candidate, test.configured, got, test.want)
		}
	}
}
