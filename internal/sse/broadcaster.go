// Package sse provides a generic Observation broadcaster for the SSE
// stream endpoint. Every Observation is published as one SSE event type;
// per-EventKind SSE event names are deliberately not used.
package sse

import (
	"sync"

	"github.com/vrclog/vrclog-companion/internal/observation"
)

const defaultSubscriberBufferSize = 32

// Subscriber is one SSE client's inbound channel.
type Subscriber struct {
	ch   chan observation.StoredObservation
	done chan struct{}
}

// Events returns the channel of newly broadcast Observations.
func (s *Subscriber) Events() <-chan observation.StoredObservation { return s.ch }

// Done is closed when the Subscriber has been disconnected (buffer
// overflow, or Broadcaster.Stop). The handler must stop reading and let the
// client reconnect with Last-Event-ID.
func (s *Subscriber) Done() <-chan struct{} { return s.done }

// Broadcaster fans out newly committed Observations to subscribed SSE
// clients. Publishing is non-blocking: a subscriber whose buffer is full is
// disconnected rather than allowed to stall ingest.
type Broadcaster struct {
	mu          sync.RWMutex
	subscribers map[*Subscriber]struct{}
	highWater   int64
	bufferSize  int
	stopped     bool
}

// NewBroadcaster creates a Broadcaster with the default per-subscriber
// buffer size.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[*Subscriber]struct{}),
		bufferSize:  defaultSubscriberBufferSize,
	}
}

// Subscribe registers a new Subscriber. The caller must call Unsubscribe
// when done. If the Broadcaster has been stopped, the returned Subscriber's
// Done channel is already closed.
func (b *Broadcaster) Subscribe() *Subscriber {
	sub := &Subscriber{
		ch:   make(chan observation.StoredObservation, b.bufferSize),
		done: make(chan struct{}),
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		close(sub.done)
		return sub
	}
	b.subscribers[sub] = struct{}{}
	return sub
}

// Unsubscribe removes a Subscriber. Safe to call more than once.
func (b *Broadcaster) Unsubscribe(sub *Subscriber) {
	if sub == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subscribers, sub)
}

// Broadcast publishes obs to every current Subscriber and advances the
// high-water sequence. Must only be called for newly inserted Observations,
// after their CommitRecord transaction has committed — never during
// startup rebuild and never for duplicates.
func (b *Broadcaster) Broadcast(obs observation.StoredObservation) {
	b.mu.Lock()
	if obs.Sequence > b.highWater {
		b.highWater = obs.Sequence
	}
	subs := make([]*Subscriber, 0, len(b.subscribers))
	for s := range b.subscribers {
		subs = append(subs, s)
	}
	b.mu.Unlock()

	for _, s := range subs {
		select {
		case s.ch <- obs:
		default:
			// Buffer full: disconnect rather than block ingest. The client
			// reconnects and recovers via Last-Event-ID backlog.
			b.disconnect(s)
		}
	}
}

func (b *Broadcaster) disconnect(s *Subscriber) {
	b.mu.Lock()
	_, ok := b.subscribers[s]
	delete(b.subscribers, s)
	b.mu.Unlock()
	if ok {
		close(s.done)
	}
}

// HighWaterSequence returns the highest sequence broadcast so far. It is
// in-memory only and resets to 0 on process restart, so it must NOT be
// used to bound SSE backlog delivery — that would silently truncate
// backlog to nothing immediately after a restart. The SSE handler uses
// Store.LatestSequence() (DB-backed) for that instead; this method exists
// only for tests.
func (b *Broadcaster) HighWaterSequence() int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.highWater
}

// Stop disconnects all current subscribers and rejects further
// subscriptions.
func (b *Broadcaster) Stop() {
	b.mu.Lock()
	b.stopped = true
	subs := b.subscribers
	b.subscribers = make(map[*Subscriber]struct{})
	b.mu.Unlock()

	for s := range subs {
		close(s.done)
	}
}
