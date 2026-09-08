package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type thumbnailTransport func(*http.Request) (*http.Response, error)

func (f thumbnailTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func thumbnailJPEG(t *testing.T) []byte {
	t.Helper()
	var body bytes.Buffer
	if err := jpeg.Encode(&body, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func thumbnailResponse(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"image/jpeg"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

const testThumbnailURL = "https://i.ytimg.com/vi/abcdefghijk/maxresdefault.jpg"

func TestThumbnailRecoversTransientFailures(t *testing.T) {
	for _, failure := range []string{"transport", "body", "truncated_image", "502", "429"} {
		t.Run(failure, func(t *testing.T) {
			body := thumbnailJPEG(t)
			calls := 0
			client := thumbnailClient(thumbnailTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					switch failure {
					case "transport":
						return nil, errors.New("temporary connection failure")
					case "body":
						resp := thumbnailResponse(200, nil)
						resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body[:20]), thumbnailBrokenReader{}))
						return resp, nil
					case "truncated_image":
						return thumbnailResponse(200, body[:len(body)-30]), nil
					case "502":
						return thumbnailResponse(502, nil), nil
					case "429":
						return thumbnailResponse(429, nil), nil
					}
				}
				return thumbnailResponse(200, body), nil
			}))
			got, err := fetchThumbnail(t.Context(), client, testThumbnailURL, "abcdefghijk")
			if err != nil || calls != 2 || !bytes.Equal(got.body, body) || got.contentType != "image/jpeg" {
				t.Fatalf("calls=%d type=%q err=%v", calls, got.contentType, err)
			}
		})
	}
}

type thumbnailBrokenReader struct{}

func (thumbnailBrokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestThumbnailFallsBackOnlyForMissingResolution(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone, http.StatusForbidden, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			body := thumbnailJPEG(t)
			var paths []string
			client := thumbnailClient(thumbnailTransport(func(r *http.Request) (*http.Response, error) {
				paths = append(paths, r.URL.Path)
				if strings.HasSuffix(r.URL.Path, "sddefault.jpg") {
					return thumbnailResponse(200, body), nil
				}
				return thumbnailResponse(status, nil), nil
			}))
			got, err := fetchThumbnail(t.Context(), client, testThumbnailURL, "abcdefghijk")
			if status == http.StatusNotFound || status == http.StatusGone {
				if err != nil || len(paths) != 2 || !bytes.Equal(got.body, body) {
					t.Fatalf("paths=%v err=%v", paths, err)
				}
			} else {
				if err == nil || len(got.body) != 0 {
					t.Fatalf("persistent failure returned image: err=%v", err)
				}
				for _, p := range paths {
					if !strings.HasSuffix(p, "maxresdefault.jpg") {
						t.Fatalf("unexpected fallback: %v", paths)
					}
				}
				want := 1
				if status == http.StatusBadGateway {
					want = 3
				}
				if len(paths) != want {
					t.Fatalf("requests=%d want=%d", len(paths), want)
				}
			}
		})
	}
}

func TestThumbnailRejectsInvalidBodies(t *testing.T) {
	largeDimensions := thumbnailJPEG(t)
	sof := bytes.Index(largeDimensions, []byte{0xff, 0xc0})
	if sof < 0 {
		t.Fatal("JPEG has no baseline frame header")
	}
	binary.BigEndian.PutUint16(largeDimensions[sof+5:sof+7], 65535)
	binary.BigEndian.PutUint16(largeDimensions[sof+7:sof+9], 65535)
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"oversized", bytes.Repeat([]byte("a"), maxThumbnailBytes+1)},
		{"html_disguised_as_image", []byte("<html>upstream unavailable</html>")},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)},
		{"empty", nil},
		{"excessive_dimensions", largeDimensions},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := thumbnailClient(thumbnailTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				return thumbnailResponse(200, tc.body), nil
			}))
			got, err := fetchThumbnail(t.Context(), client, testThumbnailURL, "abcdefghijk")
			if err == nil || len(got.body) != 0 || calls != 1 {
				t.Fatalf("invalid body accepted or retried: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestThumbnailRejectsUnsafeRedirects(t *testing.T) {
	for _, target := range []string{
		"https://example.com/image.jpg",
		"http://i.ytimg.com/image.jpg",
		"https://i.ytimg.com:444/image.jpg",
		"https://user@i.ytimg.com/image.jpg",
		"https://i.ytimg.com.evil.example/image.jpg",
	} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			client := thumbnailClient(thumbnailTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				resp := thumbnailResponse(302, nil)
				resp.Header.Set("Location", target)
				return resp, nil
			}))
			_, err := fetchThumbnail(t.Context(), client, testThumbnailURL, "abcdefghijk")
			if err == nil || calls != 1 || strings.Contains(err.Error(), target) {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestThumbnailAllowsSafeRedirectAndCapsLoops(t *testing.T) {
	for _, loop := range []bool{false, true} {
		body := thumbnailJPEG(t)
		calls := 0
		client := thumbnailClient(thumbnailTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 || loop {
				resp := thumbnailResponse(302, nil)
				resp.Header.Set("Location", "https://i9.ytimg.com/vi/abcdefghijk/maxresdefault.jpg")
				return resp, nil
			}
			return thumbnailResponse(200, body), nil
		}))
		got, err := fetchThumbnail(t.Context(), client, testThumbnailURL, "abcdefghijk")
		if loop {
			if err == nil || calls != 3 {
				t.Fatalf("loop calls=%d err=%v", calls, err)
			}
		} else if err != nil || calls != 2 || !bytes.Equal(got.body, body) {
			t.Fatalf("safe redirect calls=%d err=%v", calls, err)
		}
	}
}

func TestThumbnailCancellationStopsWork(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	client := thumbnailClient(thumbnailTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		cancel()
		return thumbnailResponse(503, nil), nil
	}))
	_, err := fetchThumbnail(ctx, client, testThumbnailURL, "abcdefghijk")
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}

	ctx, cancel = context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	client = thumbnailClient(thumbnailTransport(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	}))
	_, err = fetchThumbnail(ctx, client, testThumbnailURL, "abcdefghijk")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline not propagated: %v", err)
	}
}

func TestThumbnailDoesNotInventFallbackURLs(t *testing.T) {
	for _, rawURL := range []string{
		"https://yt3.ggpht.com/vi/abcdefghijk/maxresdefault.jpg",
		"https://i.ytimg.com/vi/anotherid12/maxresdefault.jpg",
		"https://i.ytimg.com/vi/abcdefghijk/custom.jpg",
		"https://i.ytimg.com/vi/abcdefghijk/default.jpg",
	} {
		if got := thumbnailCandidates(rawURL, "abcdefghijk"); len(got) != 1 || got[0] != rawURL {
			t.Fatalf("unexpected fallback for %q: %v", rawURL, got)
		}
	}
}

func TestThumbnailHonorsRetryAfter(t *testing.T) {
	for _, value := range []string{"60", time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)} {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		calls := 0
		client := thumbnailClient(thumbnailTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			resp := thumbnailResponse(429, nil)
			resp.Header.Set("Retry-After", value)
			return resp, nil
		}))
		_, err := fetchThumbnail(ctx, client, testThumbnailURL, "abcdefghijk")
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
			t.Fatalf("Retry-After=%q calls=%d err=%v", value, calls, err)
		}
	}
}

func TestThumbnailRejectsUnsafeInitialURL(t *testing.T) {
	calls := 0
	client := thumbnailClient(thumbnailTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return thumbnailResponse(200, thumbnailJPEG(t)), nil
	}))
	_, err := fetchThumbnail(t.Context(), client, "https://example.com/private.jpg?secret=hidden", "abcdefghijk")
	if err == nil || calls != 0 || strings.Contains(err.Error(), "hidden") || strings.Contains(err.Error(), "example.com") {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestThumbnailSupportsWebP(t *testing.T) {
	body, err := base64.StdEncoding.DecodeString("UklGRjIAAABXRUJQVlA4ICYAAACQAQCdASoBAAEAAgA0JZACdLoAA5gA/ulpH0Jse1Ff2pgkjLwAAA==")
	if err != nil {
		t.Fatal(err)
	}
	client := thumbnailClient(thumbnailTransport(func(r *http.Request) (*http.Response, error) {
		resp := thumbnailResponse(200, body)
		resp.Header.Set("Content-Type", "image/webp")
		return resp, nil
	}))
	got, err := fetchThumbnail(t.Context(), client, testThumbnailURL, "abcdefghijk")
	if err != nil || got.contentType != "image/webp" || !bytes.Equal(got.body, body) {
		t.Fatalf("type=%q err=%v", got.contentType, err)
	}
}

func TestThumbnailBoundsConcurrentBuffers(t *testing.T) {
	body := thumbnailJPEG(t)
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	results := make(chan error, 4)
	var calls atomic.Int32
	client := thumbnailClient(thumbnailTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			return thumbnailResponse(200, body), nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}))
	for range 4 {
		go func() {
			_, err := fetchThumbnail(t.Context(), client, testThumbnailURL, "abcdefghijk")
			results <- err
		}()
	}
	for range 4 {
		<-started
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	_, err := fetchThumbnail(ctx, client, testThumbnailURL, "abcdefghijk")
	cancel()
	close(release)
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 4 {
		t.Errorf("queued request allocated buffers: calls=%d err=%v", calls.Load(), err)
	}
	for range 4 {
		if err := <-results; err != nil {
			t.Errorf("active request failed: %v", err)
		}
	}
}
