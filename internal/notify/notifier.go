package notify

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/derive"
)

// FilterConfig determines which events trigger notifications.
type FilterConfig struct {
	NotifyOnJoin      bool
	NotifyOnLeave     bool
	NotifyOnWorldJoin bool
}

// NotifierStatus represents the current status of the notifier.
type NotifierStatus struct {
	Disabled       bool
	DisabledReason string
	DisabledAt     time.Time
	LastError      error
}

// DefaultMaxQueueSize is the default maximum number of events to keep in queue.
const DefaultMaxQueueSize = 100

// Notifier batches and sends Discord notifications.
// It runs a dedicated goroutine for processing events.
type Notifier struct {
	sender       Sender
	afterFunc    AfterFunc
	batchDelay   time.Duration
	filter       FilterConfig
	logger       *slog.Logger
	maxQueueSize int

	eventCh chan *derive.DerivedEvent
	flushCh chan struct{}
	stopCh  chan struct{}
	doneCh  chan struct{}

	// internal state (protected by mu)
	mu          sync.Mutex
	queue       []*derive.DerivedEvent
	timerHandle TimerHandle
	status      NotifierStatus

	// backoff state
	backoffAttempt int
	backoffUntil   time.Time

	// Stop() protection
	stopOnce sync.Once
}

// NotifierOption configures a Notifier.
type NotifierOption func(*Notifier)

// WithAfterFunc sets the timer function (for testing).
func WithAfterFunc(af AfterFunc) NotifierOption {
	return func(n *Notifier) { n.afterFunc = af }
}

// WithNotifierLogger sets the logger.
func WithNotifierLogger(logger *slog.Logger) NotifierOption {
	return func(n *Notifier) { n.logger = logger }
}

// WithMaxQueueSize sets the maximum queue size.
func WithMaxQueueSize(size int) NotifierOption {
	return func(n *Notifier) {
		if size > 0 {
			n.maxQueueSize = size
		}
	}
}

// NewNotifier creates a new Notifier.
// Call Run() to start processing events.
func NewNotifier(sender Sender, batchDelaySec int, filter FilterConfig, opts ...NotifierOption) *Notifier {
	if batchDelaySec <= 0 {
		batchDelaySec = 3 // default
	}

	n := &Notifier{
		sender:       sender,
		afterFunc:    DefaultAfterFunc,
		batchDelay:   time.Duration(batchDelaySec) * time.Second,
		filter:       filter,
		logger:       slog.Default(),
		maxQueueSize: DefaultMaxQueueSize,
		eventCh:      make(chan *derive.DerivedEvent, 64),
		flushCh:      make(chan struct{}, 1),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
		queue:        make([]*derive.DerivedEvent, 0, 16),
	}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

// Run starts the notification processing loop.
// Blocks until Stop is called or ctx is cancelled.
func (n *Notifier) Run(ctx context.Context) {
	defer close(n.doneCh)

	for {
		select {
		case ev := <-n.eventCh:
			n.handleEvent(ev)

		case <-n.flushCh:
			n.flush(ctx)

		case <-n.stopCh:
			// Best-effort flush on stop
			n.flush(ctx)
			return

		case <-ctx.Done():
			// Best-effort flush on context cancel
			n.flush(context.Background()) // use fresh context for final flush
			return
		}
	}
}

// Enqueue adds a derived event to the notification queue.
// Events are filtered based on configuration.
// Safe to call from any goroutine.
// Non-blocking: if the channel is full, the event is dropped.
func (n *Notifier) Enqueue(event *derive.DerivedEvent) {
	if event == nil {
		return
	}

	// Check if disabled
	n.mu.Lock()
	disabled := n.status.Disabled
	n.mu.Unlock()
	if disabled {
		return
	}

	// Apply filter
	if !n.shouldNotify(event) {
		return
	}

	// Non-blocking send
	select {
	case n.eventCh <- event:
	default:
		n.logger.Warn("notification queue full, event dropped",
			"type", event.Type,
		)
	}
}

func (n *Notifier) shouldNotify(event *derive.DerivedEvent) bool {
	switch event.Type {
	case derive.DerivedPlayerJoined:
		return n.filter.NotifyOnJoin
	case derive.DerivedPlayerLeft:
		return n.filter.NotifyOnLeave
	case derive.DerivedWorldChanged:
		return n.filter.NotifyOnWorldJoin
	default:
		return false
	}
}

func (n *Notifier) handleEvent(ev *derive.DerivedEvent) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.queue = append(n.queue, ev)

	// Coalesce: remove older events for the same player/world
	n.coalesceQueueLocked()

	// Enforce queue size limit (drop oldest events)
	if len(n.queue) > n.maxQueueSize {
		dropped := len(n.queue) - n.maxQueueSize
		n.queue = n.queue[dropped:]
		n.logger.Warn("queue overflow, dropped old events", "dropped", dropped)
	}

	// Start batch timer if not already running
	if n.timerHandle == nil {
		n.timerHandle = n.afterFunc(n.batchDelay, n.triggerFlush)
	}
}

// coalesceQueueLocked removes duplicate events, keeping only the latest for each player/world.
// Must be called with mu held.
func (n *Notifier) coalesceQueueLocked() {
	if len(n.queue) <= 1 {
		return
	}

	// Track latest event for each key
	// WorldChanged: only keep latest
	// PlayerJoined/Left: keep latest per PlayerID
	seen := make(map[string]int) // key -> index in result
	result := make([]*derive.DerivedEvent, 0, len(n.queue))

	for _, ev := range n.queue {
		key := n.eventKey(ev)
		if key == "" {
			// Unknown event type, keep as-is
			result = append(result, ev)
			continue
		}

		if idx, exists := seen[key]; exists {
			// Replace older event with newer one
			result[idx] = ev
		} else {
			seen[key] = len(result)
			result = append(result, ev)
		}
	}

	n.queue = result
}

// eventKey returns a unique key for coalescing events.
func (n *Notifier) eventKey(ev *derive.DerivedEvent) string {
	switch ev.Type {
	case derive.DerivedWorldChanged:
		return "world"
	case derive.DerivedPlayerJoined, derive.DerivedPlayerLeft:
		// Use PlayerID if available, otherwise PlayerName
		if ev.Event != nil {
			if ev.Event.PlayerID != nil && *ev.Event.PlayerID != "" {
				return "player:" + *ev.Event.PlayerID
			}
			if ev.Event.PlayerName != nil {
				return "player:" + *ev.Event.PlayerName
			}
		}
		return ""
	default:
		return ""
	}
}

func (n *Notifier) triggerFlush() {
	// Non-blocking send to flush channel
	select {
	case n.flushCh <- struct{}{}:
	default:
	}
}

func (n *Notifier) flush(ctx context.Context) {
	n.mu.Lock()
	if len(n.queue) == 0 {
		n.timerHandle = nil
		n.mu.Unlock()
		return
	}

	// Check backoff - keep events in queue and schedule next flush
	if time.Now().Before(n.backoffUntil) {
		remaining := time.Until(n.backoffUntil)
		n.logger.Debug("in backoff period, keeping events in queue",
			"queue_size", len(n.queue),
			"backoff_until", n.backoffUntil,
			"remaining", remaining,
		)
		// Schedule flush for when backoff ends
		if n.timerHandle == nil {
			n.timerHandle = n.afterFunc(remaining, n.triggerFlush)
		}
		n.mu.Unlock()
		return
	}

	// Take ownership of queue
	events := n.queue
	n.queue = make([]*derive.DerivedEvent, 0, 16)
	n.timerHandle = nil
	n.mu.Unlock()

	// Build and send payloads
	payloads := BuildPayloads(events)
	for _, payload := range payloads {
		result, retryAfter := n.sender.Send(ctx, payload)
		n.handleSendResult(result, retryAfter)

		// Stop sending more payloads if we hit an error
		if result != SendOK {
			break
		}
	}
}

func (n *Notifier) handleSendResult(result SendResult, retryAfter time.Duration) {
	switch result {
	case SendOK:
		n.backoffAttempt = 0
		n.backoffUntil = time.Time{}

	case SendRetryable:
		n.backoffAttempt++
		delay := retryAfter
		if delay == 0 {
			delay = CalculateBackoff(n.backoffAttempt, DefaultBackoffConfig)
		}
		n.backoffUntil = time.Now().Add(delay)
		n.logger.Warn("Discord send failed, backing off",
			"attempt", n.backoffAttempt,
			"backoff_until", n.backoffUntil,
		)

	case SendFatal:
		// Stop trying (e.g., invalid webhook URL)
		n.mu.Lock()
		n.status.Disabled = true
		n.status.DisabledReason = "fatal error (invalid webhook or authentication failed)"
		n.status.DisabledAt = time.Now()
		n.mu.Unlock()
		n.logger.Error("Discord send fatal error, notifications disabled")
	}
}

// Stop stops the notifier gracefully.
// Waits for the run loop to finish or until ctx is cancelled.
// Safe to call multiple times.
func (n *Notifier) Stop(ctx context.Context) error {
	n.stopOnce.Do(func() {
		close(n.stopCh)
	})

	select {
	case <-n.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Status returns the current notifier status.
// Safe for concurrent use.
func (n *Notifier) Status() NotifierStatus {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.status
}

// QueueLength returns the current queue length (for testing/monitoring).
// Safe for concurrent use.
func (n *Notifier) QueueLength() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.queue)
}
