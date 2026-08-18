package webapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesAssetsAndFallsBackToSPA(t *testing.T) {
	handler := Handler()

	for _, path := range []string{"/", "/watch/video-id", "/channel/channel-id"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Cooper Watch") {
			t.Errorf("GET %s = %d, body %q", path, response.Code, response.Body.String())
		}
	}
}

func TestHandlerDoesNotMaskMissingAPIRoutes(t *testing.T) {
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing API status = %d, want 404", response.Code)
	}
}
