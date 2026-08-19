package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/vrclog/vrclog-companion/internal/projector"
)

type mediaResourceDTO struct {
	URL           string `json:"url"`
	Kind          string `json:"kind"`
	Role          string `json:"role"`
	AdapterID     string `json:"adapter_id"`
	RuleID        string `json:"rule_id"`
	ObservationID string `json:"observation_id"`
	ObservedAt    string `json:"observed_at"`
}

type mediaErrorDTO struct {
	Stage         string `json:"stage"`
	Code          string `json:"code,omitempty"`
	Message       string `json:"message,omitempty"`
	AdapterID     string `json:"adapter_id"`
	ObservationID string `json:"observation_id"`
	ObservedAt    string `json:"observed_at"`
}

type mediaTargetDTO struct {
	Component string `json:"component,omitempty"`
	Key       string `json:"key,omitempty"`
	Backend   string `json:"backend,omitempty"`
}

type mediaAttemptDTO struct {
	ID              string             `json:"id"`
	FirstObservedAt string             `json:"first_observed_at"`
	LastObservedAt  string             `json:"last_observed_at"`
	Status          string             `json:"status"`
	BestOpenableURL string             `json:"best_openable_url,omitempty"`
	Resources       []mediaResourceDTO `json:"resources"`
	Errors          []mediaErrorDTO    `json:"errors"`
	ObservationIDs  []string           `json:"observation_ids"`
	AdapterIDs      []string           `json:"adapter_ids"`
	Target          *mediaTargetDTO    `json:"target,omitempty"`
	WorldInstanceID string             `json:"world_instance_id,omitempty"`
}

func toMediaAttemptDTO(a *projector.MediaAttempt) mediaAttemptDTO {
	resources := make([]mediaResourceDTO, len(a.Resources))
	for i, r := range a.Resources {
		resources[i] = mediaResourceDTO{
			URL: r.URL, Kind: r.Kind, Role: r.Role,
			AdapterID: r.AdapterID, RuleID: r.RuleID, ObservationID: r.ObservationID,
			ObservedAt: r.ObservedAt.UTC().Format(time.RFC3339Nano),
		}
	}

	errs := make([]mediaErrorDTO, len(a.Errors))
	for i, e := range a.Errors {
		errs[i] = mediaErrorDTO{
			Stage: e.Stage, Code: e.Code, Message: e.Message,
			AdapterID: e.AdapterID, ObservationID: e.ObservationID,
			ObservedAt: e.ObservedAt.UTC().Format(time.RFC3339Nano),
		}
	}

	dto := mediaAttemptDTO{
		ID:              a.ID,
		FirstObservedAt: a.FirstObservedAt.UTC().Format(time.RFC3339Nano),
		LastObservedAt:  a.LastObservedAt.UTC().Format(time.RFC3339Nano),
		Status:          a.Status,
		BestOpenableURL: a.BestOpenableURL,
		Resources:       resources,
		Errors:          errs,
		ObservationIDs:  append([]string{}, a.ObservationIDs...),
		AdapterIDs:      append([]string{}, a.AdapterIDs...),
		WorldInstanceID: a.WorldInstanceID,
	}
	if a.Target != nil {
		dto.Target = &mediaTargetDTO{Component: a.Target.Component, Key: a.Target.Key, Backend: a.Target.Backend}
	}
	return dto
}

type mediaRecentResponse struct {
	Attempts []mediaAttemptDTO `json:"attempts"`
}

// handleMediaRecent handles GET /api/v1/media/recent.
func (s *Server) handleMediaRecent(w http.ResponseWriter, r *http.Request) {
	if s.media == nil {
		writeError(w, http.StatusServiceUnavailable, "media not available", nil)
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 || n > 50 {
			writeError(w, http.StatusBadRequest, "invalid limit", nil)
			return
		}
		limit = n
	}

	attempts := s.media.Recent(r.Context(), limit)
	dtos := make([]mediaAttemptDTO, len(attempts))
	for i, a := range attempts {
		dtos[i] = toMediaAttemptDTO(a)
	}

	writeJSON(w, http.StatusOK, mediaRecentResponse{Attempts: dtos})
}
