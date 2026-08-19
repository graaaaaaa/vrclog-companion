package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	vrclog "github.com/vrclog/vrclog-go"

	"github.com/vrclog/vrclog-companion/internal/observation"
)

const (
	// heartbeatInterval is the interval for sending SSE heartbeat comments.
	heartbeatInterval = 20 * time.Second

	// backlogPageSize bounds each DB page fetched while replaying backlog.
	backlogPageSize = 100

	// maxBacklogObservations bounds total replay work for a stale
	// reconnect. A cursor older than this is reset rather than replayed,
	// so one HTTP request can never be forced to redeliver an unbounded
	// amount of history.
	maxBacklogObservations = 1000
)

// SSEObservationStore is the store dependency needed to resolve
// Last-Event-ID and replay backlog.
type SSEObservationStore interface {
	ObservationByID(ctx context.Context, id vrclog.ObservationID) (*observation.StoredObservation, error)
	ObservationsAfterSequence(ctx context.Context, sequence int64, limit int) ([]observation.StoredObservation, error)
	// LatestSequence returns the DB's current max sequence. This is the
	// backlog upper bound — NOT Broadcaster.HighWaterSequence(), which
	// starts at 0 on every process restart and only advances as new
	// Observations are broadcast in the current process's lifetime. Using
	// it as the bound would silently truncate backlog delivery to nothing
	// immediately after a restart.
	LatestSequence(ctx context.Context) (int64, error)
}

// handleStream handles GET /api/v1/stream (SSE).
//
// Race-free reconnect algorithm:
//  1. Subscribe to the broadcaster first.
//  2. Resolve Last-Event-ID to a sequence, then read the DB's current max
//     sequence (LatestSequence) as the backlog upper bound.
//  3. Replay DB backlog up to that bound, bounded by maxBacklogObservations.
//  4. Only then start reading the live channel, skipping anything at or
//     below what backlog actually delivered.
//
// Any Observation committed between steps 1 and 2 is guaranteed to be
// covered by either the backlog query or the live channel — never both,
// never neither: LatestSequence only ever sees committed rows (CommitRecord
// commits before Broadcast fires), so either it already reflects the new
// row (backlog delivers it) or it doesn't yet (the live channel — already
// subscribed in step 1 — delivers it once Broadcast runs).
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported", nil)
		return
	}

	ip := extractIP(r)
	if !s.sseConnLimiter.acquire(ip) {
		writeError(w, http.StatusServiceUnavailable, "too many active streams", nil)
		return
	}
	defer s.sseConnLimiter.release(ip)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sub := s.broadcaster.Subscribe()
	defer s.broadcaster.Unsubscribe(sub)

	lastEventID := r.Header.Get("Last-Event-ID")
	if lastEventID == "" {
		lastEventID = r.URL.Query().Get("last_event_id")
	}

	var lastSent int64
	if lastEventID != "" {
		start, ok := s.resolveLastEventID(r.Context(), w, flusher, lastEventID)
		if !ok {
			// Unknown/stale cursor: a reset event was already sent; the
			// client is expected to refetch and reconnect without it.
			return
		}

		highWater, err := s.observationsStore.LatestSequence(r.Context())
		if err != nil {
			return
		}

		delivered, ok := s.sendBacklog(r.Context(), w, flusher, start, highWater)
		if !ok {
			return
		}
		lastSent = delivered
	}

	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case obs, ok := <-sub.Events():
			if !ok {
				return
			}
			if obs.Sequence <= lastSent {
				continue // already delivered via backlog
			}
			writeSSEObservation(w, obs)
			lastSent = obs.Sequence
			flusher.Flush()

		case <-ticker.C:
			fmt.Fprintf(w, ":\n\n")
			flusher.Flush()

		case <-ctx.Done():
			return

		case <-sub.Done():
			return
		}
	}
}

// resolveLastEventID looks up the sequence for a client-supplied
// Last-Event-ID and returns ok=false if the caller should stop.
//
// A reset event is sent only when the ID is genuinely unknown/stale
// (found == nil) — the client is expected to refetch full state rather
// than silently lose data. A transient store error (e.g. a busy SQLite
// read) is NOT treated the same way: sending a reset would discard a
// still-valid cursor over a temporary hiccup, so the connection is
// closed without a reset and the client's next reconnect retries with
// the same Last-Event-ID.
func (s *Server) resolveLastEventID(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, lastEventID string) (sequence int64, ok bool) {
	found, err := s.observationsStore.ObservationByID(ctx, vrclog.ObservationID(lastEventID))
	if err != nil {
		return 0, false
	}
	if found == nil {
		writeSSEReset(w, flusher)
		return 0, false
	}
	return found.Sequence, true
}

// sendBacklog replays DB Observations in (from, highWater], returning the
// sequence actually delivered (so the caller's live-channel dedup starts
// from the right place) and whether the connection should continue.
//
// Delivery is buffered in `pending` and only written to the wire once we
// know the batch is within maxBacklogObservations — a stale reconnect that
// would require an unbounded replay gets a clean event: reset instead of a
// half-delivered backlog followed by an abrupt cutoff.
func (s *Server) sendBacklog(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, from, highWater int64) (delivered int64, ok bool) {
	if from >= highWater {
		return from, true
	}

	cursor := from
	replayed := 0
	pending := make([]observation.StoredObservation, 0, backlogPageSize)

	flushPending := func() {
		for _, obs := range pending {
			writeSSEObservation(w, obs)
		}
		if len(pending) > 0 {
			flusher.Flush()
		}
	}

	for {
		batch, err := s.observationsStore.ObservationsAfterSequence(ctx, cursor, backlogPageSize)
		if err != nil {
			return from, false
		}
		if len(batch) == 0 {
			break
		}

		for _, obs := range batch {
			if obs.Sequence > highWater {
				flushPending()
				return cursor, true // rest is covered by the live channel
			}

			replayed++
			if replayed > maxBacklogObservations {
				writeSSEReset(w, flusher)
				return from, false
			}

			pending = append(pending, obs)
			cursor = obs.Sequence
		}

		if len(batch) < backlogPageSize {
			break
		}
	}

	flushPending()
	return cursor, true
}

func writeSSEObservation(w http.ResponseWriter, obs observation.StoredObservation) {
	data, err := json.Marshal(toObservationDTO(obs))
	if err != nil {
		return
	}
	fmt.Fprintf(w, "id: %s\n", obs.ID)
	fmt.Fprintf(w, "event: observation\n")
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// writeSSEReset tells the client its Last-Event-ID is unknown (e.g. the
// database was reset). The empty id clears the browser's EventSource
// bookmark so a naive client cannot loop by resending the same stale ID.
func writeSSEReset(w http.ResponseWriter, flusher http.Flusher) {
	fmt.Fprintf(w, "id: \n")
	fmt.Fprintf(w, "event: reset\n")
	fmt.Fprintf(w, "data: {}\n\n")
	flusher.Flush()
}
