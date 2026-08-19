// Package api provides HTTP API server functionality.
package api

import (
	"context"
	"io/fs"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/vrclog/vrclog-companion/internal/app"
	"github.com/vrclog/vrclog-companion/internal/sse"
)

// Server represents the HTTP API server.
type Server struct {
	httpServer *http.Server
	mux        *http.ServeMux

	// Use case dependencies
	health       app.HealthUsecase
	observations app.ObservationsUsecase
	state        app.StateUsecase
	media        app.MediaUsecase
	adapters     app.AdaptersUsecase
	cfg          app.ConfigUsecase
	stats        app.StatsUsecase

	// SSE
	broadcaster       *sse.Broadcaster
	observationsStore SSEObservationStore

	// Auth configuration
	authEnabled  bool
	authUsername string
	authPassword string

	// SSE token configuration
	sseSecret []byte

	// Web UI filesystem
	webFS fs.FS

	// Rate limiter (LAN mode only)
	rateLimiter *RateLimiter

	// Auth failure limiter for brute-force protection (LAN mode only)
	authFailureLimiter *AuthFailureLimiter

	// CORS configuration
	corsConfig *CORSConfig

	// CSRF allowed hosts (derived from server address)
	csrfAllowedHosts []string

	// ready gates every route except /api/v1/health while the startup
	// Projector rebuild is in progress: requests get 503 instead of
	// touching not-yet-consistent state.
	ready atomic.Bool
}

// SetReady marks the server ready (or not) to serve routes other than
// /api/v1/health. Call SetReady(true) once startup Projector rebuild
// completes. Defaults to not-ready.
func (s *Server) SetReady(ready bool) {
	s.ready.Store(ready)
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithObservationsUsecase sets the observations use case.
func WithObservationsUsecase(observations app.ObservationsUsecase) ServerOption {
	return func(s *Server) { s.observations = observations }
}

// WithStateUsecase sets the state use case.
func WithStateUsecase(state app.StateUsecase) ServerOption {
	return func(s *Server) { s.state = state }
}

// WithMediaUsecase sets the media use case.
func WithMediaUsecase(media app.MediaUsecase) ServerOption {
	return func(s *Server) { s.media = media }
}

// WithAdaptersUsecase sets the adapters use case.
func WithAdaptersUsecase(adapters app.AdaptersUsecase) ServerOption {
	return func(s *Server) { s.adapters = adapters }
}

// WithConfigUsecase sets the config use case.
func WithConfigUsecase(cfg app.ConfigUsecase) ServerOption {
	return func(s *Server) { s.cfg = cfg }
}

// WithStatsUsecase sets the stats use case.
func WithStatsUsecase(stats app.StatsUsecase) ServerOption {
	return func(s *Server) { s.stats = stats }
}

// WithBroadcaster sets the SSE broadcaster and its backing Observation
// store (for Last-Event-ID resolution and backlog replay).
func WithBroadcaster(b *sse.Broadcaster, store SSEObservationStore) ServerOption {
	return func(s *Server) {
		s.broadcaster = b
		s.observationsStore = store
	}
}

// WithBasicAuth enables HTTP Basic Auth.
func WithBasicAuth(username, password string) ServerOption {
	return func(s *Server) {
		if username != "" && password != "" {
			s.authEnabled = true
			s.authUsername = username
			s.authPassword = password
		}
	}
}

// WithSSESecret sets the secret for SSE token signing.
func WithSSESecret(secret []byte) ServerOption {
	return func(s *Server) { s.sseSecret = secret }
}

// WithWebFS sets the embedded web filesystem for static file serving.
func WithWebFS(webFS fs.FS) ServerOption {
	return func(s *Server) { s.webFS = webFS }
}

// WithRateLimiter enables rate limiting (recommended for LAN mode).
func WithRateLimiter(rl *RateLimiter) ServerOption {
	return func(s *Server) { s.rateLimiter = rl }
}

// WithAuthFailureLimiter enables brute-force protection (recommended for LAN mode).
func WithAuthFailureLimiter(afl *AuthFailureLimiter) ServerOption {
	return func(s *Server) { s.authFailureLimiter = afl }
}

// WithCORS enables CORS with the specified configuration.
func WithCORS(cfg CORSConfig) ServerOption {
	return func(s *Server) { s.corsConfig = &cfg }
}

// WithCSRFAllowedHosts sets the allowed hosts for CSRF validation.
func WithCSRFAllowedHosts(hosts []string) ServerOption {
	return func(s *Server) { s.csrfAllowedHosts = hosts }
}

// NewServer creates a new API server with the given dependencies.
func NewServer(addr string, health app.HealthUsecase, opts ...ServerOption) *Server {
	mux := http.NewServeMux()
	s := &Server{
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           nil,              // Set after options are applied
			ReadHeaderTimeout: 5 * time.Second,  // Slowloris protection
			ReadTimeout:       10 * time.Second, // Total body read timeout
			WriteTimeout:      0,                // Disable for SSE (long-lived connections)
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    1 << 14, // 16KB - limit header size to prevent DoS
		},
		mux:    mux,
		health: health,
	}
	s.ready.Store(true) // callers that manage startup rebuild call SetReady(false) explicitly
	for _, opt := range opts {
		opt(s)
	}
	s.registerRoutes()

	// Build middleware chain: security headers -> CORS -> CSRF -> mux
	var handler http.Handler = mux

	if len(s.csrfAllowedHosts) > 0 {
		handler = csrfMiddleware(s.csrfAllowedHosts)(handler)
	}

	if s.corsConfig != nil {
		handler = corsMiddleware(*s.corsConfig)(handler)
	}

	handler = securityHeadersMiddleware(handler)

	s.httpServer.Handler = handler
	return s
}

// readyMiddleware returns 503 for every route it wraps until SetReady(true)
// has been called, so requests never observe a startup Projector rebuild
// in progress.
func (s *Server) readyMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.ready.Load() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "rebuilding"})
			return
		}
		h.ServeHTTP(w, r)
	})
}

// wrapAuth wraps a handler with the readiness gate, rate limiting (if
// configured), and auth middleware (if enabled).
func (s *Server) wrapAuth(h http.Handler) http.Handler {
	h = s.readyMiddleware(h)
	if s.rateLimiter != nil {
		h = s.rateLimiter.Middleware(h)
	}
	if !s.authEnabled {
		return h
	}
	return basicAuthMiddleware(s.authUsername, s.authPassword, s.authFailureLimiter)(h)
}

// wrapSSEAuth wraps a handler with the readiness gate, rate limiting (if
// configured), and SSE-aware auth middleware (accepts both Basic Auth and
// SSE tokens via query parameter).
func (s *Server) wrapSSEAuth(h http.Handler) http.Handler {
	h = s.readyMiddleware(h)
	if s.rateLimiter != nil {
		h = s.rateLimiter.Middleware(h)
	}
	if !s.authEnabled {
		return h
	}
	return sseTokenMiddleware(s.authUsername, s.authPassword, s.sseSecret, s.authFailureLimiter)(h)
}

// registerRoutes sets up the API routes per the route auth matrix:
// /api/v1/health is the only unauthenticated endpoint. Every other route
// requires Basic Auth in LAN mode (via wrapAuth/wrapSSEAuth); loopback mode
// leaves auth disabled by default per existing config semantics.
func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth)

	if s.observations != nil {
		s.mux.Handle("GET /api/v1/observations", s.wrapAuth(http.HandlerFunc(s.handleObservations)))
	}

	if s.state != nil {
		s.mux.Handle("GET /api/v1/state", s.wrapAuth(http.HandlerFunc(s.handleState)))
	}

	if s.media != nil {
		s.mux.Handle("GET /api/v1/media/recent", s.wrapAuth(http.HandlerFunc(s.handleMediaRecent)))
	}

	if s.adapters != nil {
		s.mux.Handle("GET /api/v1/adapters", s.wrapAuth(http.HandlerFunc(s.handleAdapters)))
	}

	if s.stats != nil {
		s.mux.Handle("GET /api/v1/stats/basic", s.wrapAuth(http.HandlerFunc(s.handleStats)))
	}

	if s.broadcaster != nil && s.observationsStore != nil {
		s.mux.Handle("GET /api/v1/stream", s.wrapSSEAuth(http.HandlerFunc(s.handleStream)))
	}

	// Auth token endpoint mints SSE tokens; it must only accept Basic Auth
	// (wrapAuth), never an SSE token itself.
	if len(s.sseSecret) > 0 {
		s.mux.Handle("POST /api/v1/auth/token", s.wrapAuth(http.HandlerFunc(s.handleAuthToken)))
	}

	if s.cfg != nil {
		s.mux.Handle("GET /api/v1/config", s.wrapAuth(http.HandlerFunc(s.handleGetConfig)))
		s.mux.Handle("PUT /api/v1/config", s.wrapAuth(http.HandlerFunc(s.handlePutConfig)))
	}

	// Static file serving (catch-all, must be last)
	if s.webFS != nil {
		spa, err := newSPAHandler(s.webFS)
		if err == nil {
			s.mux.Handle("/", spa)
		}
	}
}

// handleHealth handles the health check endpoint. Unauthenticated by
// design (spec 18.1): it returns only status/database/ingest/adapter count,
// never a secret, path, or URL. Unlike every other route, it is never
// gated by SetReady — during startup Projector rebuild it reports
// ingest="rebuilding" instead of 503ing.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	result, err := s.health.Handle(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", err)
		return
	}
	if !s.ready.Load() {
		result.Status = app.StatusDegraded
		result.Ingest = "rebuilding"
	}
	writeJSON(w, http.StatusOK, result)
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// Addr returns the server address.
func (s *Server) Addr() string {
	return s.httpServer.Addr
}

// Handler returns the HTTP handler for testing purposes.
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}
