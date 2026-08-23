//go:build e2e

// TestE2E_CatchUpSuppression drives the real ingest.Runner (with the real
// vrclog.Follow-backed VRChat source, not a fake) against a temp log
// directory, wiring onInsert exactly as cmd/vrclog-companion/main.go does:
// every Observation is applied to the Projector regardless of phase, but
// only DeliveryLive Observations fan out to SSE/notification. This is the
// hardening spec §12.3 acceptance scenario.
package e2e

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vrclog/vrclog-companion/internal/adapter"
	"github.com/vrclog/vrclog-companion/internal/ingest"
	"github.com/vrclog/vrclog-companion/internal/observation"
	"github.com/vrclog/vrclog-companion/internal/projector"
	"github.com/vrclog/vrclog-companion/internal/sse"
	"github.com/vrclog/vrclog-companion/internal/store"
)

func TestE2E_CatchUpSuppression(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "output_log_2024-01-01_08-00-00.txt")
	preExisting := "2024.01.01 08:00:00 Log        -  [Behaviour] OnPlayerJoined Alice\n"
	if err := os.WriteFile(logPath, []byte(preExisting), 0o644); err != nil {
		t.Fatalf("write pre-existing log: %v", err)
	}

	engine, _, err := adapter.BuildEngine()
	if err != nil {
		t.Fatalf("BuildEngine: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "e2e-catchup.sqlite")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	manager := projector.NewManager()
	broadcaster := sse.NewBroadcaster()
	t.Cleanup(broadcaster.Stop)

	sub := broadcaster.Subscribe()
	t.Cleanup(func() { broadcaster.Unsubscribe(sub) })

	var broadcastCount, notifyCount, catchUpApplied, liveApplied atomic.Int64

	onInsert := func(_ context.Context, phase ingest.DeliveryPhase, obs observation.StoredObservation) error {
		changes, err := manager.Apply(obs)
		if err != nil {
			return err
		}
		if phase == ingest.DeliveryCatchUp {
			catchUpApplied.Add(1)
		}
		if phase == ingest.DeliveryLive {
			liveApplied.Add(1)
			broadcaster.Broadcast(obs)
			broadcastCount.Add(1)
			notifyCount.Add(int64(len(changes)))
		}
		return nil
	}

	factory := ingest.NewVRChatSourceFactory(ingest.VRChatSourceConfig{LogDir: dir, PollInterval: 100 * time.Millisecond})
	runner := ingest.NewRunner(factory, engine, st, ingest.WithOnInsert(onInsert))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	runDone := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(runDone)
	}()

	// Wait for the pre-existing (catch-up) Record to be applied.
	deadline := time.Now().Add(3 * time.Second)
	for catchUpApplied.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if catchUpApplied.Load() == 0 {
		t.Fatal("catch-up record was never applied to the Projector within 3s")
	}

	// DB must contain the catch-up Observation.
	items, _, err := st.ListObservations(context.Background(), store.ObservationQuery{})
	if err != nil {
		t.Fatalf("ListObservations: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("catch-up Observation was not persisted to the DB")
	}

	// Projector state must reflect it (world/presence via Snapshot, or at
	// minimum no panic/empty state) — check the player joined.
	snap := manager.Snapshot()
	foundAlice := false
	for _, p := range snap.Players {
		if p.DisplayName == "Alice" {
			foundAlice = true
		}
	}
	if !foundAlice {
		t.Fatalf("Projector snapshot missing catch-up player Alice: %+v", snap.Players)
	}

	// Catch-up must never broadcast or notify.
	if broadcastCount.Load() != 0 {
		t.Fatalf("broadcastCount = %d after catch-up, want 0", broadcastCount.Load())
	}
	if notifyCount.Load() != 0 {
		t.Fatalf("notifyCount = %d after catch-up, want 0", notifyCount.Load())
	}
	select {
	case obs := <-sub.Events():
		t.Fatalf("SSE subscriber received a catch-up Observation: %+v", obs)
	case <-time.After(200 * time.Millisecond):
		// Expected: nothing delivered.
	}

	// Now append a live line and expect it to broadcast/notify.
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	if _, err := f.WriteString("2024.01.01 08:00:05 Log        -  [Behaviour] OnPlayerJoined Bob\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
	f.Close()

	deadline = time.Now().Add(3 * time.Second)
	for liveApplied.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if liveApplied.Load() == 0 {
		t.Fatal("live record was never applied within 3s")
	}

	if broadcastCount.Load() != 1 {
		t.Fatalf("broadcastCount after live record = %d, want 1", broadcastCount.Load())
	}

	select {
	case <-sub.Events():
		// Expected: the live Observation was delivered over SSE.
	case <-time.After(2 * time.Second):
		t.Fatal("SSE subscriber never received the live Observation")
	}

	cancel()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("runner.Run did not stop within 2s of cancellation")
	}
}
