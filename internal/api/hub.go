// Package api provides HTTP API server functionality.
package api

import (
	"log/slog"
	"sync"

	"github.com/graaaaa/vrclog-companion/internal/event"
)

const (
	defaultSubscriberBufferSize = 16
	defaultBroadcastBufferSize  = 64
)

// Subscriber represents an SSE client connection.
type Subscriber struct {
	events chan *event.Event
	done   chan struct{}
}

// Events returns the channel for receiving events.
func (s *Subscriber) Events() <-chan *event.Event {
	return s.events
}

// Done returns a channel that is closed when the subscriber is unsubscribed.
func (s *Subscriber) Done() <-chan struct{} {
	return s.done
}

// Hub manages SSE subscribers and broadcasts events.
// Uses 1 goroutine + channel management pattern for thread safety.
type Hub struct {
	register   chan *Subscriber
	unregister chan *Subscriber
	broadcast  chan *event.Event
	stop       chan struct{}
	stopped    chan struct{}
	stopOnce   sync.Once

	subscriberBufferSize int
	logger               *slog.Logger
}

// HubOption configures a Hub.
type HubOption func(*Hub)

// WithHubSubscriberBufferSize sets the buffer size for subscriber event channels.
func WithHubSubscriberBufferSize(size int) HubOption {
	return func(h *Hub) {
		if size > 0 {
			h.subscriberBufferSize = size
		}
	}
}

// WithHubLogger sets the logger for the Hub.
func WithHubLogger(logger *slog.Logger) HubOption {
	return func(h *Hub) {
		if logger != nil {
			h.logger = logger
		}
	}
}

// NewHub creates a new SSE hub.
// Call Run() to start the hub's event loop.
func NewHub(opts ...HubOption) *Hub {
	h := &Hub{
		register:             make(chan *Subscriber),
		unregister:           make(chan *Subscriber),
		broadcast:            make(chan *event.Event, defaultBroadcastBufferSize),
		stop:                 make(chan struct{}),
		stopped:              make(chan struct{}),
		subscriberBufferSize: defaultSubscriberBufferSize,
		logger:               slog.Default(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Run starts the hub's event loop.
// This method blocks until Stop() is called.
// Should be called in a goroutine: go hub.Run()
func (h *Hub) Run() {
	clients := make(map[*Subscriber]struct{})
	defer close(h.stopped)

	for {
		select {
		case sub := <-h.register:
			clients[sub] = struct{}{}
			h.logger.Debug("subscriber registered", "count", len(clients))

		case sub := <-h.unregister:
			if _, ok := clients[sub]; ok {
				delete(clients, sub)
				close(sub.done)
				close(sub.events)
				h.logger.Debug("subscriber unregistered", "count", len(clients))
			}

		case e := <-h.broadcast:
			for sub := range clients {
				select {
				case sub.events <- e:
					// Event sent successfully
				default:
					// Channel full, drop event for this subscriber
					h.logger.Warn("subscriber channel full, event dropped",
						"event_id", e.ID,
						"event_type", e.Type,
					)
				}
			}

		case <-h.stop:
			// Close all subscriber channels
			for sub := range clients {
				close(sub.done)
				close(sub.events)
			}
			return
		}
	}
}

// Stop stops the hub's event loop.
// Blocks until the hub has fully stopped.
// Safe to call multiple times (idempotent).
func (h *Hub) Stop() {
	h.stopOnce.Do(func() {
		close(h.stop)
	})
	<-h.stopped
}

// Subscribe creates a new subscriber.
// The caller must call Unsubscribe when done.
func (h *Hub) Subscribe() *Subscriber {
	sub := &Subscriber{
		events: make(chan *event.Event, h.subscriberBufferSize),
		done:   make(chan struct{}),
	}

	select {
	case h.register <- sub:
		return sub
	case <-h.stopped:
		// Hub is stopped, return a closed subscriber
		close(sub.done)
		close(sub.events)
		return sub
	}
}

// Unsubscribe removes a subscriber.
func (h *Hub) Unsubscribe(sub *Subscriber) {
	if sub == nil {
		return
	}

	select {
	case h.unregister <- sub:
	case <-h.stopped:
		// Hub is stopped, nothing to do
	}
}

// Publish sends an event to all subscribers.
// Non-blocking: if the broadcast channel is full, the event is dropped.
func (h *Hub) Publish(e *event.Event) {
	if e == nil {
		return
	}

	select {
	case h.broadcast <- e:
		// Event queued for broadcast
	case <-h.stopped:
		// Hub is stopped
	default:
		// Broadcast channel full
		h.logger.Warn("broadcast channel full, event dropped",
			"event_id", e.ID,
			"event_type", e.Type,
		)
	}
}
