package notify

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/vrclog/vrclog-companion/internal/projector"
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
	sendCh     chan struct{}
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

func waitSend(t *testing.T, m *MockSender) {
	t.Helper()
	select {
	case <-m.sendCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for send")
	}
}

func makeJoinChange(name string) projector.Change {
	return projector.PlayerJoined{
		Player: projector.PlayerInfo{ID: "usr_" + name, DisplayName: name, JoinedAt: time.Now()},
		At:     time.Now(),
	}
}

func makeLeaveChange(name string) projector.Change {
	return projector.PlayerLeft{
		Player: projector.PlayerInfo{ID: "usr_" + name, DisplayName: name},
		At:     time.Now(),
	}
}

func makeWorldChange(worldName string) projector.Change {
	return projector.WorldChanged{
		Current: &projector.CurrentWorld{ID: "wrld_123", Name: worldName, InstanceID: "inst_1", JoinedAt: time.Now()},
		At:      time.Now(),
	}
}

func TestNotifier_BatchesChanges(t *testing.T) {
	timerFactory := &FakeTimerFactory{}
	sender := NewMockSender()

	n := NewNotifier(sender, 3, FilterConfig{
		NotifyOnJoin:      true,
		NotifyOnLeave:     true,
		NotifyOnWorldJoin: true,
	}, WithAfterFunc(timerFactory.AfterFunc()))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		n.Run(ctx)
		close(done)
	}()

	n.Enqueue(makeJoinChange("Alice"))
	n.Enqueue(makeJoinChange("Bob"))
	n.Enqueue(makeLeaveChange("Charlie"))

	time.Sleep(50 * time.Millisecond)

	if sender.CallCount() != 0 {
		t.Errorf("expected 0 calls before timer, got %d", sender.CallCount())
	}

	timerFactory.FireAll()
	waitSend(t, sender)

	if sender.CallCount() != 1 {
		t.Fatalf("expected 1 batched call, got %d", sender.CallCount())
	}

	calls := sender.Calls()
	if len(calls[0].Embeds) != 2 {
		t.Errorf("expected 2 embeds (joins + leaves), got %d", len(calls[0].Embeds))
	}
	if len(calls[0].AllowedMentions.Parse) != 0 {
		t.Errorf("expected empty AllowedMentions.Parse, got %v", calls[0].AllowedMentions.Parse)
	}

	cancel()
	<-done
}

func TestNotifier_FilterConfig(t *testing.T) {
	timerFactory := &FakeTimerFactory{}
	sender := NewMockSender()

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

	n.Enqueue(makeJoinChange("Alice"))
	n.Enqueue(makeLeaveChange("Bob"))
	n.Enqueue(makeWorldChange("Test World"))

	time.Sleep(50 * time.Millisecond)
	timerFactory.FireAll()
	waitSend(t, sender)

	if sender.CallCount() != 1 {
		t.Fatalf("expected 1 call, got %d", sender.CallCount())
	}

	calls := sender.Calls()
	if len(calls[0].Embeds) != 1 {
		t.Errorf("expected 1 embed (join only), got %d", len(calls[0].Embeds))
	}
	if calls[0].Embeds[0].Title != "Player Joined" {
		t.Errorf("expected 'Player Joined', got %q", calls[0].Embeds[0].Title)
	}

	cancel()
	<-done
}

func TestNotifier_MediaAndNameUpdateNeverNotify(t *testing.T) {
	timerFactory := &FakeTimerFactory{}
	sender := NewMockSender()

	n := NewNotifier(sender, 3, FilterConfig{
		NotifyOnJoin: true,
	}, WithAfterFunc(timerFactory.AfterFunc()))

	n.Enqueue(projector.WorldNameUpdated{Current: &projector.CurrentWorld{Name: "X"}, At: time.Now()})
	n.Enqueue(projector.MediaAttemptUpdated{Attempt: &projector.MediaAttempt{ID: "a1"}, At: time.Now()})

	if n.QueueLength() != 0 {
		t.Fatalf("QueueLength() = %d, want 0 (media/name-update must never notify)", n.QueueLength())
	}
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

	n.Enqueue(makeJoinChange("Alice"))
	time.Sleep(50 * time.Millisecond)
	timerFactory.FireAll()
	waitSend(t, sender)

	if sender.CallCount() != 1 {
		t.Fatalf("expected 1 call, got %d", sender.CallCount())
	}

	n.Enqueue(makeJoinChange("Bob"))
	time.Sleep(50 * time.Millisecond)
	timerFactory.FireAll()
	time.Sleep(100 * time.Millisecond)

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

	n.Enqueue(makeJoinChange("Alice"))
	time.Sleep(50 * time.Millisecond)
	timerFactory.FireAll()
	waitSend(t, sender)

	status := n.Status()
	if !status.Disabled {
		t.Error("expected notifier to be disabled")
	}
	if status.DisabledReason == "" {
		t.Error("expected disabled reason to be set")
	}

	n.Enqueue(makeJoinChange("Bob"))
	time.Sleep(50 * time.Millisecond)
	timerFactory.FireAll()
	time.Sleep(100 * time.Millisecond)

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

	n.Enqueue(makeJoinChange("Alice"))
	time.Sleep(50 * time.Millisecond)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := n.Stop(stopCtx); err != nil {
		t.Errorf("stop failed: %v", err)
	}

	<-done

	if sender.CallCount() != 1 {
		t.Errorf("expected 1 call (best-effort flush), got %d", sender.CallCount())
	}

	cancel()
}

func TestNotifier_NilChange(t *testing.T) {
	sender := NewMockSender()
	n := NewNotifier(sender, 3, FilterConfig{
		NotifyOnJoin: true,
	})

	n.Enqueue(nil)

	if n.QueueLength() != 0 {
		t.Errorf("expected 0 queue length, got %d", n.QueueLength())
	}
}

func TestBackoff_Calculation(t *testing.T) {
	cfg := DefaultBackoffConfig

	d0 := CalculateBackoff(0, cfg)
	if d0 < 800*time.Millisecond || d0 > 1200*time.Millisecond {
		t.Errorf("attempt 0: expected ~1s, got %v", d0)
	}

	d1 := CalculateBackoff(1, cfg)
	if d1 < 1600*time.Millisecond || d1 > 2400*time.Millisecond {
		t.Errorf("attempt 1: expected ~2s, got %v", d1)
	}

	d100 := CalculateBackoff(100, cfg)
	if d100 > cfg.MaxDelay+time.Duration(float64(cfg.MaxDelay)*cfg.JitterFactor) {
		t.Errorf("attempt 100: expected <= MaxDelay + jitter, got %v", d100)
	}
}

func TestPayload_BuildPayloads(t *testing.T) {
	changes := []projector.Change{
		makeWorldChange("Test World"),
		makeJoinChange("Alice"),
		makeJoinChange("Bob"),
		makeLeaveChange("Charlie"),
	}

	payloads := BuildPayloads(changes)
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}
	if len(payloads[0].Embeds) != 3 {
		t.Errorf("expected 3 embeds, got %d", len(payloads[0].Embeds))
	}
	if payloads[0].Embeds[0].Title != "World Changed" {
		t.Errorf("expected first embed to be world, got %q", payloads[0].Embeds[0].Title)
	}
	if payloads[0].Embeds[1].Title != "Player Joined" {
		t.Errorf("expected second embed to be joins, got %q", payloads[0].Embeds[1].Title)
	}
}

func TestPayload_EmptyChanges(t *testing.T) {
	if payloads := BuildPayloads(nil); payloads != nil {
		t.Error("expected nil for empty changes")
	}
	if payloads := BuildPayloads([]projector.Change{}); payloads != nil {
		t.Error("expected nil for empty slice")
	}
}

func TestPayload_SanitizesMentionsAndMarkdown(t *testing.T) {
	changes := []projector.Change{
		projector.PlayerJoined{
			Player: projector.PlayerInfo{DisplayName: "@everyone **hack** <@123>"},
			At:     time.Now(),
		},
	}
	payloads := BuildPayloads(changes)
	desc := payloads[0].Embeds[0].Description

	if want := "@everyone"; containsExact(desc, want) {
		t.Errorf("description still contains a live mention trigger %q: %q", want, desc)
	}
	if containsExact(desc, "**hack**") && !containsExact(desc, "\\*\\*hack\\*\\*") {
		t.Errorf("markdown bold was not escaped: %q", desc)
	}
}

func containsExact(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
