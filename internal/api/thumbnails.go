package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	_ "golang.org/x/image/webp"

	"github.com/nerdswhofish/coop/internal/youtube"
)

const (
	thumbnailTTL       = 7 * 24 * time.Hour
	maxThumbnailBytes  = 4 << 20
	maxThumbnailPixels = 2048 * 2048
	thumbnailTimeout   = 10 * time.Second
)

var errThumbnailRedirect = errors.New("thumbnail redirect rejected")

// Limit the combined buffers and decoded images, not just individual bodies.
var thumbnailSlots = make(chan struct{}, 4)

type thumbnailError struct {
	reason string
	status int
}

func (e *thumbnailError) Error() string {
	return fmt.Sprintf("thumbnail %s (status %d)", e.reason, e.status)
}

type thumbnailImage struct {
	body        []byte
	contentType string
}

func (s *Server) handleThumbnail(w http.ResponseWriter, r *http.Request) {
	videoID := r.PathValue("videoId")
	video, err := s.deps.Catalog.Video(r.Context(), videoID)
	if err != nil || !isYouTubeImageURL(video.ThumbnailURL) {
		http.NotFound(w, r)
		return
	}

	thumbnail, err := fetchThumbnail(r.Context(), thumbnailClient(nil), video.ThumbnailURL, videoID)
	if err != nil {
		s.deps.Logger.WarnContext(r.Context(), "thumbnail unavailable", "error", err)
		http.Error(w, "thumbnail unavailable", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", thumbnail.contentType)
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(thumbnailTTL.Seconds())))
	w.Header().Set("Content-Length", strconv.Itoa(len(thumbnail.body)))
	if _, err := w.Write(thumbnail.body); err != nil {
		s.deps.Logger.DebugContext(r.Context(), "thumbnail response interrupted")
	}
}

func thumbnailClient(transport http.RoundTripper) *http.Client {
	return &http.Client{
		Transport: transport,
		Timeout:   3 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 || !isYouTubeImageURL(req.URL.String()) {
				return errThumbnailRedirect
			}
			return nil
		},
	}
}

func fetchThumbnail(ctx context.Context, client *http.Client, rawURL, videoID string) (thumbnailImage, error) {
	ctx, cancel := context.WithTimeout(ctx, thumbnailTimeout)
	defer cancel()
	ctx, span := otel.Tracer("coop/api").Start(ctx, "coop.thumbnail.fetch", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	select {
	case thumbnailSlots <- struct{}{}:
		defer func() { <-thumbnailSlots }()
	case <-ctx.Done():
		span.RecordError(ctx.Err())
		span.SetStatus(codes.Error, "thumbnail queue timeout")
		return thumbnailImage{}, ctx.Err()
	}

	var lastErr error
	for index, candidate := range thumbnailCandidates(rawURL, videoID) {
		thumbnail, err := fetchThumbnailCandidate(ctx, client, candidate)
		if err == nil {
			span.SetAttributes(attribute.Bool("coop.thumbnail.fallback", index > 0))
			return thumbnail, nil
		}
		lastErr = err
		var failure *thumbnailError
		if !errors.As(err, &failure) || (failure.status != http.StatusNotFound && failure.status != http.StatusGone) {
			break
		}
	}
	span.RecordError(lastErr)
	span.SetStatus(codes.Error, "thumbnail unavailable")
	return thumbnailImage{}, lastErr
}

func fetchThumbnailCandidate(ctx context.Context, client *http.Client, rawURL string) (thumbnailImage, error) {
	if !isYouTubeImageURL(rawURL) {
		return thumbnailImage{}, &thumbnailError{reason: "url_rejected"}
	}
	return backoff.Retry(ctx, func() (thumbnailImage, error) {
		if ctx.Err() != nil {
			return thumbnailImage{}, backoff.Permanent(ctx.Err())
		}
		thumbnail, retry, err := requestThumbnail(ctx, client, rawURL)
		if err != nil && !retry {
			err = backoff.Permanent(err)
		}
		return thumbnail, err
	}, backoff.WithMaxTries(3), backoff.WithBackOff(backoff.NewConstantBackOff(100*time.Millisecond)),
		backoff.WithNotify(func(err error, _ time.Duration) {
			trace.SpanFromContext(ctx).AddEvent("coop.thumbnail.retry", trace.WithAttributes(
				attribute.String("coop.thumbnail.failure", err.Error()),
			))
		}))
}

func requestThumbnail(ctx context.Context, client *http.Client, rawURL string) (thumbnailImage, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return thumbnailImage{}, false, &thumbnailError{reason: "url_rejected"}
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return thumbnailImage{}, false, ctx.Err()
		}
		if errors.Is(err, errThumbnailRedirect) {
			return thumbnailImage{}, false, &thumbnailError{reason: "redirect_rejected"}
		}
		// net/http errors contain the URL, which can identify a child's video.
		return thumbnailImage{}, true, &thumbnailError{reason: "transport"}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		retry := resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		failure := &thumbnailError{reason: "upstream_status", status: resp.StatusCode}
		if retry {
			if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds >= 0 {
				delay := time.Duration(min(seconds, int(thumbnailTimeout.Seconds()))) * time.Second
				return thumbnailImage{}, true, errors.Join(failure, &backoff.RetryAfterError{Duration: delay})
			}
			if until, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil && time.Until(until) > 0 {
				return thumbnailImage{}, true, errors.Join(failure, &backoff.RetryAfterError{Duration: min(time.Until(until), thumbnailTimeout)})
			}
		}
		return thumbnailImage{}, retry, failure
	}
	if resp.ContentLength > maxThumbnailBytes {
		return thumbnailImage{}, false, &thumbnailError{reason: "too_large"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxThumbnailBytes+1))
	if err != nil {
		return thumbnailImage{}, true, &thumbnailError{reason: "body_read"}
	}
	if len(body) > maxThumbnailBytes {
		return thumbnailImage{}, false, &thumbnailError{reason: "too_large"}
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return thumbnailImage{}, !errors.Is(err, image.ErrFormat), &thumbnailError{reason: "invalid_image"}
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxThumbnailPixels/config.Height {
		return thumbnailImage{}, false, &thumbnailError{reason: "invalid_image"}
	}
	if _, _, err := image.Decode(bytes.NewReader(body)); err != nil {
		return thumbnailImage{}, true, &thumbnailError{reason: "invalid_image"}
	}
	return thumbnailImage{body: body, contentType: "image/" + format}, false, nil
}

func thumbnailCandidates(rawURL, videoID string) []string {
	candidates := []string{rawURL}
	if !youtube.ValidVideoID(videoID) || !isYouTubeImageURL(rawURL) {
		return candidates
	}
	u, _ := url.Parse(rawURL)
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if (u.Host != "i.ytimg.com" && u.Host != "i9.ytimg.com") || len(parts) != 3 || parts[0] != "vi" || parts[1] != videoID {
		return candidates
	}
	sizes := []string{"maxresdefault.jpg", "sddefault.jpg", "hqdefault.jpg", "mqdefault.jpg", "default.jpg"}
	for index, size := range sizes {
		if parts[2] != size {
			continue
		}
		for _, fallback := range sizes[index+1:] {
			u.Path = "/vi/" + videoID + "/" + fallback
			u.RawQuery = ""
			candidates = append(candidates, u.String())
		}
		break
	}
	return candidates
}

func isYouTubeImageURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return false
	}
	switch u.Host {
	case "i.ytimg.com", "i9.ytimg.com", "yt3.ggpht.com", "yt3.googleusercontent.com":
		return true
	default:
		return false
	}
}
