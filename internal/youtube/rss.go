package youtube

import (
	"encoding/xml"
	"fmt"
	"io"
	"time"
)

// FeedURL is a channel's public Atom feed of recent uploads. It costs no API
// quota, and doubles as the WebSub topic URL, so moving from polling to push
// later needs no new integration. See docs/PLAN.md §8.
func FeedURL(channelID string) string {
	return "https://www.youtube.com/feeds/videos.xml?channel_id=" + channelID
}

// FeedWindow is roughly how many entries a channel feed carries. The window
// only moves forward, so anything older is never visible here again.
const FeedWindow = 15

// FeedEntry is one upload as the channel feed describes it.
type FeedEntry struct {
	VideoID      string
	ChannelID    string
	Title        string
	Published    time.Time
	CanonicalURL string

	// IsShort comes from the canonical URL carrying a /shorts/ path, which is
	// YouTube stating the video's own form rather than a duration guess.
	IsShort bool
}

type feedDocument struct {
	XMLName xml.Name      `xml:"feed"`
	Entries []feedRawItem `xml:"entry"`
}

type feedRawItem struct {
	VideoID   string     `xml:"videoId"`
	ChannelID string     `xml:"channelId"`
	Title     string     `xml:"title"`
	Published string     `xml:"published"`
	Links     []feedLink `xml:"link"`
}

type feedLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

// ParseFeed reads a channel's Atom feed. Entries without a video ID are
// skipped rather than failing the feed: one malformed entry should not cost a
// channel its whole refresh.
func ParseFeed(r io.Reader) ([]FeedEntry, error) {
	var doc feedDocument
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decoding channel feed: %w", err)
	}

	entries := make([]FeedEntry, 0, len(doc.Entries))
	for _, raw := range doc.Entries {
		if raw.VideoID == "" {
			continue
		}

		entry := FeedEntry{
			VideoID:      raw.VideoID,
			ChannelID:    raw.ChannelID,
			Title:        raw.Title,
			CanonicalURL: alternateHref(raw.Links),
		}
		entry.IsShort = ClassifyShortFromURL(entry.CanonicalURL)

		if raw.Published != "" {
			// A bad timestamp is not worth discarding an entry over; the zero
			// time sorts oldest, which is the harmless direction.
			if t, err := time.Parse(time.RFC3339, raw.Published); err == nil {
				entry.Published = t
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func alternateHref(links []feedLink) string {
	for _, l := range links {
		if l.Rel == "alternate" {
			return l.Href
		}
	}
	return ""
}
