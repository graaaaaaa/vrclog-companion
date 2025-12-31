package notify

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/derive"
	"github.com/graaaaa/vrclog-companion/internal/event"
)

// FakeTimerHandle implements TimerHandle for testing.
type FakeTimerHandle struct {
	mu      sync.Mutex
	stopped bool
	onFire  func()
}

func (h *FakeTimerHandle) Stop() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	stopped := !h.stopped
	h.stopped = true
	return stopped
}

func (h *FakeTimerHandle) Fire() {
	h.mu.Lock()
	stopped := h.stopped
	onFire := h.onFire
	h.mu.Unlock()

	if !stopped && onFire != nil {
		onFire()
	}
}

// FakeTimerFactory creates fake timers for testing.
type FakeTimerFactory struct {
	mu      sync.Mutex
	handles []*FakeTimerHandle
}

func (f *FakeTimerFactory) AfterFunc() AfterFunc {
	return func(d time.Duration, fn func()) TimerHandle {
		h := &FakeTimerHandle{onFire: fn}
		f.mu.Lock()
		f.handles = append(f.handles, h)
		f.mu.Unlock()
		return h
	}
}

func (f *FakeTimerFactory) FireAll() {
	f.mu.Lock()
	handles := append([]*FakeTimerHandle(nil), f.handles...)
	f.mu.Unlock()

	for _, h := range handles {
		h.Fire()
	}
}

func (f *FakeTimerFactory) LastHandle() *FakeTimerHandle {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.handles) == 0 {
		return nil
	}
	return f.handles[len(f.handles)-1]
}

// MockSender implements Sender for testing.
type MockSender struct {
	mu         sync.Mutex
	calls      []DiscordPayload
	result     SendResult
	retryAfter time.Duration
	sendCh     chan struct{} // Notifies when Send is called
}

func NewMockSender() *MockSender {
	return &MockSender{
		result: SendOK,
		sendCh: make(chan struct{}, 10),
	}
}

func (m *MockSender) Send(ctx context.Context, payload DiscordPayload) (SendResult, time.Duration) {
	m.mu.Lock()
	m.calls = append(m.calls, payload)
	result := m.result
	retryAfter := m.retryAfter
	m.mu.Unlock()

	// Notify waiters
	select {
	case m.sendCh <- struct{}{}:
	default:
	}
	return result, retryAfter
}

func (m *MockSender) SetResult(r SendResult, retryAfter time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.result = r
	m.retryAfter = retryAfter
}

func (m *MockSender) Calls() []DiscordPayload {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]DiscordPayload(nil), m.calls...)
}

func (m *MockSender) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// waitSend waits for a send notification with timeout.
func waitSend(t *testing.T, m *MockSender) {
	t.Helper()
	select {
	case <-m.sendCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for send")
	}
}

func ptr(s string) *string { return &s }

func makeJoinEvent(name string) *derive.DerivedEvent {
	return &derive.DerivedEvent{
		Type: derive.DerivedPlayerJoined,
		Event: &event.Event{
			Type:       event.TypePlayerJoin,
			PlayerName: ptr(name),
			Ts:         time.Now(),
		},
	}
}

func makeLeaveEvent(name string) *derive.DerivedEvent {
	return &derive.DerivedEvent{
		Type: derive.DerivedPlayerLeft,
		Event: &event.Event{
			Type:       event.TypePlayerLeft,
			PlayerName: ptr(name),
			Ts:         time.Now(),
		},
	}
}

func makeWorldEvent(worldName string) *derive.DerivedEvent {
	return &derive.DerivedEvent{
		Type: derive.DerivedWorldChanged,
		Event: &event.Event{
			Type:      event.TypeWorldJoin,
			WorldName: ptr(worldName),
			WorldID:   ptr("wrld_123"),
			Ts:        time.Now(),
		},
	}
}

func TestNotifier_BatchesEvents(t *testing.T) {
	timerFactory := &FakeTimerFactory{}
	sender := NewMockSender()

	n := NewNotifier(sender, 3, FilterConfig{
		NotifyOnJoin:      true,
		NotifyOnLeave:     true,
		NotifyOnWorldJoin: true,
	}, WithAfterFunc(timerFactory.AfterFunc()))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start notifier
	done := make(chan struct{})
	go func() {
		n.Run(ctx)
		close(done)
	}()

	// Enqueue multiple events
	n.Enqueue(makeJoinEvent("Alice"))
	n.Enqueue(makeJoinEvent("Bob"))
	n.Enqueue(makeLeaveEvent("Charlie"))

	// Wait for events to be queued
	time.Sleep(50 * time.Millisecond)

	// No sends yet (timer hasn't fired)
	if sender.CallCount() != 0 {
		t.Errorf("expected 0 calls before timer, got %d", sender.CallCount())
	}

	// Fire the timer
	timerFactory.FireAll()

	// Wait for send (no more flaky time.Sleep)
	waitSend(t, sender)

	// Should have one batched send
	if sender.CallCount() != 1 {
		t.Fatalf("expected 1 batched call, got %d", sender.CallCount())
	}

	// Verify batch contains all events (2 embeds: joins + leaves)
	calls := sender.Calls()
	if len(calls) == 0 {
		t.Fatal("expected at least 1 call")
	}
	if len(calls[0].Embeds) != 2 {
		t.Errorf("expected 2 embeds (joins + leaves), got %d", len(calls[0].Embeds))
	}

	cancel()
	<-done
}

func TestNotifier_FilterConfig(t *testing.T) {
	timerFactory := &FakeTimerFactory{}
	sender := NewMockSender()

	// Only notify on joins
	n := NewNotifier(sender, 3, FilterConfig{
		NotifyOnJoin:      true,
		NotifyOnLeave:     false,
		NotifyOnWorldJoin: false,
	}, WithAfterFunc(timerFactory.AfterFunc()))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		n.Run(ctx)
		close(done)
	}()

	// Enqueue different event types
	n.Enqueue(makeJoinEvent("Alice"))
	n.Enqueue(makeLeaveEvent("Bob"))
	n.Enqueue(makeWorldEvent("Test World"))

	time.Sleep(50 * time.Millisecond)
	timerFactory.FireAll()
	waitSend(t, sender)

	// Should have one call
	if sender.CallCount() != 1 {
		t.Fatalf("expected 1 call, got %d", sender.CallCount())
	}

	// Should only have join embed
	calls := sender.Calls()
	if len(calls) == 0 {
		t.Fatal("expected at least 1 call")
	}
	if len(calls[0].Embeds) != 1 {
		t.Errorf("expected 1 embed (join only), got %d", len(calls[0].Embeds))
	}
	if calls[0].Embeds[0].Title != "Player Joined" {
		t.Errorf("expected 'Player Joined', got %q", calls[0].Embeds[0].Title)
	}

	cancel()
	<-done
}

func TestNotifier_BackoffOn429(t *testing.T) {
	timerFactory := &FakeTimerFactory{}
	sender := NewMockSender()
	sender.SetResult(SendRetryable, 5*time.Second)

	n := NewNotifier(sender, 3, FilterConfig{
		NotifyOnJoin: true,
	}, WithAfterFunc(timerFactory.AfterFunc()))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		n.Run(ctx)
		close(done)
	}()

	// First batch
	n.Enqueue(makeJoinEvent("Alice"))
	time.Sleep(50 * time.Millisecond)
	timerFactory.FireAll()
	waitSend(t, sender)

	// Should have attempted to send
	if sender.CallCount() != 1 {
		t.Fatalf("expected 1 call, got %d", sender.CallCount())
	}

	// Enqueue another event during backoff
	n.Enqueue(makeJoinEvent("Bob"))
	time.Sleep(50 * time.Millisecond)
	timerFactory.FireAll()
	// Short sleep - we expect NO send during backoff, can't wait on channel
	time.Sleep(100 * time.Millisecond)

	// Should not have sent (still in backoff)
	if sender.CallCount() != 1 {
		t.Errorf("expected 1 call (backoff should prevent send), got %d", sender.CallCount())
	}

	cancel()
	<-done
}

func TestNotifier_StopsOnFatal(t *testing.T) {
	timerFactory := &FakeTimerFactory{}
	sender := NewMockSender()
	sender.SetResult(SendFatal, 0)

	n := NewNotifier(sender, 3, FilterConfig{
		NotifyOnJoin: true,
	}, WithAfterFunc(timerFactory.AfterFunc()))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		n.Run(ctx)
		close(done)
	}()

	// First event
	n.Enqueue(makeJoinEvent("Alice"))
	time.Sleep(50 * time.Millisecond)
	timerFactory.FireAll()
	waitSend(t, sender)

	// Check status
	status := n.Status()
	if !status.Disabled {
		t.Error("expected notifier to be disabled")
	}
	if status.DisabledReason == "" {
		t.Error("expected disabled reason to be set")
	}

	// Subsequent events should be ignored
	n.Enqueue(makeJoinEvent("Bob"))
	time.Sleep(50 * time.Millisecond)
	timerFactory.FireAll()
	// Short sleep - we expect NO send (notifier disabled), can't wait on channel
	time.Sleep(100 * time.Millisecond)

	// Should only have the first call
	if sender.CallCount() != 1 {
		t.Errorf("expected 1 call (subsequent ignored), got %d", sender.CallCount())
	}

	cancel()
	<-done
}

func TestNotifier_BestEffortFlushOnStop(t *testing.T) {
	timerFactory := &FakeTimerFactory{}
	sender := NewMockSender()

	n := NewNotifier(sender, 3, FilterConfig{
		NotifyOnJoin: true,
	}, WithAfterFunc(timerFactory.AfterFunc()))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		n.Run(ctx)
		close(done)
	}()

	// Enqueue event but don't fire timer
	n.Enqueue(makeJoinEvent("Alice"))
	time.Sleep(50 * time.Millisecond)

	// Stop (should trigger best-effort flush)
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := n.Stop(stopCtx); err != nil {
		t.Errorf("stop failed: %v", err)
	}

	<-done

	// Should have flushed on stop
	if sender.CallCount() != 1 {
		t.Errorf("expected 1 call (best-effort flush), got %d", sender.CallCount())
	}

	cancel()
}

func TestNotifier_NilEvent(t *testing.T) {
	sender := NewMockSender()
	n := NewNotifier(sender, 3, FilterConfig{
		NotifyOnJoin: true,
	})

	// Should not panic
	n.Enqueue(nil)

	// Queue should be empty
	if n.QueueLength() != 0 {
		t.Errorf("expected 0 queue length, got %d", n.QueueLength())
	}
}

func TestBackoff_Calculation(t *testing.T) {
	cfg := DefaultBackoffConfig

	// First attempt
	d0 := CalculateBackoff(0, cfg)
	if d0 < 800*time.Millisecond || d0 > 1200*time.Millisecond {
		t.Errorf("attempt 0: expected ~1s, got %v", d0)
	}

	// Second attempt (should be ~2s)
	d1 := CalculateBackoff(1, cfg)
	if d1 < 1600*time.Millisecond || d1 > 2400*time.Millisecond {
		t.Errorf("attempt 1: expected ~2s, got %v", d1)
	}

	// Many attempts should cap at MaxDelay
	d100 := CalculateBackoff(100, cfg)
	if d100 > cfg.MaxDelay+time.Duration(float64(cfg.MaxDelay)*cfg.JitterFactor) {
		t.Errorf("attempt 100: expected <= MaxDelay + jitter, got %v", d100)
	}
}

func TestPayload_BuildPayloads(t *testing.T) {
	events := []*derive.DerivedEvent{
		makeWorldEvent("Test World"),
		makeJoinEvent("Alice"),
		makeJoinEvent("Bob"),
		makeLeaveEvent("Charlie"),
	}

	payloads := BuildPayloads(events)
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}

	// Should have 3 embeds: world + joins + leaves
	if len(payloads[0].Embeds) != 3 {
		t.Errorf("expected 3 embeds, got %d", len(payloads[0].Embeds))
	}

	// Verify order: world first
	if payloads[0].Embeds[0].Title != "World Changed" {
		t.Errorf("expected first embed to be world, got %q", payloads[0].Embeds[0].Title)
	}

	// Joins should be batched
	if payloads[0].Embeds[1].Title != "Player Joined" {
		t.Errorf("expected second embed to be joins, got %q", payloads[0].Embeds[1].Title)
	}
}

func TestPayload_EmptyEvents(t *testing.T) {
	payloads := BuildPayloads(nil)
	if payloads != nil {
		t.Error("expected nil for empty events")
	}

	payloads = BuildPayloads([]*derive.DerivedEvent{})
	if payloads != nil {
		t.Error("expected nil for empty slice")
	}
}
