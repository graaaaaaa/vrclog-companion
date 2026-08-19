package sse

import (
	"testing"
	"time"

	"github.com/vrclog/vrclog-companion/internal/observation"
)

func TestBroadcaster_DeliversToSubscriber(t *testing.T) {
	b := NewBroadcaster()
	sub := b.Subscribe()
	defer b.Unsubscribe(sub)

	obs := observation.StoredObservation{Sequence: 1, ID: "o1"}
	b.Broadcast(obs)

	select {
	case got := <-sub.Events():
		if got.ID != "o1" {
			t.Fatalf("got.ID = %s, want o1", got.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestBroadcaster_HighWaterAdvances(t *testing.T) {
	b := NewBroadcaster()
	b.Broadcast(observation.StoredObservation{Sequence: 5})
	b.Broadcast(observation.StoredObservation{Sequence: 3}) // out of order, should not regress
	if hw := b.HighWaterSequence(); hw != 5 {
		t.Fatalf("HighWaterSequence() = %d, want 5", hw)
	}
}

func TestBroadcaster_OverflowDisconnects(t *testing.T) {
	b := NewBroadcaster()
	b.bufferSize = 1
	sub := b.Subscribe()

	b.Broadcast(observation.StoredObservation{Sequence: 1})
	b.Broadcast(observation.StoredObservation{Sequence: 2}) // buffer full -> disconnect

	select {
	case <-sub.Done():
	case <-time.After(time.Second):
		t.Fatal("expected subscriber to be disconnected on overflow")
	}
}

func TestBroadcaster_StopClosesAllSubscribers(t *testing.T) {
	b := NewBroadcaster()
	sub1 := b.Subscribe()
	sub2 := b.Subscribe()

	b.Stop()

	for _, sub := range []*Subscriber{sub1, sub2} {
		select {
		case <-sub.Done():
		case <-time.After(time.Second):
			t.Fatal("expected subscriber Done to be closed after Stop")
		}
	}

	// Subscribing after Stop returns an already-closed Subscriber.
	late := b.Subscribe()
	select {
	case <-late.Done():
	default:
		t.Fatal("expected post-Stop Subscribe to return an already-closed Subscriber")
	}
}
