package api

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/nerdswhofish/coop/internal/auth"
)

// parentHandler runs with an authenticated parent and their scope resolved.
type parentHandler func(http.ResponseWriter, *http.Request, auth.Parent) error

// childHandler runs with an authenticated child device.
type childHandler func(http.ResponseWriter, *http.Request, auth.Child) error

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
			ID:       parent.ID,
			FamilyID: parent.FamilyID,
			Role:     parent.Role,
		}
		if !principal.IsAdmin() {
			scoped, err := s.deps.Accounts.ScopedChildIDs(r.Context(), parent.ID)
			if err != nil {
				return err
			}
			principal.ScopedChildIDs = scoped
		}

		// Best effort: a failure to record last-seen must not fail the request.
		if err := s.deps.Accounts.TouchSession(r.Context(), session.ID); err != nil {
			s.deps.Logger.Debug("touching session", "error", err)
		}

		return h(w, r, principal)
	}
}

// withChild authenticates a paired device token.
func (s *Server) withChild(h childHandler) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		token := auth.BearerToken(r.Header.Get("Authorization"))
		if token == "" {
			return unauthorized()
		}

		device, child, err := s.deps.Accounts.DeviceByToken(r.Context(), auth.HashToken(token))
		if err != nil {
			return unauthorized()
		}

		if err := s.deps.Accounts.TouchDevice(r.Context(), device.ID); err != nil {
			s.deps.Logger.Debug("touching device", "error", err)
		}

		return h(w, r, auth.Child{
			ID:       child.ID,
			FamilyID: child.FamilyID,
			DeviceID: device.ID,
		})
	}
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
