package api

import (
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/nerdswhofish/coop/internal/auth"
	"github.com/nerdswhofish/coop/internal/store"
)

func (s *Server) clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	immediate, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	if !s.trustedProxy(immediate) {
		return immediate.String()
	}

	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			continue
		}
		if !s.trustedProxy(candidate) {
			return candidate.String()
		}
	}
	return immediate.String()
}

func (s *Server) trustedProxy(address netip.Addr) bool {
	for _, prefix := range s.trustedProxyPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

// parentHandler runs with an authenticated parent and their scope resolved.
type parentHandler func(http.ResponseWriter, *http.Request, auth.Parent) error

// childHandler runs with an authenticated child device.
type childHandler func(http.ResponseWriter, *http.Request, auth.Child) error

// Headers a client uses to report what it is running, so a parent can see
// which devices are on which build.
const (
	clientBuildHeader   = "X-Coop-Client-Build"
	clientVersionHeader = "X-Coop-Client-Version"
)

// clientAppPattern bounds an attacker-controlled string on its way to storage
// and back out to another parent's screen.
var clientAppPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,31}$`)

func clientApp(r *http.Request) store.ClientApp {
	client := store.ClientApp{
		Build:   strings.TrimSpace(r.Header.Get(clientBuildHeader)),
		Version: strings.TrimSpace(r.Header.Get(clientVersionHeader)),
	}
	if !clientAppPattern.MatchString(client.Build) {
		return store.ClientApp{}
	}
	if !clientAppPattern.MatchString(client.Version) {
		client.Version = ""
	}
	return client
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/install/") {
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		} else {
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-src https://www.youtube-nocookie.com; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		}
		next.ServeHTTP(w, r)
	})
}

// withParent authenticates a parent session and resolves their scope here
// rather than per handler, so no handler can serve a request having forgotten
// to ask which children the caller may see.
func (s *Server) withParent(h parentHandler) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		token := auth.BearerToken(r.Header.Get("Authorization"))
		if token == "" {
			return unauthorized()
		}

		session, parent, err := s.deps.Accounts.SessionByToken(r.Context(), auth.HashToken(token))
		if err != nil {
			return unauthorized()
		}

		principal := auth.Parent{
			ID:        parent.ID,
			FamilyID:  parent.FamilyID,
			Role:      parent.Role,
			SessionID: session.ID,
		}
		if !principal.IsAdmin() {
			scoped, err := s.deps.Accounts.ScopedChildIDs(r.Context(), parent.ID)
			if err != nil {
				return err
			}
			principal.ScopedChildIDs = scoped
		}

		// Best effort: a failure to record last-seen must not fail the request.
		if err := s.deps.Accounts.TouchSession(r.Context(), session.ID, clientApp(r)); err != nil {
			s.deps.Logger.Debug("touching session", "error", err)
		}

		return h(w, r, principal)
	}
}

// withChild authenticates a paired device token.
func (s *Server) withChild(h childHandler) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		token := auth.BearerToken(r.Header.Get("Authorization"))
		fromCookie := false
		if token == "" {
			cookie, err := r.Cookie(webSessionCookie)
			if err != nil || cookie.Value == "" {
				return unauthorized()
			}
			token = cookie.Value
			fromCookie = true
		}
		if fromCookie && r.Method != http.MethodGet && r.Method != http.MethodHead &&
			r.Method != http.MethodOptions && !sameOrigin(r.Header.Get("Origin"), s.deps.Config.Server.PublicURL) {
			return forbidden("cross-origin browser request refused")
		}

		device, child, err := s.deps.Accounts.DeviceByToken(r.Context(), auth.HashToken(token))
		if err != nil {
			return unauthorized()
		}

		if err := s.deps.Accounts.TouchDevice(r.Context(), device.ID, clientApp(r)); err != nil {
			s.deps.Logger.Debug("touching device", "error", err)
		}

		return h(w, r, auth.Child{
			ID:       child.ID,
			FamilyID: child.FamilyID,
			DeviceID: device.ID,
		})
	}
}

func sameOrigin(candidate, configured string) bool {
	left, leftErr := url.Parse(candidate)
	right, rightErr := url.Parse(configured)
	if leftErr != nil || rightErr != nil || left.Scheme == "" || left.Host == "" {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

// statusRecorder captures the status code for logging, since ResponseWriter
// does not expose what was written.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.deps.Now()
		recorder := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(recorder, r)

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}

		level := slog.LevelInfo
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		}

		s.deps.Logger.Log(r.Context(), level, "request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"bytes", recorder.bytes,
			"duration", time.Since(start).String(),
		)
	})
}

// recoverPanics turns a panic into a 500 rather than a dropped connection, and
// keeps the stack in the log rather than in the response.
func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			s.deps.Logger.Error("panic serving request",
				"method", r.Method,
				"path", r.URL.Path,
				"panic", recovered,
				"stack", string(debug.Stack()),
			)
			writeError(w, r, s.deps.Logger, internal(nil))
		}()

		next.ServeHTTP(w, r)
	})
}
