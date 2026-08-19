package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

var pathParameter = regexp.MustCompile(`\{[^}]+\}`)

// Every documented operation must resolve to a registered server route.
// OPTIONS lets ServeMux report methods without executing handlers or needing
// database dependencies.
func TestOpenAPIOperationsHaveServerRoutes(t *testing.T) {
	contents, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(contents, &document); err != nil {
		t.Fatal(err)
	}

	server := &Server{mux: http.NewServeMux()}
	server.routes()
	for path, item := range document.Paths {
		for method := range item {
			method = strings.ToUpper(method)
			if !isHTTPMethod(method) {
				continue
			}

			requestPath := "/api/v1" + pathParameter.ReplaceAllString(path, "test")
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodOptions, requestPath, nil)
			server.mux.ServeHTTP(recorder, request)

			if !strings.Contains(recorder.Header().Get("Allow"), method) {
				t.Errorf("%s %s is documented but not registered; Allow = %q",
					method, path, recorder.Header().Get("Allow"))
			}
		}
	}
}

func isHTTPMethod(value string) bool {
	switch value {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// Coop no longer serves packages, so nothing may claim /install/. The web
// handler answers it instead, and refuses it there; a route registered here
// would quietly resurrect an endpoint every client has already moved off.
func TestNothingServesTheRetiredInstaller(t *testing.T) {
	bare := &Server{mux: http.NewServeMux()}
	bare.routes()
	for _, path := range []string{"/install/", "/install", "/install/releases/parent.json"} {
		if got := routeStatus(bare.mux, path); got != http.StatusNotFound {
			t.Errorf("%s = %d with no web handler, want 404", path, got)
		}
	}

	withWeb := &Server{
		deps: Deps{
			Web: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
			}),
		},
		mux: http.NewServeMux(),
	}
	withWeb.routes()
	if got := routeStatus(withWeb.mux, "/install/"); got != http.StatusAccepted {
		t.Errorf("/install/ = %d, want it to reach the web handler", got)
	}
}

func routeStatus(handler http.Handler, path string) int {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder.Code
}
