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

func TestInstallerRoutesAreOptIn(t *testing.T) {
	disabled := &Server{mux: http.NewServeMux()}
	disabled.routes()
	if got := routeStatus(disabled.mux, "/"); got != http.StatusNotFound {
		t.Fatalf("disabled root status = %d, want 404", got)
	}
	if got := routeStatus(disabled.mux, "/install/"); got != http.StatusNotFound {
		t.Fatalf("disabled installer status = %d, want 404", got)
	}

	enabled := &Server{
		deps: Deps{
			Installer: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}),
			Web: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
			}),
		},
		mux: http.NewServeMux(),
	}
	enabled.routes()
	rootRecorder := httptest.NewRecorder()
	enabled.mux.ServeHTTP(rootRecorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if rootRecorder.Code != http.StatusAccepted {
		t.Fatalf("web root = %d, want %d", rootRecorder.Code, http.StatusAccepted)
	}
	if got := routeStatus(enabled.mux, "/install/"); got != http.StatusNoContent {
		t.Fatalf("enabled installer status = %d, want 204", got)
	}

	recorder := httptest.NewRecorder()
	enabled.mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/install", nil))
	if recorder.Code != http.StatusPermanentRedirect || recorder.Header().Get("Location") != "/install/" {
		t.Fatalf("installer redirect = %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func routeStatus(handler http.Handler, path string) int {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder.Code
}
