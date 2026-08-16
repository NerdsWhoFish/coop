// Package api serves Coop's HTTP surface: a parent side behind a session, and
// a read-mostly child side behind a paired device token.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/config"
	"github.com/nerdswhofish/coop/internal/crypto"
	"github.com/nerdswhofish/coop/internal/feed"
	"github.com/nerdswhofish/coop/internal/store"
	"github.com/nerdswhofish/coop/internal/version"
	"github.com/nerdswhofish/coop/internal/youtube"
	"github.com/nerdswhofish/coop/internal/youtubeclient"
)

// Deps is everything the HTTP layer needs. Assembled by the composition root
// so this package constructs nothing it does not own.
type Deps struct {
	Config   *config.Config
	Logger   *slog.Logger
	Accounts *store.Accounts
	Rules    *store.Rules
	Catalog  *store.Catalog
	Activity *store.Activity
	Feed     *feed.Service
	Quota    *store.QuotaStore
	Sealer   *crypto.Sealer
	YouTube  *youtubeclient.Factory
	DB       *store.DB
	Now      func() time.Time
}

// Server routes and serves the API.
type Server struct {
	deps Deps
	mux  *http.ServeMux
}

// NewServer wires the routes.
func NewServer(deps Deps) (*Server, error) {
	if deps.Config == nil {
		return nil, errors.New("api: config is required")
	}
	if deps.YouTube == nil {
		return nil, errors.New("api: YouTube client factory is required")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}

	s := &Server{deps: deps, mux: http.NewServeMux()}
	s.routes()
	return s, nil
}

// Handler returns the fully wrapped handler.
func (s *Server) Handler() http.Handler {
	return s.recoverPanics(s.logRequests(s.mux))
}

func (s *Server) routes() {
	m := s.mux

	m.HandleFunc("GET /healthz", s.handleHealth)
	m.HandleFunc("GET /api/v1/healthz", s.handleHealth)
	m.HandleFunc("GET /version", s.handleVersion)

	// Setup and sign-in are the only unauthenticated write paths.
	m.HandleFunc("GET /api/v1/setup", s.handle(s.handleSetupStatus))
	m.HandleFunc("POST /api/v1/setup", s.handle(s.handleSetup))
	m.HandleFunc("POST /api/v1/parent/auth/login", s.handle(s.handleLogin))
	m.HandleFunc("POST /api/v1/parent/auth/invitation", s.handle(s.handleAcceptParentInvitation))

	parent := func(pattern string, h parentHandler) {
		m.HandleFunc(pattern, s.handle(s.withParent(h)))
	}
	child := func(pattern string, h childHandler) {
		m.HandleFunc(pattern, s.handle(s.withChild(h)))
	}

	parent("GET /api/v1/parent/me", s.handleParentMe)

	parent("GET /api/v1/parent/family", s.handleGetFamily)
	parent("PATCH /api/v1/parent/family", s.handleUpdateFamily)
	parent("PUT /api/v1/parent/family/api-key", s.handleSetAPIKey)
	parent("GET /api/v1/parent/family/quota", s.handleQuota)

	parent("GET /api/v1/parent/parents", s.handleListParents)
	parent("POST /api/v1/parent/parents/invite", s.handleInviteParent)
	parent("DELETE /api/v1/parent/parents/{parentId}", s.handleDeleteParent)
	parent("PUT /api/v1/parent/parents/{parentId}/scope", s.handleSetScope)

	parent("GET /api/v1/parent/children", s.handleListChildren)
	parent("POST /api/v1/parent/children", s.handleCreateChild)
	parent("GET /api/v1/parent/children/{childId}", s.handleGetChild)
	parent("PATCH /api/v1/parent/children/{childId}", s.handleUpdateChild)
	parent("DELETE /api/v1/parent/children/{childId}", s.handleDeleteChild)

	parent("POST /api/v1/parent/children/{childId}/pairing-code", s.handleCreatePairingCode)
	parent("GET /api/v1/parent/children/{childId}/devices", s.handleListDevices)
	parent("DELETE /api/v1/parent/devices/{deviceId}", s.handleRevokeDevice)

	parent("GET /api/v1/parent/allowlist/global", s.handleGlobalAllowlist)
	parent("PUT /api/v1/parent/allowlist/global", s.handleAllowGlobally)
	parent("DELETE /api/v1/parent/allowlist/global/{channelId}", s.handleDisallowGlobally)

	parent("GET /api/v1/parent/children/{childId}/allowlist", s.handleChildAllowlist)
	parent("PUT /api/v1/parent/children/{childId}/allowlist", s.handleAllowForChild)
	parent("DELETE /api/v1/parent/children/{childId}/allowlist/{channelId}", s.handleDisallowForChild)
	parent("PUT /api/v1/parent/children/{childId}/denylist/{channelId}", s.handleDenyForChild)
	parent("DELETE /api/v1/parent/children/{childId}/denylist/{channelId}", s.handleUndenyForChild)

	parent("GET /api/v1/parent/blocklist", s.handleBlocklist)
	parent("PUT /api/v1/parent/blocklist", s.handleBlockChannel)
	parent("DELETE /api/v1/parent/blocklist/{channelId}", s.handleUnblockChannel)

	parent("GET /api/v1/parent/keywords", s.handleListKeywords)
	parent("POST /api/v1/parent/keywords", s.handleCreateKeyword)
	parent("PATCH /api/v1/parent/keywords/{keywordId}", s.handleUpdateKeyword)
	parent("DELETE /api/v1/parent/keywords/{keywordId}", s.handleDeleteKeyword)

	parent("GET /api/v1/parent/requests", s.handleListRequests)
	parent("POST /api/v1/parent/requests/{requestId}/approve", s.handleApproveRequest)
	parent("POST /api/v1/parent/requests/{requestId}/deny", s.handleDenyRequest)

	parent("GET /api/v1/parent/children/{childId}/suppressions", s.handleListSuppressions)
	parent("POST /api/v1/parent/suppressions/{suppressionId}/override", s.handleOverrideSuppression)

	parent("GET /api/v1/parent/search/channels", s.handleParentSearchChannels)

	m.HandleFunc("POST /api/v1/child/pair", s.handle(s.handlePair))
	child("GET /api/v1/child/me", s.handleChildMe)
	child("GET /api/v1/child/feed", s.handleChildFeed)
	child("GET /api/v1/child/shorts", s.handleChildShorts)
	child("GET /api/v1/child/subscriptions", s.handleChildSubscriptions)
	child("PUT /api/v1/child/subscriptions/{channelId}", s.handleSubscribe)
	child("DELETE /api/v1/child/subscriptions/{channelId}", s.handleUnsubscribe)
	child("GET /api/v1/child/channels/{channelId}", s.handleChildChannel)
	child("GET /api/v1/child/search", s.handleChildSearch)
	child("GET /api/v1/child/videos/{videoId}", s.handleWatch)
	child("PUT /api/v1/child/videos/{videoId}/reaction", s.handleSetReaction)
	child("DELETE /api/v1/child/videos/{videoId}/reaction", s.handleClearReaction)
	child("POST /api/v1/child/videos/{videoId}/watch", s.handleRecordWatch)
	child("GET /api/v1/child/requests", s.handleChildRequests)
	child("POST /api/v1/child/requests", s.handleRaiseRequest)

	// Thumbnails are proxied so a child device never talks to a Google host
	// for images, and so one fewer domain has to be allowed by a filter.
	m.HandleFunc("GET /api/v1/thumb/{videoId}", s.handleThumbnail)
}

// handlerFunc is a handler that may fail, so the error path is written once.
type handlerFunc func(http.ResponseWriter, *http.Request) error

func (s *Server) handle(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			writeError(w, r, s.deps.Logger, err)
		}
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.DB.SQL().PingContext(r.Context()); err != nil {
		http.Error(w, "database unreachable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.deps.Logger, http.StatusOK, map[string]string{
		"version": version.Version,
		"commit":  version.Commit,
		"built":   version.Date,
	})
}

// youtubeFor builds a client bound to one family's API key. Built per request
// rather than cached, since a stale client would keep spending against a key
// the parent has already replaced.
func (s *Server) youtubeFor(ctx context.Context, familyID uuid.UUID) (*youtube.Client, error) {
	client, err := s.deps.YouTube.ForFamily(ctx, familyID)
	if errors.Is(err, youtubeclient.ErrNoAPIKey) {
		return nil, conflict("no_api_key", "this family has no YouTube API key configured")
	}
	if err != nil {
		return nil, internal(err)
	}
	return client, nil
}
