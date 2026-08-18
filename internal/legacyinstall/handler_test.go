package legacyinstall

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nerdswhofish/coop/internal/fledge"
)

const (
	parentBundle = "fish.nerdswhofish.coop.parent"
	childBundle  = "fish.nerdswhofish.coop.child"
)

// legacyRelease mirrors CoopKit's AppRelease exactly. Clients shipped before
// the move to Fledge decode this shape, and they fail open, so a renamed field
// does not error: it silently stops every installed app from ever updating.
type legacyRelease struct {
	App          string `json:"app"`
	Title        string `json:"title"`
	Build        string `json:"build"`
	InstallURL   string `json:"installUrl"`
	InstallerURL string `json:"installerUrl"`
}

func TestServesTheShapeOldClientsDecode(t *testing.T) {
	upstream := fakeFledge(t, `{
		"bundle_id": "` + parentBundle + `",
		"build": "14",
		"build_id": "570fdeca6768",
		"install_page_url": "https://public.example/a/` + parentBundle + `"
	}`)
	defer upstream.Close()

	body, status := get(t, handlerFor(t, upstream), "/releases/parent.json")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	var got legacyRelease
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decoding as a legacy client would: %v", err)
	}
	if got.App != "parent" || got.Title != "Cooper The Cop" || got.Build != "14" {
		t.Errorf("release = %+v, want parent/Cooper The Cop/14", got)
	}
	if got.InstallerURL != "https://public.example/a/"+parentBundle {
		t.Errorf("installerUrl = %q, want Fledge's install page", got.InstallerURL)
	}

	// The manifest must sit on the origin Fledge publishes, not on whatever
	// address Coop was configured with, or installing away from home breaks.
	manifest := "https://public.example/a/" + parentBundle + "/570fdeca6768/manifest.plist"
	want := "itms-services://?action=download-manifest&url=" + url.QueryEscape(manifest)
	if got.InstallURL != want {
		t.Errorf("installUrl = %q, want %q", got.InstallURL, want)
	}

	var keys map[string]any
	if err := json.Unmarshal(body, &keys); err != nil {
		t.Fatalf("unmarshalling to a map: %v", err)
	}
	for _, key := range []string{"app", "title", "build", "installUrl", "installerUrl"} {
		if _, ok := keys[key]; !ok {
			t.Errorf("response is missing %q", key)
		}
	}
	if len(keys) != 5 {
		t.Errorf("response has %d keys, want exactly the 5 legacy ones: %v", len(keys), keys)
	}
}

func TestChildIsServedFromItsOwnBundle(t *testing.T) {
	var requested atomic.Value
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested.Store(r.URL.Path)
		_, _ = w.Write([]byte(`{"bundle_id":"` + childBundle + `","build":"22","build_id":"e8fba2b4c97b"}`))
	}))
	defer upstream.Close()

	body, status := get(t, handlerFor(t, upstream), "/releases/child.json")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if path, _ := requested.Load().(string); !strings.Contains(path, childBundle) {
		t.Errorf("asked Fledge for %q, want the child bundle", path)
	}

	var got legacyRelease
	_ = json.Unmarshal(body, &got)
	if got.App != "child" || got.Title != "Cooper Watch" || got.Build != "22" {
		t.Errorf("release = %+v, want child/Cooper Watch/22", got)
	}
}

func TestFledgeIsNotAskedOnEveryRequest(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"bundle_id":"` + parentBundle + `","build":"14","build_id":"aaa"}`))
	}))
	defer upstream.Close()

	handler := handlerFor(t, upstream)
	for range 3 {
		if _, status := get(t, handler, "/releases/parent.json"); status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("asked Fledge %d times, want 1 within the refresh interval", got)
	}
}

// A Fledge outage must not read as "you are up to date", because that is
// indistinguishable from the state this whole package exists to avoid.
func TestOutageServesTheLastKnownBuild(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"bundle_id":"` + parentBundle + `","build":"14","build_id":"aaa"}`))
	}))
	defer upstream.Close()

	clock := time.Now()
	handler := handlerFor(t, upstream)
	handler.now = func() time.Time { return clock }

	if _, status := get(t, handler, "/releases/parent.json"); status != http.StatusOK {
		t.Fatal("priming the cache failed")
	}

	healthy.Store(false)
	clock = clock.Add(2 * refreshInterval)

	body, status := get(t, handler, "/releases/parent.json")
	if status != http.StatusOK {
		t.Fatalf("status = %d during an outage, want the last known build", status)
	}
	var got legacyRelease
	_ = json.Unmarshal(body, &got)
	if got.Build != "14" {
		t.Errorf("build = %q during an outage, want the cached 14", got.Build)
	}
}

func TestUnknownBuildIsNotAnUpdate(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer upstream.Close()

	if _, status := get(t, handlerFor(t, upstream), "/releases/parent.json"); status != http.StatusNotFound {
		t.Errorf("status = %d with nothing published, want 404", status)
	}
}

func TestRoutingRejectsAnythingElse(t *testing.T) {
	upstream := fakeFledge(t, `{"bundle_id":"x","build":"1","build_id":"a"}`)
	defer upstream.Close()
	handler := handlerFor(t, upstream)

	for _, path := range []string{
		"/releases/nope.json",
		"/releases/parent",
		"/releases/parent.json.json",
		"/manifests/parent.plist",
		"/apps/CooperTheCop.ipa",
		"/styles.css",
	} {
		if _, status := get(t, handler, path); status != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, status)
		}
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/releases/parent.json", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d, want 405", recorder.Code)
	}
}

func TestRootSendsPeopleToFledge(t *testing.T) {
	upstream := fakeFledge(t, `{}`)
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	handlerFor(t, upstream).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); !strings.HasPrefix(location, "https://") {
		t.Errorf("Location = %q, want the Fledge origin", location)
	}
}

func fakeFledge(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
}

func handlerFor(t *testing.T, upstream *httptest.Server) *Handler {
	t.Helper()
	client, err := fledge.New(upstream.URL, fledge.WithHTTPClient(upstream.Client()))
	if err != nil {
		t.Fatalf("fledge.New() error = %v", err)
	}

	handler, err := New(client, parentBundle, childBundle,
		slog.New(slog.DiscardHandler), time.Now)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func get(t *testing.T, handler *Handler, path string) ([]byte, int) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return body, recorder.Code
}
