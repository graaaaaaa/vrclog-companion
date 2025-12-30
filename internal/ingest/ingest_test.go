package ingest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/event"
)

// MockEventSource implements EventSource for testing.
type MockEventSource struct {
	events chan Event
	errs   chan error
}

func NewMockEventSource() *MockEventSource {
	return &MockEventSource{
		events: make(chan Event, 10),
		errs:   make(chan error, 10),
	}
}

func (m *MockEventSource) Start(ctx context.Context) (<-chan Event, <-chan error, error) {
	// Create output channels that close when context is done or input channels close
	eventCh := make(chan Event)
	errCh := make(chan error)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		// Use nil-channel pattern
		events := m.events
		errs := m.errs

		for events != nil || errs != nil {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				select {
				case eventCh <- ev:
				case <-ctx.Done():
					return
				}
			case err, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				select {
				case errCh <- err:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return eventCh, errCh, nil
}

func (m *MockEventSource) SendEvent(ev Event) {
	m.events <- ev
}

func (m *MockEventSource) SendError(err error) {
	m.errs <- err
}

func (m *MockEventSource) Close() {
	close(m.events)
	close(m.errs)
}

// MockEventStore implements EventStore for testing.
type MockEventStore struct {
	mu              sync.Mutex
	insertedEvents  []*event.Event
	insertedErrors  []string
	insertEventErr  error
	insertFailErr   error
	nextID          int64
}

func NewMockEventStore() *MockEventStore {
	return &MockEventStore{nextID: 1}
}

func (m *MockEventStore) InsertEvent(ctx context.Context, e *event.Event) (int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.insertEventErr != nil {
		return 0, false, m.insertEventErr
	}

	id := m.nextID
	m.nextID++
	e.ID = id
	m.insertedEvents = append(m.insertedEvents, e)
	return id, true, nil
}

func (m *MockEventStore) InsertParseFailure(ctx context.Context, rawLine, errorMsg string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.insertFailErr != nil {
		return false, m.insertFailErr
	}

	m.insertedErrors = append(m.insertedErrors, rawLine)
	return true, nil
}

func (m *MockEventStore) GetInsertedEvents() []*event.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*event.Event(nil), m.insertedEvents...)
}

func (m *MockEventStore) GetInsertedErrors() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.insertedErrors...)
}

func TestIngester_HandleEvent(t *testing.T) {
	source := NewMockEventSource()
	store := NewMockEventStore()
	ingester := New(source, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start ingester in background
	done := make(chan error, 1)
	go func() {
		done <- ingester.Run(ctx)
	}()

	// Send an event
	rawLine := "2024.01.15 10:30:45 Log - [NetworkManager] OnPlayerJoined TestUser"
	ev := Event{
		Type:       "player_join",
		Timestamp:  time.Now(),
		PlayerName: "TestUser",
		PlayerID:   "usr_12345",
		RawLine:    rawLine,
	}
	source.SendEvent(ev)

	// Wait a bit for processing
	time.Sleep(50 * time.Millisecond)

	// Check event was inserted
	events := store.GetInsertedEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].Type != "player_join" {
		t.Errorf("expected type player_join, got %s", events[0].Type)
	}
	if events[0].DedupeKey != SHA256Hex(rawLine) {
		t.Errorf("expected dedupe key %s, got %s", SHA256Hex(rawLine), events[0].DedupeKey)
	}

	// Cancel and wait for shutdown
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for ingester to stop")
	}
}

func TestIngester_HandleParseError(t *testing.T) {
	source := NewMockEventSource()
	store := NewMockEventStore()
	ingester := New(source, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start ingester in background
	done := make(chan error, 1)
	go func() {
		done <- ingester.Run(ctx)
	}()

	// Send a parse error
	rawLine := "invalid log line format"
	parseErr := &ParseError{
		Line: rawLine,
		Err:  errors.New("parse failed"),
	}
	source.SendError(parseErr)

	// Wait a bit for processing
	time.Sleep(50 * time.Millisecond)

	// Check error was recorded
	errs := store.GetInsertedErrors()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}

	if errs[0] != rawLine {
		t.Errorf("expected raw line %q, got %q", rawLine, errs[0])
	}

	// Cancel and wait for shutdown
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for ingester to stop")
	}
}

func TestIngester_ContextCancellation(t *testing.T) {
	source := NewMockEventSource()
	store := NewMockEventStore()
	ingester := New(source, store)

	ctx, cancel := context.WithCancel(context.Background())

	// Start ingester in background
	done := make(chan error, 1)
	go func() {
		done <- ingester.Run(ctx)
	}()

	// Cancel immediately
	cancel()

	// Should return context.Canceled
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled on cancellation, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for ingester to stop on context cancellation")
	}
}

func TestIngester_SourceClose(t *testing.T) {
	source := NewMockEventSource()
	store := NewMockEventStore()
	ingester := New(source, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start ingester in background
	done := make(chan error, 1)
	go func() {
		done <- ingester.Run(ctx)
	}()

	// Close source channels
	source.Close()

	// Should return nil (clean shutdown when channels close)
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error when source closes, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for ingester to stop on source close")
	}
}

func TestIngester_Dedupe(t *testing.T) {
	rawLine := "2024.01.15 10:30:45 Log - [NetworkManager] OnPlayerJoined TestUser"

	// Same RawLine should produce same dedupe key
	ev1 := Event{
		Type:    "player_join",
		RawLine: rawLine,
	}
	ev2 := Event{
		Type:    "player_join",
		RawLine: rawLine,
	}

	storeEvent1 := ToStoreEvent(ev1)
	storeEvent2 := ToStoreEvent(ev2)

	if storeEvent1.DedupeKey != storeEvent2.DedupeKey {
		t.Errorf("same RawLine should produce same DedupeKey: %s != %s",
			storeEvent1.DedupeKey, storeEvent2.DedupeKey)
	}

	// Different RawLine should produce different dedupe key
	ev3 := Event{
		Type:    "player_join",
		RawLine: "different line",
	}
	storeEvent3 := ToStoreEvent(ev3)

	if storeEvent1.DedupeKey == storeEvent3.DedupeKey {
		t.Error("different RawLine should produce different DedupeKey")
	}
}

func TestParseError_Error(t *testing.T) {
	// With underlying error
	parseErr := &ParseError{
		Line: "bad line",
		Err:  errors.New("parse failed"),
	}
	if parseErr.Error() != "parse failed" {
		t.Errorf("Error() = %q, want %q", parseErr.Error(), "parse failed")
	}

	// Without underlying error
	parseErr2 := &ParseError{
		Line: "bad line",
		Err:  nil,
	}
	if parseErr2.Error() != "parse error" {
		t.Errorf("Error() = %q, want %q", parseErr2.Error(), "parse error")
	}
}

func TestParseError_Unwrap(t *testing.T) {
	underlyingErr := errors.New("underlying error")
	parseErr := &ParseError{
		Line: "bad line",
		Err:  underlyingErr,
	}

	if !errors.Is(parseErr, underlyingErr) {
		t.Error("errors.Is should return true for underlying error")
	}
}

// MockEventSourceWithSeparateClose allows closing events and errors channels independently.
type MockEventSourceWithSeparateClose struct {
	events chan Event
	errs   chan error
}

func NewMockEventSourceWithSeparateClose() *MockEventSourceWithSeparateClose {
	return &MockEventSourceWithSeparateClose{
		events: make(chan Event, 10),
		errs:   make(chan error, 10),
	}
}

func (m *MockEventSourceWithSeparateClose) Start(ctx context.Context) (<-chan Event, <-chan error, error) {
	eventCh := make(chan Event)
	errCh := make(chan error)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		events := m.events
		errs := m.errs

		for events != nil || errs != nil {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				select {
				case eventCh <- ev:
				case <-ctx.Done():
					return
				}
			case err, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				select {
				case errCh <- err:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return eventCh, errCh, nil
}

func (m *MockEventSourceWithSeparateClose) SendEvent(ev Event) {
	m.events <- ev
}

func (m *MockEventSourceWithSeparateClose) SendError(err error) {
	m.errs <- err
}

func (m *MockEventSourceWithSeparateClose) CloseEvents() {
	close(m.events)
}

func (m *MockEventSourceWithSeparateClose) CloseErrors() {
	close(m.errs)
}

func TestIngester_EventsCloseBeforeErrors(t *testing.T) {
	source := NewMockEventSourceWithSeparateClose()
	store := NewMockEventStore()
	ingester := New(source, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- ingester.Run(ctx)
	}()

	// Send an event first
	source.SendEvent(Event{Type: "player_join", RawLine: "line1"})
	time.Sleep(20 * time.Millisecond)

	// Close events channel, but keep errors open
	source.CloseEvents()

	// Send an error after events closed
	source.SendError(&ParseError{Line: "bad", Err: errors.New("test")})
	time.Sleep(20 * time.Millisecond)

	// Now close errors
	source.CloseErrors()

	// Should exit cleanly
	select {
	case err := <-done:
		// nil is expected when channels close (not context cancellation)
		if err != nil {
			t.Errorf("expected nil when channels close, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for ingester to stop")
	}

	// Verify both event and error were processed
	events := store.GetInsertedEvents()
	errs := store.GetInsertedErrors()

	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d", len(errs))
	}
}

func TestIngester_ErrorsCloseBeforeEvents(t *testing.T) {
	source := NewMockEventSourceWithSeparateClose()
	store := NewMockEventStore()
	ingester := New(source, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- ingester.Run(ctx)
	}()

	// Send an error first
	source.SendError(&ParseError{Line: "bad", Err: errors.New("test")})
	time.Sleep(20 * time.Millisecond)

	// Close errors channel, but keep events open
	source.CloseErrors()

	// Send an event after errors closed
	source.SendEvent(Event{Type: "player_join", RawLine: "line1"})
	time.Sleep(20 * time.Millisecond)

	// Now close events
	source.CloseEvents()

	// Should exit cleanly
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil when channels close, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for ingester to stop")
	}

	// Verify both error and event were processed
	events := store.GetInsertedEvents()
	errs := store.GetInsertedErrors()

	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d", len(errs))
	}
}

func TestToStoreEventWithClock(t *testing.T) {
	fixedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &fakeClock{t: fixedTime}

	ev := Event{
		Type:    "player_join",
		RawLine: "test line",
	}

	storeEvent := ToStoreEventWithClock(ev, clock)

	if !storeEvent.IngestedAt.Equal(fixedTime) {
		t.Errorf("IngestedAt = %v, want %v", storeEvent.IngestedAt, fixedTime)
	}
}

type fakeClock struct {
	t time.Time
}

func (f *fakeClock) Now() time.Time {
	return f.t
}
