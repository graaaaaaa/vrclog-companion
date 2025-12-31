// Package api provides HTTP API server functionality.
package api

import (
	"context"
	"io/fs"
	"net/http"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/app"
)

// Server represents the HTTP API server.
type Server struct {
	httpServer *http.Server
	mux        *http.ServeMux

	// Use case dependencies
	health app.HealthUsecase
	events app.EventsUsecase
	state  app.StateUsecase
	cfg    app.ConfigUsecase

	// SSE hub
	hub *Hub

	// Auth configuration
	authEnabled  bool
	authUsername string
	authPassword string

	// SSE token configuration
	sseSecret []byte

	// Web UI filesystem
	webFS fs.FS
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithEventsUsecase sets the events use case.
func WithEventsUsecase(events app.EventsUsecase) ServerOption {
	return func(s *Server) { s.events = events }
}

// WithStateUsecase sets the state use case.
func WithStateUsecase(state app.StateUsecase) ServerOption {
	return func(s *Server) { s.state = state }
}

// WithConfigUsecase sets the config use case.
func WithConfigUsecase(cfg app.ConfigUsecase) ServerOption {
	return func(s *Server) { s.cfg = cfg }
}

// WithHub sets the SSE hub.
func WithHub(hub *Hub) ServerOption {
	return func(s *Server) { s.hub = hub }
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

// NewServer creates a new API server with the given dependencies.
func NewServer(addr string, health app.HealthUsecase, opts ...ServerOption) *Server {
	mux := http.NewServeMux()
	s := &Server{
		httpServer: &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 0, // Disable for SSE (long-lived connections)
			IdleTimeout:  60 * time.Second,
		},
		mux:    mux,
		health: health,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.registerRoutes()
	return s
}

// wrapAuth wraps a handler with auth middleware if auth is enabled.
func (s *Server) wrapAuth(h http.Handler) http.Handler {
	if !s.authEnabled {
		return h
	}
	return basicAuthMiddleware(s.authUsername, s.authPassword)(h)
}

// wrapSSEAuth wraps a handler with SSE-aware auth middleware.
// Accepts both Basic Auth and SSE tokens via query parameter.
func (s *Server) wrapSSEAuth(h http.Handler) http.Handler {
	if !s.authEnabled {
		return h
	}
	return sseTokenMiddleware(s.authUsername, s.authPassword, s.sseSecret)(h)
}

// registerRoutes sets up the API routes.
func (s *Server) registerRoutes() {
	// Health endpoint (no auth required)
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth)

	// Events endpoint (auth required if configured)
	if s.events != nil {
		s.mux.Handle("GET /api/v1/events", s.wrapAuth(http.HandlerFunc(s.handleEvents)))
	}

	// Now endpoint (auth required if configured)
	if s.state != nil {
		s.mux.Handle("GET /api/v1/now", s.wrapAuth(http.HandlerFunc(s.handleNow)))
	}

	// SSE stream endpoint (auth required if configured, accepts token auth)
	if s.hub != nil && s.events != nil {
		s.mux.Handle("GET /api/v1/stream", s.wrapSSEAuth(http.HandlerFunc(s.handleStream)))
	}

	// Auth token endpoint (auth required if configured, issues SSE tokens)
	if len(s.sseSecret) > 0 {
		s.mux.Handle("POST /api/v1/auth/token", s.wrapAuth(http.HandlerFunc(s.handleAuthToken)))
	}

	// Config endpoints (auth required if configured)
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

// handleHealth handles the health check endpoint.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	result, err := s.health.Handle(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
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
