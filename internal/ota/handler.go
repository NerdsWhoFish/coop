// Package ota serves registered-device iOS packages from operator-managed storage.
package ota

import (
	"embed"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed index.html styles.css
var assets embed.FS

var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type application struct {
	Slug        string
	IPAName     string
	BundleID    string
	Title       string
	Description string
	Badge       string
}

var applications = []application{
	{
		Slug:        "parent",
		IPAName:     "CooperTheCop.ipa",
		BundleID:    "fish.nerdswhofish.coop.parent",
		Title:       "Cooper The Cop",
		Description: "Parent controls, approvals, pairing, policy, and the family dispatch desk.",
		Badge:       "P",
	},
	{
		Slug:        "child",
		IPAName:     "CooperWatch.ipa",
		BundleID:    "fish.nerdswhofish.coop.child",
		Title:       "Cooper Watch",
		Description: "The approved feed, search, subscriptions, Shorts, and kid-side requests.",
		Badge:       "W",
	},
}

type Handler struct {
	directory string
	baseURL   *url.URL
	index     *template.Template
}

type pageApplication struct {
	application
	Available  bool
	InstallURL template.URL
}

// New creates an installer rooted at directory.
func New(publicURL, directory string) (*Handler, error) {
	baseURL, err := url.Parse(publicURL)
	if err != nil || baseURL.Scheme != "https" || baseURL.Host == "" ||
		baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, errors.New("OTA requires an HTTPS public URL")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/install/"
	baseURL.RawPath = ""
	info, err := os.Stat(directory)
	if err != nil {
		return nil, fmt.Errorf("opening OTA package directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("opening OTA package directory: %s is not a directory", directory)
	}

	contents, err := assets.ReadFile("index.html")
	if err != nil {
		return nil, fmt.Errorf("reading embedded OTA page: %w", err)
	}
	index, err := template.New("index").Parse(string(contents))
	if err != nil {
		return nil, fmt.Errorf("parsing embedded OTA page: %w", err)
	}

	return &Handler{directory: directory, baseURL: baseURL, index: index}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch {
	case r.URL.Path == "/":
		h.serveIndex(w, r)
	case r.URL.Path == "/styles.css":
		h.serveStyles(w, r)
	case strings.HasPrefix(r.URL.Path, "/manifests/"):
		h.serveManifest(w, r, strings.TrimPrefix(r.URL.Path, "/manifests/"))
	case strings.HasPrefix(r.URL.Path, "/releases/"):
		h.serveRelease(w, r, strings.TrimPrefix(r.URL.Path, "/releases/"))
	case strings.HasPrefix(r.URL.Path, "/apps/"):
		h.serveIPA(w, r, strings.TrimPrefix(r.URL.Path, "/apps/"))
	default:
		http.NotFound(w, r)
	}
}

type release struct {
	App          string `json:"app"`
	Title        string `json:"title"`
	Build        string `json:"build"`
	InstallURL   string `json:"installUrl"`
	InstallerURL string `json:"installerUrl"`
}

func (h *Handler) serveRelease(w http.ResponseWriter, r *http.Request, name string) {
	app, ok := applicationBySlug(strings.TrimSuffix(name, ".json"))
	if !ok || name != app.Slug+".json" {
		http.NotFound(w, r)
		return
	}
	build, available := h.version(app)
	if !available {
		http.NotFound(w, r)
		return
	}
	manifestURL := h.baseURL.JoinPath("manifests", app.Slug+".plist").String()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(release{
		App:          app.Slug,
		Title:        app.Title,
		Build:        build,
		InstallURL:   "itms-services://?action=download-manifest&url=" + url.QueryEscape(manifestURL),
		InstallerURL: h.baseURL.String(),
	})
}

func (h *Handler) serveIndex(w http.ResponseWriter, _ *http.Request) {
	apps := make([]pageApplication, 0, len(applications))
	for _, app := range applications {
		_, available := h.version(app)
		manifestURL := h.baseURL.JoinPath("manifests", app.Slug+".plist").String()
		apps = append(apps, pageApplication{
			application: app,
			Available:   available,
			InstallURL: template.URL("itms-services://?action=download-manifest&url=" +
				url.QueryEscape(manifestURL)),
		})
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	if err := h.index.Execute(w, map[string]any{"Applications": apps}); err != nil {
		http.Error(w, "rendering installer", http.StatusInternalServerError)
	}
}

func (h *Handler) serveStyles(w http.ResponseWriter, r *http.Request) {
	contents, err := assets.ReadFile("styles.css")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write(contents)
}

func (h *Handler) serveManifest(w http.ResponseWriter, r *http.Request, name string) {
	app, ok := applicationBySlug(strings.TrimSuffix(name, ".plist"))
	if !ok || name != app.Slug+".plist" {
		http.NotFound(w, r)
		return
	}
	version, available := h.version(app)
	if !available {
		http.NotFound(w, r)
		return
	}

	packageURL := h.baseURL.JoinPath("apps", app.IPAName).String()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = fmt.Fprintf(w, manifest, xmlEscape(packageURL), xmlEscape(app.BundleID), xmlEscape(version), xmlEscape(app.Title))
}

func (h *Handler) serveIPA(w http.ResponseWriter, r *http.Request, name string) {
	app, ok := applicationByIPA(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if _, available := h.version(app); !available {
		http.NotFound(w, r)
		return
	}

	file, err := os.Open(filepath.Join(h.directory, app.IPAName))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", app.IPAName))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, app.IPAName, info.ModTime(), file)
}

func (h *Handler) version(app application) (string, bool) {
	info, err := os.Stat(filepath.Join(h.directory, app.IPAName))
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	contents, err := os.ReadFile(filepath.Join(h.directory, app.IPAName+".version"))
	if err != nil {
		return "", false
	}
	version := strings.TrimSpace(string(contents))
	return version, versionPattern.MatchString(version)
}

func applicationBySlug(slug string) (application, bool) {
	for _, app := range applications {
		if app.Slug == slug {
			return app, true
		}
	}
	return application{}, false
}

func applicationByIPA(name string) (application, bool) {
	for _, app := range applications {
		if app.IPAName == name {
			return app, true
		}
	}
	return application{}, false
}

func xmlEscape(value string) string {
	var escaped strings.Builder
	_ = xml.EscapeText(&escaped, []byte(value))
	return escaped.String()
}

const manifest = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "https://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>items</key><array><dict><key>assets</key><array><dict><key>kind</key><string>software-package</string><key>url</key><string>%s</string></dict></array><key>metadata</key><dict><key>bundle-identifier</key><string>%s</string><key>bundle-version</key><string>%s</string><key>kind</key><string>software</string><key>title</key><string>%s</string></dict></dict></array></dict></plist>
`
