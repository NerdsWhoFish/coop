// Package youtube talks to the YouTube Data API and the public channel feeds,
// with a response cache and a daily spend ledger in front of both.
package youtube

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nerdswhofish/coop/internal/domain"
)

// ShortsMaxDuration is the longest a video can be and still be a Short. Only
// used for back catalog, where the authoritative RSS signal is unavailable.
const ShortsMaxDuration = 3 * time.Minute

// UploadsPlaylistID derives a channel's uploads playlist from its ID. YouTube
// guarantees UCxxxx maps to UUxxxx, so this is computed rather than fetched,
// which saves an API call per channel and never goes stale.
func UploadsPlaylistID(channelID string) (string, error) {
	if !ValidChannelID(channelID) {
		return "", fmt.Errorf("not a channel ID: %q", channelID)
	}
	return "UU" + channelID[2:], nil
}

var channelIDPattern = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)

// ValidChannelID reports whether s is a well-formed YouTube channel ID.
func ValidChannelID(s string) bool { return channelIDPattern.MatchString(s) }

// ClassifyLive turns the two independent live signals into one state.
// liveBroadcastContent alone is not enough: a finished stream reverts it to
// "none" while keeping liveStreamingDetails, so archived streams would slip in.
func ClassifyLive(liveBroadcastContent string, hasLiveStreamingDetails bool) domain.LiveState {
	switch liveBroadcastContent {
	case "live":
		return domain.LiveLive
	case "upcoming":
		return domain.LiveUpcoming
	}
	if hasLiveStreamingDetails {
		return domain.LiveArchived
	}
	return domain.LiveNone
}

// ClassifyShortFromURL reads YouTube's own canonical URL for a video, which is
// authoritative: a /shorts/ path means the video is a Short.
func ClassifyShortFromURL(canonicalURL string) bool {
	return strings.Contains(canonicalURL, "/shorts/")
}

// ClassifyShortFromDuration guesses for videos outside the RSS window. The
// result is permanent for that row, since the window only moves forward.
func ClassifyShortFromDuration(d time.Duration) bool {
	return d > 0 && d <= ShortsMaxDuration
}

var isoDurationPattern = regexp.MustCompile(
	`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

// ParseISODuration reads the ISO 8601 duration the Data API returns, such as
// PT4M13S. Weeks, months and years are not emitted for videos and are rejected
// rather than silently misread.
func ParseISODuration(s string) (time.Duration, error) {
	m := isoDurationPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("not an ISO 8601 video duration: %q", s)
	}

	units := []time.Duration{24 * time.Hour, time.Hour, time.Minute, time.Second}
	var total time.Duration
	for i, unit := range units {
		if m[i+1] == "" {
			continue
		}
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return 0, fmt.Errorf("parsing %q in duration %q: %w", m[i+1], s, err)
		}
		total += time.Duration(n) * unit
	}
	return total, nil
}

// EmbedURL builds the player URL. Always youtube-nocookie.com, the embed-only
// host, so a network filter can block youtube.com on a child's device without
// breaking playback here. See docs/PLAN.md §3.
func EmbedURL(videoID string, autoplay bool) string {
	v := "https://www.youtube-nocookie.com/embed/" + videoID + "?rel=0&modestbranding=1"
	if autoplay {
		v += "&autoplay=1"
	}
	return v
}

// WatchURL builds the canonical youtube.com link, for sharing out and for the
// parent app's review deep link.
func WatchURL(videoID string) string {
	return "https://www.youtube.com/watch?v=" + videoID
}

// ChannelURL builds the canonical channel link the parent app opens for review.
func ChannelURL(channelID string) string {
	return "https://www.youtube.com/channel/" + channelID
}
