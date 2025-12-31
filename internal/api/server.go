// Package api provides HTTP API server functionality.
package api

import (
	"context"
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

	// SSE hub
	hub *Hub

	// Auth configuration
	authEnabled  bool
	authUsername string
	authPassword string
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithEventsUsecase sets the events use case.
func WithEventsUsecase(events app.EventsUsecase) ServerOption {
	return func(s *Server) { s.events = events }
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

// registerRoutes sets up the API routes.
func (s *Server) registerRoutes() {
	// Health endpoint (no auth required)
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth)

	// Events endpoint (auth required if configured)
	if s.events != nil {
		s.mux.Handle("GET /api/v1/events", s.wrapAuth(http.HandlerFunc(s.handleEvents)))
	}

	// SSE stream endpoint (auth required if configured)
	if s.hub != nil && s.events != nil {
		s.mux.Handle("GET /api/v1/stream", s.wrapAuth(http.HandlerFunc(s.handleStream)))
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
