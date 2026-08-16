package api

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// thumbnailTTL is how long a client may cache a thumbnail. They never change
// for a given video, so this is generous on purpose.
const thumbnailTTL = 7 * 24 * time.Hour

// maxThumbnailBytes bounds what will be proxied, so a surprise upstream
// response cannot be streamed straight through unbounded.
const maxThumbnailBytes = 4 << 20

// handleThumbnail proxies a video's thumbnail. Images are the one thing worth
// relaying: static, no player functionality, and it removes a Google host a
// child device would otherwise contact. See docs/PLAN.md §3.
func (s *Server) handleThumbnail(w http.ResponseWriter, r *http.Request) {
	videoID := r.PathValue("videoId")

	video, err := s.deps.Catalog.Video(r.Context(), videoID)
	if err != nil || video.ThumbnailURL == "" {
		http.NotFound(w, r)
		return
	}

	// Only ever fetch from YouTube's image host, so a poisoned catalog row
	// cannot turn this endpoint into an open proxy.
	if !isYouTubeImageURL(video.ThumbnailURL) {
		http.NotFound(w, r)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, video.ThumbnailURL, nil)
	if err != nil {
		http.Error(w, "bad thumbnail url", http.StatusBadGateway)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "thumbnail unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "thumbnail unavailable", http.StatusBadGateway)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		http.Error(w, "thumbnail unavailable", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control",
		"public, max-age="+strconv.Itoa(int(thumbnailTTL.Seconds())))
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, io.LimitReader(resp.Body, maxThumbnailBytes)); err != nil {
		s.deps.Logger.Debug("streaming thumbnail", "video", videoID, "error", err)
	}
}

// isYouTubeImageURL allowlists the hosts YouTube serves thumbnails from.
func isYouTubeImageURL(raw string) bool {
	for _, prefix := range []string{
		"https://i.ytimg.com/",
		"https://i9.ytimg.com/",
		"https://yt3.ggpht.com/",
		"https://yt3.googleusercontent.com/",
	} {
		if strings.HasPrefix(raw, prefix) {
			return true
		}
	}
	return false
}
