package api

import (
	"sync"
	"testing"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/event"
)

func TestHub_SubscribeUnsubscribe(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	// Subscribe
	sub := hub.Subscribe()
	if sub == nil {
		t.Fatal("Subscribe returned nil")
	}

	// Verify subscriber has open channels
	select {
	case <-sub.Done():
		t.Error("Done channel should not be closed")
	default:
	}

	// Unsubscribe
	hub.Unsubscribe(sub)

	// Wait for unsubscribe to complete
	select {
	case <-sub.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Done channel should be closed after unsubscribe")
	}
}

func TestHub_Publish(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	// Publish an event
	e := &event.Event{
		ID:   1,
		Type: event.TypePlayerJoin,
	}
	hub.Publish(e)

	// Verify event is received
	select {
	case received := <-sub.Events():
		if received.ID != e.ID {
			t.Errorf("expected event ID %d, got %d", e.ID, received.ID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event")
	}
}

func TestHub_PublishToMultipleSubscribers(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	const numSubscribers = 5
	subs := make([]*Subscriber, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		subs[i] = hub.Subscribe()
	}
	defer func() {
		for _, sub := range subs {
			hub.Unsubscribe(sub)
		}
	}()

	// Publish an event
	e := &event.Event{
		ID:   42,
		Type: event.TypeWorldJoin,
	}
	hub.Publish(e)

	// Verify all subscribers receive the event
	var wg sync.WaitGroup
	for i, sub := range subs {
		wg.Add(1)
		go func(i int, sub *Subscriber) {
			defer wg.Done()
			select {
			case received := <-sub.Events():
				if received.ID != e.ID {
					t.Errorf("subscriber %d: expected event ID %d, got %d", i, e.ID, received.ID)
				}
			case <-time.After(100 * time.Millisecond):
				t.Errorf("subscriber %d: timeout waiting for event", i)
			}
		}(i, sub)
	}
	wg.Wait()
}

func TestHub_PublishWithFullChannel(t *testing.T) {
	// Create hub with small buffer
	hub := NewHub(WithHubSubscriberBufferSize(1))
	go hub.Run()
	defer hub.Stop()

	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	// Fill the subscriber's buffer
	hub.Publish(&event.Event{ID: 1, Type: event.TypePlayerJoin})

	// Wait for event to be queued
	time.Sleep(10 * time.Millisecond)

	// This should be dropped (buffer is full and we're not reading)
	hub.Publish(&event.Event{ID: 2, Type: event.TypePlayerJoin})

	// Wait for potential drop
	time.Sleep(10 * time.Millisecond)

	// Read the first event
	select {
	case e := <-sub.Events():
		if e.ID != 1 {
			t.Errorf("expected first event ID 1, got %d", e.ID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for first event")
	}

	// Second event should not be in the channel (was dropped)
	select {
	case e := <-sub.Events():
		t.Errorf("did not expect second event, got ID %d", e.ID)
	case <-time.After(50 * time.Millisecond):
		// Expected - no second event
	}
}

func TestHub_PublishNil(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	// Publishing nil should be a no-op
	hub.Publish(nil)

	// No event should be received
	select {
	case e := <-sub.Events():
		t.Errorf("did not expect event, got %v", e)
	case <-time.After(50 * time.Millisecond):
		// Expected - no event
	}
}

func TestHub_UnsubscribeNil(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	// Should not panic
	hub.Unsubscribe(nil)
}

func TestHub_Stop(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	sub1 := hub.Subscribe()
	sub2 := hub.Subscribe()

	// Stop the hub
	hub.Stop()

	// All subscribers should have their channels closed
	select {
	case <-sub1.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("sub1 Done channel should be closed after Stop")
	}

	select {
	case <-sub2.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("sub2 Done channel should be closed after Stop")
	}
}

func TestHub_SubscribeAfterStop(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	hub.Stop()

	// Subscribing after stop should return a closed subscriber
	sub := hub.Subscribe()

	select {
	case <-sub.Done():
		// Expected - already closed
	default:
		t.Error("subscriber Done channel should be closed when hub is stopped")
	}
}

func TestHub_PublishAfterStop(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	hub.Stop()

	// Should not panic
	hub.Publish(&event.Event{ID: 1, Type: event.TypePlayerJoin})
}

func TestHub_ConcurrentOperations(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	const numGoroutines = 10
	var wg sync.WaitGroup

	// Spawn goroutines that subscribe, publish, and unsubscribe
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			sub := hub.Subscribe()

			// Publish some events
			for j := 0; j < 5; j++ {
				hub.Publish(&event.Event{
					ID:   int64(id*100 + j),
					Type: event.TypePlayerJoin,
				})
			}

			// Read available events
			timeout := time.After(50 * time.Millisecond)
		drain:
			for {
				select {
				case <-sub.Events():
				case <-timeout:
					break drain
				}
			}

			hub.Unsubscribe(sub)
		}(i)
	}

	wg.Wait()
}

func TestHub_StopIdempotent(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Calling Stop multiple times should not panic
	hub.Stop()
	hub.Stop()
	hub.Stop()

	// Test concurrent calls to Stop
	hub2 := NewHub()
	go hub2.Run()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hub2.Stop()
		}()
	}
	wg.Wait()
}
