package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/vrclog/vrclog-companion/internal/observation"
	"github.com/vrclog/vrclog-companion/internal/store"
)

// observationRecordDTO is the API-safe subset of RecordRef: no local Path,
// no raw line.
type observationRecordDTO struct {
	ID       string `json:"id"`
	SourceID string `json:"source_id"`
	Offset   int64  `json:"offset"`
	Line     uint64 `json:"line"`
}

type observationDTO struct {
	Sequence   int64                `json:"sequence"`
	ID         string               `json:"id"`
	OccurredAt string               `json:"occurred_at"`
	Type       string               `json:"type"`
	Payload    json.RawMessage      `json:"payload"`
	AdapterID  string               `json:"adapter_id"`
	RuleID     string               `json:"rule_id"`
	Record     observationRecordDTO `json:"record"`
	IngestedAt string               `json:"ingested_at"`
}

func toObservationDTO(obs observation.StoredObservation) observationDTO {
	return observationDTO{
		Sequence:   obs.Sequence,
		ID:         string(obs.ID),
		OccurredAt: obs.OccurredAt.UTC().Format(time.RFC3339Nano),
		Type:       string(obs.Type),
		Payload:    obs.Payload,
		AdapterID:  string(obs.AdapterID),
		RuleID:     string(obs.RuleID),
		Record: observationRecordDTO{
			ID:       string(obs.RecordID),
			SourceID: string(obs.SourceID),
			Offset:   obs.SourceOffset,
			Line:     obs.SourceLine,
		},
		IngestedAt: obs.IngestedAt.UTC().Format(time.RFC3339Nano),
	}
}

type observationsResponse struct {
	Items []observationDTO `json:"items"`
	// NextCursor is always present, explicitly null when there is no next
	// page — omitempty would drop the key entirely, and a JS client reading
	// a missing key gets undefined, which fails a strict `!== null` check.
	NextCursor *int64 `json:"next_cursor"`
}

// handleObservations handles GET /api/v1/observations.
func (s *Server) handleObservations(w http.ResponseWriter, r *http.Request) {
	q, err := parseObservationQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	items, next, err := s.observations.List(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", err)
		return
	}

	dtos := make([]observationDTO, len(items))
	for i, it := range items {
		dtos[i] = toObservationDTO(it)
	}

	writeJSON(w, http.StatusOK, observationsResponse{Items: dtos, NextCursor: next})
}

// parseObservationQuery parses cursor/limit/type/adapter_id/since/until
// query parameters. type/adapter_id are matched by exact equality — there
// is no fixed allowlist, so an unrecognized value simply yields zero rows.
func parseObservationQuery(r *http.Request) (store.ObservationQuery, error) {
	q := store.ObservationQuery{Order: store.OrderDesc}
	qp := r.URL.Query()

	if c := qp.Get("cursor"); c != "" {
		seq, err := strconv.ParseInt(c, 10, 64)
		if err != nil {
			return q, fmt.Errorf("invalid cursor: %s", c)
		}
		q.AfterSequence = &seq
	}

	q.Limit = store.DefaultObservationLimit
	if l := qp.Get("limit"); l != "" {
		limit, err := strconv.Atoi(l)
		if err != nil || limit < 1 || limit > store.MaxObservationLimit {
			return q, fmt.Errorf("invalid limit: %s", l)
		}
		q.Limit = limit
	}

	if t := qp.Get("type"); t != "" {
		q.Type = &t
	}
	if a := qp.Get("adapter_id"); a != "" {
		q.AdapterID = &a
	}

	if since := qp.Get("since"); since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			return q, fmt.Errorf("invalid since: %s", since)
		}
		q.Since = &t
	}
	if until := qp.Get("until"); until != "" {
		t, err := time.Parse(time.RFC3339, until)
		if err != nil {
			return q, fmt.Errorf("invalid until: %s", until)
		}
		q.Until = &t
	}

	return q, nil
}
