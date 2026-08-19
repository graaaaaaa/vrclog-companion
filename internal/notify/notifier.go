package notify

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/vrclog/vrclog-companion/internal/projector"
)

// FilterConfig determines which Changes trigger notifications.
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

// DefaultMaxQueueSize is the default maximum number of Changes to keep in queue.
const DefaultMaxQueueSize = 100

// Notifier batches and sends Discord notifications for Projector Changes.
// It runs a dedicated goroutine for processing.
type Notifier struct {
	sender       Sender
	afterFunc    AfterFunc
	batchDelay   time.Duration
	filter       FilterConfig
	logger       *slog.Logger
	maxQueueSize int

	changeCh chan projector.Change
	flushCh  chan struct{}
	stopCh   chan struct{}
	doneCh   chan struct{}

	// internal state (protected by mu)
	mu          sync.Mutex
	queue       []projector.Change
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

// NewNotifier creates a new Notifier. Call Run() to start processing.
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
		changeCh:     make(chan projector.Change, 64),
		flushCh:      make(chan struct{}, 1),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
		queue:        make([]projector.Change, 0, 16),
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
		case c := <-n.changeCh:
			n.handleChange(c)

		case <-n.flushCh:
			n.flush(ctx)

		case <-n.stopCh:
			n.flush(ctx)
			return

		case <-ctx.Done():
			n.flush(context.Background())
			return
		}
	}
}

// Enqueue adds a Change to the notification queue. Changes are filtered
// based on configuration: only WorldChanged/PlayerJoined/PlayerLeft can
// ever notify — WorldNameUpdated and MediaAttemptUpdated never do (media
// URLs must never reach Discord). Safe to call from any goroutine.
// Non-blocking: if the channel is full, the Change is dropped.
func (n *Notifier) Enqueue(change projector.Change) {
	if change == nil {
		return
	}

	n.mu.Lock()
	disabled := n.status.Disabled
	n.mu.Unlock()
	if disabled {
		return
	}

	if !n.shouldNotify(change) {
		return
	}

	select {
	case n.changeCh <- change:
	default:
		n.logger.Warn("notification queue full, change dropped", "type", changeTypeName(change))
	}
}

func (n *Notifier) shouldNotify(change projector.Change) bool {
	switch change.(type) {
	case projector.PlayerJoined:
		return n.filter.NotifyOnJoin
	case projector.PlayerLeft:
		return n.filter.NotifyOnLeave
	case projector.WorldChanged:
		return n.filter.NotifyOnWorldJoin
	default:
		return false
	}
}

func changeTypeName(c projector.Change) string {
	switch c.(type) {
	case projector.WorldChanged:
		return "world_changed"
	case projector.WorldNameUpdated:
		return "world_name_updated"
	case projector.PlayerJoined:
		return "player_joined"
	case projector.PlayerLeft:
		return "player_left"
	case projector.MediaAttemptUpdated:
		return "media_attempt_updated"
	default:
		return "unknown"
	}
}

func (n *Notifier) handleChange(c projector.Change) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.queue = append(n.queue, c)
	n.coalesceQueueLocked()

	if len(n.queue) > n.maxQueueSize {
		dropped := len(n.queue) - n.maxQueueSize
		n.queue = n.queue[dropped:]
		n.logger.Warn("queue overflow, dropped old changes", "dropped", dropped)
	}

	if n.timerHandle == nil {
		n.timerHandle = n.afterFunc(n.batchDelay, n.triggerFlush)
	}
}

// coalesceQueueLocked removes duplicate Changes, keeping only the latest
// for each world/player key. Must be called with mu held.
func (n *Notifier) coalesceQueueLocked() {
	if len(n.queue) <= 1 {
		return
	}

	seen := make(map[string]int)
	result := make([]projector.Change, 0, len(n.queue))

	for _, c := range n.queue {
		key := changeCoalesceKey(c)
		if key == "" {
			result = append(result, c)
			continue
		}
		if idx, exists := seen[key]; exists {
			result[idx] = c
		} else {
			seen[key] = len(result)
			result = append(result, c)
		}
	}

	n.queue = result
}

func changeCoalesceKey(c projector.Change) string {
	switch v := c.(type) {
	case projector.WorldChanged:
		return "world"
	case projector.PlayerJoined:
		return "player:" + playerCoalesceKey(v.Player)
	case projector.PlayerLeft:
		return "player:" + playerCoalesceKey(v.Player)
	default:
		return ""
	}
}

func playerCoalesceKey(p projector.PlayerInfo) string {
	if p.ID != "" {
		return p.ID
	}
	return p.DisplayName
}

func (n *Notifier) triggerFlush() {
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

	if time.Now().Before(n.backoffUntil) {
		remaining := time.Until(n.backoffUntil)
		n.logger.Debug("in backoff period, keeping changes in queue",
			"queue_size", len(n.queue),
			"backoff_until", n.backoffUntil,
			"remaining", remaining,
		)
		if n.timerHandle == nil {
			n.timerHandle = n.afterFunc(remaining, n.triggerFlush)
		}
		n.mu.Unlock()
		return
	}

	changes := n.queue
	n.queue = make([]projector.Change, 0, 16)
	n.timerHandle = nil
	n.mu.Unlock()

	payloads := BuildPayloads(changes)
	for _, payload := range payloads {
		result, retryAfter := n.sender.Send(ctx, payload)
		n.handleSendResult(result, retryAfter)

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
		n.mu.Lock()
		n.status.Disabled = true
		n.status.DisabledReason = "fatal error (invalid webhook or authentication failed)"
		n.status.DisabledAt = time.Now()
		n.mu.Unlock()
		n.logger.Error("Discord send fatal error, notifications disabled")
	}
}

// Stop stops the notifier gracefully. Safe to call multiple times.
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

// Status returns the current notifier status. Safe for concurrent use.
func (n *Notifier) Status() NotifierStatus {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.status
}

// QueueLength returns the current queue length (for testing/monitoring).
func (n *Notifier) QueueLength() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.queue)
}
