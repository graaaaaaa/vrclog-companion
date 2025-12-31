package api

import (
	"encoding/json"
	"net/http"

	"github.com/graaaaa/vrclog-companion/internal/app"
)

// handleGetConfig handles GET /api/v1/config requests.
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}

	result := s.cfg.GetConfig(r.Context())
	writeJSON(w, http.StatusOK, result)
}

// handlePutConfig handles PUT /api/v1/config requests.
func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}

	var req app.ConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}

	result, err := s.cfg.UpdateConfig(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}
