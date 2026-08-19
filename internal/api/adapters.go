package api

import "net/http"

type loadedAdapterDTO struct {
	ID     string `json:"id"`
	Origin string `json:"origin"`
}

type adaptersResponse struct {
	Adapters []loadedAdapterDTO `json:"adapters"`
}

// handleAdapters handles GET /api/v1/adapters.
func (s *Server) handleAdapters(w http.ResponseWriter, r *http.Request) {
	if s.adapters == nil {
		writeError(w, http.StatusServiceUnavailable, "adapters not available", nil)
		return
	}

	loaded := s.adapters.List(r.Context())
	dtos := make([]loadedAdapterDTO, len(loaded))
	for i, a := range loaded {
		dtos[i] = loadedAdapterDTO{ID: a.ID, Origin: a.Origin}
	}

	writeJSON(w, http.StatusOK, adaptersResponse{Adapters: dtos})
}
