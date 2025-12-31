package api

import (
	"encoding/json"
	"net/http"

	"github.com/graaaaa/vrclog-companion/internal/app"
)

// handleGetConfig handles GET /api/v1/config requests.
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil {
		writeError(w, http.StatusServiceUnavailable, "config not available", nil)
		return
	}

	result := s.cfg.GetConfig(r.Context())
	writeJSON(w, http.StatusOK, result)
}

// handlePutConfig handles PUT /api/v1/config requests.
func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil {
		writeError(w, http.StatusServiceUnavailable, "config not available", nil)
		return
	}

	// Limit request body size to 1MB to prevent DoS
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req app.ConfigUpdateRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields() // Strict JSON parsing
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", nil)
		return
	}

	result, err := s.cfg.UpdateConfig(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, result)
}
