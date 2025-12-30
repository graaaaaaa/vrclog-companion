package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/event"
	"github.com/graaaaa/vrclog-companion/internal/store"
)

// eventsResponse represents the response for the events endpoint.
type eventsResponse struct {
	Items      []event.Event `json:"items"`
	NextCursor *string       `json:"next_cursor,omitempty"`
}

// handleEvents handles GET /api/v1/events
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	filter, err := parseEventsFilter(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	result, err := s.events.Query(r.Context(), filter)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cursor"})
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := eventsResponse{
		Items:      result.Items,
		NextCursor: result.NextCursor,
	}

	// Ensure Items is an empty array, not null, for JSON serialization
	if resp.Items == nil {
		resp.Items = []event.Event{}
	}

	writeJSON(w, http.StatusOK, resp)
}

// parseEventsFilter parses query parameters into a QueryFilter.
func parseEventsFilter(r *http.Request) (store.QueryFilter, error) {
	var filter store.QueryFilter
	q := r.URL.Query()

	// Parse 'since' (RFC3339)
	if s := q.Get("since"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return filter, fmt.Errorf("invalid since: %w", err)
		}
		filter.Since = &t
	}

	// Parse 'until' (RFC3339)
	if u := q.Get("until"); u != "" {
		t, err := time.Parse(time.RFC3339, u)
		if err != nil {
			return filter, fmt.Errorf("invalid until: %w", err)
		}
		filter.Until = &t
	}

	// Parse 'type'
	if t := q.Get("type"); t != "" {
		switch t {
		case event.TypePlayerJoin, event.TypePlayerLeft, event.TypeWorldJoin:
			filter.Type = &t
		default:
			return filter, fmt.Errorf("invalid type: %s", t)
		}
	}

	// Parse 'limit'
	if l := q.Get("limit"); l != "" {
		limit, err := strconv.Atoi(l)
		if err != nil || limit < 1 {
			return filter, fmt.Errorf("invalid limit: %s", l)
		}
		filter.Limit = limit
	}

	// Parse 'cursor'
	if c := q.Get("cursor"); c != "" {
		filter.Cursor = &c
	}

	return filter, nil
}
