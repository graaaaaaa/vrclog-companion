package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/event"
	"github.com/graaaaa/vrclog-companion/internal/store"
)

const (
	// heartbeatInterval is the interval for sending SSE heartbeat comments.
	heartbeatInterval = 20 * time.Second

	// missedEventsPageSize is the number of events to fetch per page during replay.
	missedEventsPageSize = 100

	// missedEventsMaxPages limits the number of pages to replay (best-effort).
	missedEventsMaxPages = 5
)

// handleStream handles GET /api/v1/stream (SSE)
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	// Check for streaming support
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported", nil)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Parse Last-Event-ID header or query parameter for reconnection support
	// Query parameter allows manual reconnection with Last-Event-ID
	lastEventID := r.Header.Get("Last-Event-ID")
	if lastEventID == "" {
		lastEventID = r.URL.Query().Get("last_event_id")
	}

	// If Last-Event-ID is provided, send missed events (best-effort)
	if lastEventID != "" {
		// Errors are ignored - invalid cursor or DB errors just skip replay
		_ = s.sendMissedEvents(r.Context(), w, flusher, lastEventID)
	}

	// Subscribe to hub
	sub := s.hub.Subscribe()
	defer s.hub.Unsubscribe(sub)

	// Send initial comment to establish connection
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	// Create heartbeat ticker
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	// Handle client disconnect
	ctx := r.Context()

	for {
		select {
		case e, ok := <-sub.Events():
			if !ok {
				// Channel closed, subscriber removed
				return
			}

			writeSSEEvent(w, e)
			flusher.Flush()

		case <-ticker.C:
			// Send heartbeat comment to keep connection alive
			fmt.Fprintf(w, ":\n\n")
			flusher.Flush()

		case <-ctx.Done():
			// Client disconnected
			return

		case <-sub.Done():
			// Subscriber removed (hub stopped)
			return
		}
	}
}

// sendMissedEvents sends events that were missed during a reconnection.
// Uses Last-Event-ID as a cursor for QueryEvents.
// Best-effort: invalid cursors or errors are silently ignored.
// Limited to missedEventsMaxPages pages to prevent unbounded replay.
func (s *Server) sendMissedEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, lastEventID string) error {
	cursor := lastEventID
	filter := store.QueryFilter{
		Cursor: &cursor,
		Limit:  missedEventsPageSize,
		Order:  store.QueryOrderAsc, // Fetch events after Last-Event-ID (forward in time)
	}

	for page := 0; page < missedEventsMaxPages; page++ {
		result, err := s.events.Query(ctx, filter)
		if err != nil {
			if errors.Is(err, store.ErrInvalidCursor) {
				// Invalid cursor - skip replay and start fresh
				return nil
			}
			// Other errors (DB, context cancelled) - stop replay
			return err
		}

		for i := range result.Items {
			writeSSEEvent(w, &result.Items[i])
		}
		flusher.Flush()

		if result.NextCursor == nil {
			break
		}
		filter.Cursor = result.NextCursor
	}

	return nil
}

// writeSSEEvent writes a single event in SSE format.
// Uses cursor-style ID (base64(ts|id)) for Last-Event-ID support.
func writeSSEEvent(w http.ResponseWriter, e *event.Event) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}

	eventID := store.EncodeCursor(e.Ts, e.ID)
	fmt.Fprintf(w, "id: %s\n", eventID)
	fmt.Fprintf(w, "event: %s\n", e.Type)
	fmt.Fprintf(w, "data: %s\n\n", data)
}
