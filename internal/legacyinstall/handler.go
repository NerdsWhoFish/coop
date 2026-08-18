// Package legacyinstall keeps Coop's retired /install/ release endpoint
// answering, sourced from Fledge instead of local storage.
//
// Clients built before Coop moved to Fledge poll /install/releases/{app}.json
// and fail open on anything that is not a decodable 200, so removing the route
// outright would not break them: it would silently and permanently remove the
// only channel that can tell them to update. This package exists to migrate
// those clients, and is deleted once none remain.
package legacyinstall

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/nerdswhofish/coop/internal/fledge"
)

// refreshInterval bounds how often Fledge is asked. Every client checks on
// launch and on every foreground, so this is the difference between a poll per
// app launch and a poll per minute.
const refreshInterval = time.Minute

type application struct {
	Slug     string
	Title    string
	BundleID string
}

// Handler serves the legacy release endpoint.
type Handler struct {
	fledge       *fledge.Client
	logger       *slog.Logger
	now          func() time.Time
	applications []application

	mu     sync.Mutex
	cached map[string]entry
}

type entry struct {
	release   *fledge.Release
	refreshed time.Time
}

type release struct {
	App          string `json:"app"`
	Title        string `json:"title"`
	Build        string `json:"build"`
	InstallURL   string `json:"installUrl"`
	InstallerURL string `json:"installerUrl"`
}

// New returns a handler bridging the legacy endpoint onto client.
func New(client *fledge.Client, parentBundleID, childBundleID string,
	logger *slog.Logger, now func() time.Time) (*Handler, error) {
	if client == nil {
		return nil, errors.New("legacyinstall: nil Fledge client")
	}
	if parentBundleID == "" || childBundleID == "" {
		return nil, errors.New("legacyinstall: both bundle identifiers are required")
	}
	return &Handler{
		fledge: client,
		logger: logger,
		now:    now,
		applications: []application{
			{Slug: "parent", Title: "Cooper The Cop", BundleID: parentBundleID},
			{Slug: "child", Title: "Cooper Watch", BundleID: childBundleID},
		},
		cached: make(map[string]entry),
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch {
	case r.URL.Path == "/":
		h.redirectToFledge(w, r)
	case strings.HasPrefix(r.URL.Path, "/releases/"):
		h.serveRelease(w, r, strings.TrimPrefix(r.URL.Path, "/releases/"))
	default:
		http.NotFound(w, r)
	}
}

// redirectToFledge covers bookmarks of the old installer page. Coop no longer
// hosts packages, so there is nothing here to render.
func (h *Handler) redirectToFledge(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, h.fledge.BaseURL(), http.StatusFound)
}

func (h *Handler) serveRelease(w http.ResponseWriter, r *http.Request, name string) {
	app, ok := h.applicationBySlug(strings.TrimSuffix(name, ".json"))
	if !ok || name != app.Slug+".json" {
		http.NotFound(w, r)
		return
	}

	latest := h.latest(r, app)
	if latest == nil {
		// Not knowing the published build is indistinguishable from nothing
		// being published, and both must read as "no update" rather than an
		// error a fail-open client would treat the same way anyway.
		http.NotFound(w, r)
		return
	}

	manifestURL := h.fledge.ManifestURL(latest)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(release{
		App:          app.Slug,
		Title:        app.Title,
		Build:        latest.Build,
		InstallURL:   "itms-services://?action=download-manifest&url=" + url.QueryEscape(manifestURL),
		InstallerURL: h.fledge.InstallPageURL(latest),
	})
}

// latest returns the published build, preferring a recent answer and falling
// back to the last one seen. Serving a stale build number keeps update
// prompts working through a Fledge outage; serving nothing strands clients.
func (h *Handler) latest(r *http.Request, app application) *fledge.Release {
	h.mu.Lock()
	cached, hit := h.cached[app.Slug]
	h.mu.Unlock()

	if hit && h.now().Sub(cached.refreshed) < refreshInterval {
		return cached.release
	}

	fetched, err := h.fledge.Latest(r.Context(), app.BundleID)
	if err != nil {
		if !errors.Is(err, fledge.ErrNotFound) {
			h.logger.Warn("reading release from Fledge failed",
				"app", app.Slug, "bundle_id", app.BundleID, "error", err,
				"serving_stale", hit)
		}
		if hit {
			return cached.release
		}
		return nil
	}

	h.mu.Lock()
	h.cached[app.Slug] = entry{release: fetched, refreshed: h.now()}
	h.mu.Unlock()
	return fetched
}

func (h *Handler) applicationBySlug(slug string) (application, bool) {
	for _, app := range h.applications {
		if app.Slug == slug {
			return app, true
		}
	}
	return application{}, false
}
