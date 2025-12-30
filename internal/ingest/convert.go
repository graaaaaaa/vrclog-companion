package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/event"
)

// Clock provides time for deterministic testing.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// DefaultClock is used by the simple production API.
var DefaultClock Clock = realClock{}

// ToStoreEvent converts an ingest.Event to event.Event with SHA256 dedupe key.
// Uses DefaultClock for IngestedAt timestamp.
func ToStoreEvent(e Event) *event.Event {
	return ToStoreEventWithClock(e, DefaultClock)
}

// ToStoreEventWithClock allows deterministic tests by injecting a clock.
func ToStoreEventWithClock(e Event, clk Clock) *event.Event {
	dedupeKey := SHA256Hex(e.RawLine)
	return &event.Event{
		Ts:         e.Timestamp,
		Type:       e.Type,
		PlayerName: stringPtrIfNotEmpty(e.PlayerName),
		PlayerID:   stringPtrIfNotEmpty(e.PlayerID),
		WorldID:    stringPtrIfNotEmpty(e.WorldID),
		WorldName:  stringPtrIfNotEmpty(e.WorldName),
		InstanceID: stringPtrIfNotEmpty(e.InstanceID),
		DedupeKey:  dedupeKey,
		IngestedAt: clk.Now(),
	}
}

// SHA256Hex returns the SHA256 hash of the input string as a hex string.
func SHA256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// stringPtrIfNotEmpty returns a pointer to s if non-empty, otherwise nil.
func stringPtrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
