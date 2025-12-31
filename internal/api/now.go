package api

import (
	"net/http"
)

// handleNow handles GET /api/v1/now requests.
func (s *Server) handleNow(w http.ResponseWriter, r *http.Request) {
	if s.state == nil {
		writeError(w, http.StatusServiceUnavailable, "state not available", nil)
		return
	}

	result := s.state.GetCurrentState(r.Context())
	writeJSON(w, http.StatusOK, result)
}
