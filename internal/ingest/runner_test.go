package ingest

import (
	"context"
	"errors"
	"iter"
	"path/filepath"
	"sync"
	"testing"
	"time"

	vrclog "github.com/vrclog/vrclog-go"

	"github.com/vrclog/vrclog-companion/internal/observation"
	"github.com/vrclog/vrclog-companion/internal/store"
)

// --- test doubles ---

type recordOrErr struct {
	record vrclog.Record
	err    error
}

// fakeSource yields a fixed sequence then blocks on ctx.Done, simulating a
// live tail that has caught up.
type fakeSource struct {
	items []recordOrErr
}

func (s *fakeSource) Records(ctx context.Context) iter.Seq2[vrclog.Record, error] {
	return func(yield func(vrclog.Record, error) bool) {
		for _, item := range s.items {
			if ctx.Err() != nil {
				return
			}
			if !yield(item.record, item.err) {
				return
			}
		}
		<-ctx.Done()
	}
}

type staticFactory struct {
	mu      sync.Mutex
	sources []RecordSource
	calls   []*vrclog.Cursor
	err     error
}

func (f *staticFactory) NewSource(_ context.Context, cursor *vrclog.Cursor) (RecordSource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, cursor)
	if f.err != nil {
		return nil, f.err
	}
	if len(f.sources) == 0 {
		return &fakeSource{}, nil
	}
	src := f.sources[0]
	f.sources = f.sources[1:]
	return src, nil
}

func (f *staticFactory) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *staticFactory) lastCursor() *vrclog.Cursor {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

type fakeEngine struct {
	mu       sync.Mutex
	seen     []vrclog.Record
	resultFn func(vrclog.Record) vrclog.Result
}

func (e *fakeEngine) Process(record vrclog.Record) vrclog.Result {
	e.mu.Lock()
	e.seen = append(e.seen, record)
	e.mu.Unlock()
	if e.resultFn != nil {
		return e.resultFn(record)
	}
	return vrclog.Result{}
}

func (e *fakeEngine) seenCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.seen)
}

// fakeStore is a minimal in-memory RecordStore that can be told to fail the
// next N CommitRecord calls, for deterministic DB-failure retry testing. It
// also records the offset attempted on every call (including failures) so
// tests can assert ordering without racing wall-clock sleeps against the
// backoff timer.
type fakeStore struct {
	mu        sync.Mutex
	failNextN int
	commits   int
	attempts  []int64
	cursor    *vrclog.Cursor
	seq       int64
}

func (s *fakeStore) LatestCursor(_ context.Context) (*vrclog.Cursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursor, nil
}

func (s *fakeStore) CommitRecord(_ context.Context, commit store.RecordCommit) (store.CommitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts = append(s.attempts, commit.Record.Offset)
	if s.failNextN > 0 {
		s.failNextN--
		return store.CommitResult{}, errors.New("simulated db failure")
	}
	s.commits++
	cur := commit.Record.Cursor()
	s.cursor = &cur

	inserted := make([]observation.StoredObservation, 0, len(commit.Result.Observations))
	for _, obs := range commit.Result.Observations {
		s.seq++
		stored, err := observation.FromVrclogObservation(obs, commit.IngestedAt)
		if err != nil {
			return store.CommitResult{}, err
		}
		stored.Sequence = s.seq
		inserted = append(inserted, stored)
	}
	return store.CommitResult{InsertedObservations: inserted, Cursor: cur}, nil
}

func mkRecord(sourceID string, offset int64, line uint64, issue *vrclog.RecordIssue) vrclog.Record {
	return vrclog.Record{
		ID:         vrclog.RecordID("rec"),
		Time:       time.Now().UTC(),
		SourceID:   vrclog.SourceID(sourceID),
		Path:       "/tmp/" + sourceID,
		Offset:     offset,
		NextOffset: offset + 10,
		Line:       line,
		Issue:      issue,
	}
}

func playerJoinedResult(name string) vrclog.Result {
	return vrclog.Result{
		Observations: []vrclog.Observation{{
			ID:        vrclog.ObservationID("obs-" + name),
			Time:      time.Now().UTC(),
			AdapterID: "vrchat.core",
			RuleID:    "player_joined",
			Event:     vrclog.PlayerJoined{Player: vrclog.Player{DisplayName: name}},
		}},
	}
}

// --- tests ---

func TestRunner_EngineReceivesEveryRecord(t *testing.T) {
	src := &fakeSource{items: []recordOrErr{
		{record: mkRecord("src1", 0, 1, nil)},
		{record: mkRecord("src1", 10, 2, nil)},
		{record: mkRecord("src1", 20, 3, nil)},
	}}
	factory := &staticFactory{sources: []RecordSource{src}}
	engine := &fakeEngine{}
	st := &fakeStore{}

	r := NewRunner(factory, engine, st)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	waitFor(t, func() bool { return engine.seenCount() == 3 })
	cancel()
	<-done
}

func TestRunner_ZeroEventRecordAdvancesCursor(t *testing.T) {
	rec := mkRecord("src1", 0, 1, nil)
	src := &fakeSource{items: []recordOrErr{{record: rec}}}
	factory := &staticFactory{sources: []RecordSource{src}}
	engine := &fakeEngine{}
	st := &fakeStore{}

	r := NewRunner(factory, engine, st)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	waitFor(t, func() bool {
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.cursor != nil && st.cursor.Offset == rec.NextOffset
	})
	cancel()
	<-done
}

func TestRunner_RecordIssuePersistsDiagnosticAndAdvancesCursor(t *testing.T) {
	rec := mkRecord("src1", 0, 1, &vrclog.RecordIssue{Code: "bad_line", Message: "malformed"})
	src := &fakeSource{items: []recordOrErr{{record: rec}}}
	factory := &staticFactory{sources: []RecordSource{src}}

	// Use the real vrclog Engine: it produces a DiagnosticRecordIssue for
	// records with a non-nil Issue, without invoking any adapter.
	realEngine, err := vrclog.NewEngine(vrclog.NewVRChatAdapter())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	s := openTestStoreIngest(t)
	r := NewRunner(factory, realEngine, s)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	waitFor(t, func() bool {
		count, err := s.CountDiagnostics(context.Background())
		return err == nil && count == 1
	})

	cursor, err := s.LatestCursor(context.Background())
	if err != nil {
		t.Fatalf("LatestCursor: %v", err)
	}
	if cursor == nil || cursor.Offset != rec.NextOffset {
		t.Fatalf("cursor = %+v, want offset %d (RecordIssue must still advance cursor)", cursor, rec.NextOffset)
	}

	cancel()
	<-done
}

func TestRunner_AdapterErrorPersistsDiagnosticAndContinues(t *testing.T) {
	rec1 := mkRecord("src1", 0, 1, nil)
	rec2 := mkRecord("src1", 10, 2, nil)
	src := &fakeSource{items: []recordOrErr{{record: rec1}, {record: rec2}}}
	factory := &staticFactory{sources: []RecordSource{src}}

	engine := &fakeEngine{resultFn: func(rec vrclog.Record) vrclog.Result {
		if rec.Offset == 0 {
			return vrclog.Result{Diagnostics: []vrclog.Diagnostic{{
				Code:      vrclog.DiagnosticAdapterError,
				Message:   "adapter exploded",
				AdapterID: "vrchat.core",
				Record:    vrclog.RecordRef{ID: rec.ID, SourceID: rec.SourceID, Offset: rec.Offset, Line: rec.Line},
			}}}
		}
		return playerJoinedResult("Alice")
	}}

	s := openTestStoreIngest(t)
	r := NewRunner(factory, engine, s)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	waitFor(t, func() bool {
		diagCount, err := s.CountDiagnostics(context.Background())
		if err != nil {
			return false
		}
		items, _, err := s.ListObservations(context.Background(), store.ObservationQuery{})
		return err == nil && diagCount == 1 && len(items) == 1
	})

	cancel()
	<-done
}

func TestRunner_DBFailureDoesNotConsumeNextRecord(t *testing.T) {
	rec1 := mkRecord("src1", 0, 1, nil)
	rec2 := mkRecord("src1", 10, 2, nil)
	src := &fakeSource{items: []recordOrErr{{record: rec1}, {record: rec2}}}
	factory := &staticFactory{sources: []RecordSource{src}}
	engine := &fakeEngine{}
	st := &fakeStore{failNextN: 2}

	r := NewRunner(factory, engine, st, WithDBRetryDelays(1*time.Millisecond, 5*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	waitFor(t, func() bool {
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.commits == 2
	})
	if n := engine.seenCount(); n != 2 {
		t.Fatalf("engine.seenCount() = %d after recovery, want 2", n)
	}

	// Inspect the recorded attempt order: the two simulated failures plus
	// the eventual success must all be for record 1 (offset 0) before
	// record 2 (offset 10) is ever attempted — proving the Runner retried
	// the same record in place rather than skipping ahead while the DB
	// was failing.
	st.mu.Lock()
	attempts := append([]int64(nil), st.attempts...)
	st.mu.Unlock()

	wantPrefix := []int64{rec1.Offset, rec1.Offset, rec1.Offset}
	if len(attempts) < len(wantPrefix)+1 {
		t.Fatalf("attempts = %v, want at least %d entries", attempts, len(wantPrefix)+1)
	}
	for i, want := range wantPrefix {
		if attempts[i] != want {
			t.Fatalf("attempts[%d] = %d, want %d (offset of record 1): full log %v", i, attempts[i], want, attempts)
		}
	}
	if attempts[len(wantPrefix)] != rec2.Offset {
		t.Fatalf("attempts[%d] = %d, want %d (offset of record 2 only after record 1 committed): full log %v",
			len(wantPrefix), attempts[len(wantPrefix)], rec2.Offset, attempts)
	}

	cancel()
	<-done
}

func TestRunner_SourceRetryResumesLastCommittedCursor(t *testing.T) {
	rec1 := mkRecord("src1", 0, 1, nil)
	failingSrc := &fakeSource{items: []recordOrErr{
		{record: rec1},
		{err: errors.New("fatal source error")},
	}}
	replacementSrc := &fakeSource{}

	factory := &staticFactory{sources: []RecordSource{failingSrc, replacementSrc}}
	engine := &fakeEngine{}
	st := &fakeStore{}

	r := NewRunner(factory, engine, st, WithSourceRetryDelays(1*time.Millisecond, 5*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	waitFor(t, func() bool { return factory.callCount() >= 2 })

	cursor := factory.lastCursor()
	if cursor == nil || cursor.Offset != rec1.NextOffset {
		t.Fatalf("factory was rebuilt with cursor %+v, want offset %d (last committed cursor)", cursor, rec1.NextOffset)
	}

	cancel()
	<-done
}

// TestRunner_ObservationConflictDoesNotBlockIngestForever pins the Round-3
// adversarial-review fix: a deterministic ErrObservationConflict must not
// be retried with backoff forever (which would permanently halt ingest).
// The conflicting Observation is dropped (with a Diagnostic recorded) and
// the rest of the Record's Observations still commit, advancing the cursor.
func TestRunner_ObservationConflictDoesNotBlockIngestForever(t *testing.T) {
	rec1 := mkRecord("src1", 0, 1, nil)
	src := &fakeSource{items: []recordOrErr{{record: rec1}}}
	factory := &staticFactory{sources: []RecordSource{src}}

	s := openTestStoreIngest(t)

	// Seed a stored Observation whose ID the live Engine output will later
	// collide with, but with different content.
	seedRec := mkRecord("src0", 0, 1, nil)
	seedObs := vrclog.Observation{
		ID:        "obs-conflict",
		Time:      time.Now().UTC(),
		AdapterID: "vrchat.core",
		RuleID:    "player_joined",
		Event:     vrclog.PlayerJoined{Player: vrclog.Player{DisplayName: "Original"}},
	}
	if _, err := s.CommitRecord(context.Background(), store.RecordCommit{
		Record:     seedRec,
		Result:     vrclog.Result{Observations: []vrclog.Observation{seedObs}},
		IngestedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed commit: %v", err)
	}

	engine := &fakeEngine{resultFn: func(rec vrclog.Record) vrclog.Result {
		return vrclog.Result{Observations: []vrclog.Observation{
			{
				ID: "obs-conflict", Time: rec.Time, AdapterID: "vrchat.core", RuleID: "player_joined",
				Event: vrclog.PlayerJoined{Player: vrclog.Player{DisplayName: "Different"}},
			},
			{
				ID: "obs-valid", Time: rec.Time, AdapterID: "vrchat.core", RuleID: "player_joined",
				Event: vrclog.PlayerJoined{Player: vrclog.Player{DisplayName: "Valid"}},
			},
		}}
	}}

	r := NewRunner(factory, engine, s, WithDBRetryDelays(1*time.Millisecond, 5*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	// If the conflict were retried forever, obs-valid would never commit.
	waitFor(t, func() bool {
		items, _, err := s.ListObservations(context.Background(), store.ObservationQuery{})
		if err != nil {
			return false
		}
		for _, it := range items {
			if string(it.ID) == "obs-valid" {
				return true
			}
		}
		return false
	})

	diagCount, err := s.CountDiagnostics(context.Background())
	if err != nil {
		t.Fatalf("CountDiagnostics: %v", err)
	}
	if diagCount == 0 {
		t.Fatal("expected a diagnostic recording the dropped conflicting observation")
	}

	cursor, err := s.LatestCursor(context.Background())
	if err != nil {
		t.Fatalf("LatestCursor: %v", err)
	}
	if cursor == nil || cursor.Offset != rec1.NextOffset {
		t.Fatalf("cursor = %+v, want offset %d (must advance past the conflicting record)", cursor, rec1.NextOffset)
	}

	cancel()
	<-done
}

func TestRunner_CancellationCleanlyStops(t *testing.T) {
	factory := &staticFactory{sources: []RecordSource{&fakeSource{}}}
	engine := &fakeEngine{}
	st := &fakeStore{}

	r := NewRunner(factory, engine, st)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() returned %v, want nil on clean cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not stop within 2s of context cancellation")
	}

	if got := r.Status().State; got != StateStopped {
		t.Fatalf("Status().State = %s, want %s", got, StateStopped)
	}
}

// openTestStoreIngest opens a real temp-file SQLite store for ingest tests
// that need genuine CommitRecord/diagnostics/cursor behavior.
func openTestStoreIngest(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "ingest-test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
