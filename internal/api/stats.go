package api

import (
	"net/http"
)

// handleStats handles GET /api/v1/stats/basic requests.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if s.stats == nil {
		writeError(w, http.StatusServiceUnavailable, "stats not available", nil)
		return
	}

	result, err := s.stats.GetBasicStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}
