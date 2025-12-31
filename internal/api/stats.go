package api

import (
	"net/http"
)

// handleStats handles GET /api/v1/stats/basic requests.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if s.stats == nil {
		http.Error(w, "stats not available", http.StatusServiceUnavailable)
		return
	}

	result, err := s.stats.GetBasicStats(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, result)
}
