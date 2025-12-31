package api

import (
	"net/http"
)

// handleNow handles GET /api/v1/now requests.
func (s *Server) handleNow(w http.ResponseWriter, r *http.Request) {
	if s.state == nil {
		http.Error(w, "state not available", http.StatusServiceUnavailable)
		return
	}

	result := s.state.GetCurrentState(r.Context())
	writeJSON(w, http.StatusOK, result)
}
