// Package fledge reads release metadata from a Fledge over-the-air
// distribution server. Coop never uploads: publishing is an operator or CI
// action, and this client only ever reads the public app API.
package fledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNotFound means Fledge has no published build for the bundle. It is a
// distinct error because "nothing published yet" and "Fledge is broken" want
// opposite handling: the first is a normal state, the second is an outage.
var ErrNotFound = errors.New("fledge: no published build")

// maxBody caps the response read. Release metadata is a few hundred bytes; a
// far larger body means we are talking to something that is not Fledge.
const maxBody = 1 << 20

// Release is the subset of Fledge's latest-build metadata that Coop uses. Its
// UpdateAvailable field is deliberately unmapped: it is string inequality
// against the newest upload, so an older archive re-published reads as newer.
type Release struct {
	BundleID       string `json:"bundle_id"`
	Name           string `json:"name"`
	Version        string `json:"version"`
	Build          string `json:"build"`
	BuildID        string `json:"build_id"`
	Size           int64  `json:"size"`
	MinimumOS      string `json:"minimum_os"`
	InstallPageURL string `json:"install_page_url"`
	Expired        bool   `json:"expired"`
	ExpiresAt      string `json:"expires_at"`
}

// Client reads from one Fledge server.
type Client struct {
	base *url.URL
	http *http.Client
}

// Option adjusts a Client.
type Option func(*Client)

// WithHTTPClient replaces the default HTTP client, for a deployment that needs
// its own transport, proxy or trust root.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) { c.http = client }
}

// New returns a client for the Fledge server at baseURL.
func New(baseURL string, opts ...Option) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing Fledge URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("fledge: base URL must be a plain HTTPS origin")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""

	client := &Client{base: parsed, http: &http.Client{Timeout: 10 * time.Second}}
	for _, opt := range opts {
		opt(client)
	}
	return client, nil
}

// Latest reads the newest published build for a bundle.
func (c *Client) Latest(ctx context.Context, bundleID string) (*Release, error) {
	if bundleID == "" {
		return nil, errors.New("fledge: empty bundle identifier")
	}
	endpoint := c.base.JoinPath("api", "v1", "apps", bundleID, "latest").String()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building Fledge request: %w", err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("reading %s from Fledge: %w", bundleID, err)
	}
	defer func() { _ = response.Body.Close() }()

	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, ErrNotFound
	default:
		return nil, fmt.Errorf("fledge returned %d for %s", response.StatusCode, bundleID)
	}

	var release Release
	if err := json.NewDecoder(io.LimitReader(response.Body, maxBody)).Decode(&release); err != nil {
		return nil, fmt.Errorf("decoding Fledge release for %s: %w", bundleID, err)
	}
	if release.Build == "" || release.BuildID == "" {
		return nil, fmt.Errorf("fledge release for %s has no build", bundleID)
	}
	return &release, nil
}

// BaseURL is the server's origin, for sending a person to Fledge itself.
func (c *Client) BaseURL() string {
	return c.base.String() + "/"
}

// ManifestURL hangs off the install page, not this client's base URL: Fledge
// reports that page publicly, and a device off the household network cannot
// resolve the internal name Coop may be configured with.
func (c *Client) ManifestURL(release *Release) string {
	if page, err := url.Parse(release.InstallPageURL); err == nil && page.Host != "" {
		return page.JoinPath(release.BuildID, "manifest.plist").String()
	}
	return c.base.JoinPath("a", release.BundleID, release.BuildID, "manifest.plist").String()
}

// InstallPageURL is the human-facing install page for a build. Fledge reports
// it against its own configured public URL, which is the name devices must
// use, so its answer wins over anything composed here.
func (c *Client) InstallPageURL(release *Release) string {
	if release.InstallPageURL != "" {
		return release.InstallPageURL
	}
	return c.base.JoinPath("a", release.BundleID).String()
}
