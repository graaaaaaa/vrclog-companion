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
	health     app.HealthUsecase
}

// NewServer creates a new API server with the given dependencies.
func NewServer(addr string, health app.HealthUsecase) *Server {
	mux := http.NewServeMux()
	s := &Server{
		httpServer: &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		mux:    mux,
		health: health,
	}
	s.registerRoutes()
	return s
}

// registerRoutes sets up the API routes.
func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth)
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
