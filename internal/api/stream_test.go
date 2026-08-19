package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	vrclog "github.com/vrclog/vrclog-go"

	"github.com/vrclog/vrclog-companion/internal/sse"
	"github.com/vrclog/vrclog-companion/internal/store"
)

func commitTestObservation(t *testing.T, st *store.Store, id, sourceID string, offset int64, line uint64, at time.Time) store.CommitResult {
	t.Helper()
	rec := vrclog.Record{
		ID:         vrclog.RecordID("rec-" + id),
		Time:       at,
		SourceID:   vrclog.SourceID(sourceID),
		Path:       "/tmp/" + sourceID + ".txt",
		Offset:     offset,
		NextOffset: offset + 10,
		Line:       line,
	}
	obs := vrclog.Observation{
		ID:        vrclog.ObservationID(id),
		Time:      at,
		AdapterID: "vrchat.core",
		RuleID:    "player_joined",
		Record: vrclog.RecordRef{
			ID: rec.ID, SourceID: rec.SourceID, Offset: rec.Offset, Line: rec.Line,
		},
		Event: vrclog.PlayerJoined{Player: vrclog.Player{DisplayName: id}},
	}
	result, err := st.CommitRecord(context.Background(), store.RecordCommit{
		Record:     rec,
		Result:     vrclog.Result{Observations: []vrclog.Observation{obs}},
		IngestedAt: at,
	})
	if err != nil {
		t.Fatalf("CommitRecord(%s): %v", id, err)
	}
	return result
}

// TestStream_BacklogSurvivesRestart pins the CRITICAL fix: a client
// reconnecting with a valid Last-Event-ID must receive DB backlog even
// when the Broadcaster is freshly constructed (highWater=0), exactly as
// happens after a process restart. Before the fix, sendBacklog compared
// against Broadcaster.HighWaterSequence() and delivered nothing.
func TestStream_BacklogSurvivesRestart(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "stream-test.sqlite"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	now := time.Now().UTC()
	first := commitTestObservation(t, st, "obs1", "src1", 0, 1, now)
	commitTestObservation(t, st, "obs2", "src1", 10, 2, now.Add(time.Second))
	commitTestObservation(t, st, "obs3", "src1", 20, 3, now.Add(2*time.Second))

	// A brand-new Broadcaster simulates the state immediately after a
	// process restart: highWater=0, even though the DB already has rows
	// with much higher sequences.
	b := sse.NewBroadcaster()
	defer b.Stop()

	server := NewServer("127.0.0.1:0", testHealth(), WithBroadcaster(b, st))
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/stream", nil)
	req.Header.Set("Last-Event-ID", string(first.InsertedObservations[0].ID))

	resp, err := ts.Client().Do(req)
	if err != nil {
		// A context-deadline error after headers/body started streaming is
		// expected (SSE holds the connection open); ignore that specific
		// case and inspect whatever body was captured instead.
		if !strings.Contains(err.Error(), "context deadline exceeded") {
			t.Fatalf("request failed: %v", err)
		}
	}
	if resp == nil {
		t.Fatal("no response received")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body) // best-effort read until deadline/close
	text := string(body)

	if !strings.Contains(text, `"id":"obs2"`) {
		t.Errorf("backlog missing obs2 (highWater-vs-DB restart bug not fixed): %s", text)
	}
	if !strings.Contains(text, `"id":"obs3"`) {
		t.Errorf("backlog missing obs3 (highWater-vs-DB restart bug not fixed): %s", text)
	}
	if strings.Contains(text, `"id":"obs1"`) {
		t.Errorf("backlog re-delivered obs1, which the client already has: %s", text)
	}
}

// TestStream_LiveEventNotDuplicatedAfterBacklog verifies that once backlog
// delivery ends, the live channel does not redeliver the same observation
// (the lastSent returned from sendBacklog must gate the live loop).
func TestStream_LiveEventNotDuplicatedAfterBacklog(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "stream-test2.sqlite"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	now := time.Now().UTC()
	first := commitTestObservation(t, st, "obsA", "src1", 0, 1, now)
	second := commitTestObservation(t, st, "obsB", "src1", 10, 2, now.Add(time.Second))

	b := sse.NewBroadcaster()
	defer b.Stop()

	server := NewServer("127.0.0.1:0", testHealth(), WithBroadcaster(b, st))
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/stream", nil)
	req.Header.Set("Last-Event-ID", string(first.InsertedObservations[0].ID))

	resp, err := ts.Client().Do(req)
	if err != nil && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("request failed: %v", err)
	}
	if resp == nil {
		t.Fatal("no response received")
	}
	defer resp.Body.Close()

	// Broadcast obsB again on the live channel — simulates a race where the
	// observation committed right around Subscribe()/LatestSequence() time.
	b.Broadcast(second.InsertedObservations[0])

	body, _ := io.ReadAll(resp.Body)
	count := strings.Count(string(body), `"id":"obsB"`)
	if count != 1 {
		t.Errorf(`"obsB" appeared %d times, want exactly 1 (backlog + live must not double-deliver): %s`, count, body)
	}
}
