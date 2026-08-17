package youtube

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"time"
)

// CacheFloor is the shortest TTL any cached response may have. A floor rather
// than a default: one short-TTL call site can drain the search allocation,
// which cannot be bought back.
const CacheFloor = time.Hour

// Cache stores raw API responses keyed by request.
type Cache interface {
	Get(ctx context.Context, key string) (body []byte, ok bool, err error)
	Put(ctx context.Context, key, endpoint string, body []byte, ttl time.Duration) error
}

// TTLs holds the per-endpoint cache lifetimes. Every value is clamped up to
// CacheFloor when read, so a misconfiguration cannot bypass the floor.
type TTLs struct {
	Default time.Duration
	Channel time.Duration
	Uploads time.Duration
	Video   time.Duration
	Feed    time.Duration
	Search  time.Duration
}

// DefaultTTLs are the production cache lifetimes for each YouTube endpoint.
func DefaultTTLs() TTLs {
	return TTLs{
		Default: CacheFloor,
		Channel: 30 * 24 * time.Hour,
		Uploads: 6 * time.Hour,
		Video:   30 * 24 * time.Hour,
		Feed:    6 * time.Hour,
		Search:  24 * time.Hour,
	}
}

// For returns the lifetime for an endpoint, never below CacheFloor.
func (t TTLs) For(endpoint string) time.Duration {
	var ttl time.Duration
	switch endpoint {
	case endpointChannels:
		ttl = t.Channel
	case endpointPlaylistItems:
		ttl = t.Uploads
	case endpointVideos:
		ttl = t.Video
	case endpointFeed:
		ttl = t.Feed
	case endpointSearch:
		ttl = t.Search
	default:
		ttl = t.Default
	}
	return max(ttl, CacheFloor)
}

// NormalizeQuery folds trivially different phrasings of the same search onto
// one cache entry, which is what protects the 100-call-per-day bucket.
func NormalizeQuery(q string) string {
	return strings.Join(strings.Fields(strings.ToLower(q)), " ")
}

// CacheKey derives a stable key from an endpoint and its parameters.
// url.Values.Encode sorts keys, so the same logical request hashes the same
// way regardless of the order parameters were added.
func CacheKey(endpoint string, params url.Values) string {
	sum := sha256.Sum256([]byte(endpoint + "?" + params.Encode()))
	return endpoint + ":" + hex.EncodeToString(sum[:16])
}
