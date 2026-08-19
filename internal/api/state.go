package api

import (
	"net/http"
	"time"
)

type worldDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	InstanceID string `json:"instance_id"`
	JoinedAt   string `json:"joined_at"`
}

type playerDTO struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	JoinedAt    string `json:"joined_at"`
}

type latestOpenableMediaDTO struct {
	AttemptID  string `json:"attempt_id"`
	URL        string `json:"url"`
	Status     string `json:"status"`
	ObservedAt string `json:"observed_at"`
}

type stateResponse struct {
	World               *worldDTO               `json:"world"`
	Players             []playerDTO             `json:"players"`
	LatestOpenableMedia *latestOpenableMediaDTO `json:"latest_openable_media,omitempty"`
}

// handleState handles GET /api/v1/state.
func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if s.state == nil {
		writeError(w, http.StatusServiceUnavailable, "state not available", nil)
		return
	}

	snap := s.state.GetCurrentState(r.Context())
	resp := stateResponse{Players: []playerDTO{}}

	if snap.World != nil {
		resp.World = &worldDTO{
			ID:         snap.World.ID,
			Name:       snap.World.Name,
			InstanceID: snap.World.InstanceID,
			JoinedAt:   snap.World.JoinedAt.UTC().Format(time.RFC3339),
		}
	}

	for _, p := range snap.Players {
		resp.Players = append(resp.Players, playerDTO{
			ID:          p.ID,
			DisplayName: p.DisplayName,
			JoinedAt:    p.JoinedAt.UTC().Format(time.RFC3339),
		})
	}

	if m := snap.LatestOpenableMedia; m != nil {
		resp.LatestOpenableMedia = &latestOpenableMediaDTO{
			AttemptID:  m.AttemptID,
			URL:        m.URL,
			Status:     m.Status,
			ObservedAt: m.ObservedAt.UTC().Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, resp)
}
