package fledge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const parentBundle = "fish.nerdswhofish.coop.parent"

func TestNewRejectsUnusableOrigins(t *testing.T) {
	for _, raw := range []string{
		"http://fledge.example",
		"fledge.example",
		"https://",
		"https://user:pass@fledge.example",
		"https://fledge.example?a=b",
		"https://fledge.example#frag",
	} {
		if _, err := New(raw); err == nil {
			t.Errorf("New(%q) = nil error, want rejection", raw)
		}
	}
}

func TestLatestReadsPublishedBuild(t *testing.T) {
	var gotPath string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"bundle_id": "` + parentBundle + `",
			"name": "Cooper The Cop",
			"version": "1.8.0",
			"build": "10800",
			"build_id": "570fdeca6768",
			"install_page_url": "https://fledge.example/a/` + parentBundle + `",
			"expired": false
		}`))
	}))
	defer server.Close()

	client := testClient(t, server)
	release, err := client.Latest(context.Background(), parentBundle)
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}

	if want := "/api/v1/apps/" + parentBundle + "/latest"; gotPath != want {
		t.Errorf("requested %q, want %q", gotPath, want)
	}
	if release.Build != "10800" || release.BuildID != "570fdeca6768" {
		t.Errorf("Latest() = build %q id %q, want 10800/570fdeca6768", release.Build, release.BuildID)
	}
}

func TestLatestDistinguishesUnpublishedFromBroken(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
	}{
		{name: "nothing published", status: http.StatusNotFound, want: ErrNotFound},
		{name: "server error", status: http.StatusInternalServerError},
		{name: "unauthorized", status: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			_, err := testClient(t, server).Latest(context.Background(), parentBundle)
			if err == nil {
				t.Fatal("Latest() = nil error, want failure")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("Latest() error = %v, want %v", err, tt.want)
			}
			if tt.want == nil && errors.Is(err, ErrNotFound) {
				t.Error("Latest() reported ErrNotFound for an outage, which reads as a normal state")
			}
		})
	}
}

// A build-less response is treated as a failure rather than published, so a
// stale cached answer wins over an empty one.
func TestLatestRejectsResponseWithoutABuild(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"bundle_id": "` + parentBundle + `"}`))
	}))
	defer server.Close()

	if _, err := testClient(t, server).Latest(context.Background(), parentBundle); err == nil {
		t.Error("Latest() = nil error for a release with no build")
	}
}

func TestURLsForBuild(t *testing.T) {
	client, err := New("https://fledge.example")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	release := &Release{BundleID: parentBundle, BuildID: "570fdeca6768"}

	want := "https://fledge.example/a/" + parentBundle + "/570fdeca6768/manifest.plist"
	if got := client.ManifestURL(release); got != want {
		t.Errorf("ManifestURL() = %q, want %q", got, want)
	}

	// Coop may be configured with an internal address while Fledge publishes a
	// public one. Composing the manifest from the internal name produces an
	// install link that works at home and silently fails everywhere else.
	release.InstallPageURL = "https://public.example/a/" + parentBundle
	wantPublic := "https://public.example/a/" + parentBundle + "/570fdeca6768/manifest.plist"
	if got := client.ManifestURL(release); got != wantPublic {
		t.Errorf("ManifestURL() = %q, want the public %q", got, wantPublic)
	}

	// Fledge reports the install page against its own public URL, which is the
	// name devices must use, so its answer wins over a composed one.
	release.InstallPageURL = "https://public.example/a/" + parentBundle
	if got := client.InstallPageURL(release); got != release.InstallPageURL {
		t.Errorf("InstallPageURL() = %q, want Fledge's own %q", got, release.InstallPageURL)
	}
	release.InstallPageURL = ""
	if got := client.InstallPageURL(release); !strings.HasPrefix(got, "https://fledge.example/a/") {
		t.Errorf("InstallPageURL() = %q, want a composed fallback", got)
	}
}

func testClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client, err := New(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("New(%q) error = %v", server.URL, err)
	}
	return client
}
