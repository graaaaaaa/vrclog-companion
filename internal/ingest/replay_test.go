package ingest

import (
	"testing"
	"time"
)

func TestCalculateReplaySince_ZeroTime(t *testing.T) {
	// When lastEventTime is zero, should return current time.
	// Use the clock-based version for deterministic testing.
	fixedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &testClock{t: fixedTime}

	result := CalculateReplaySinceWithClock(time.Time{}, DefaultReplayRollback, clock)

	if !result.Equal(fixedTime) {
		t.Errorf("expected %v, got %v", fixedTime, result)
	}
}

func TestCalculateReplaySince_WithLastEvent(t *testing.T) {
	lastEvent := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	rollback := 5 * time.Minute

	result := CalculateReplaySince(lastEvent, rollback)

	expected := lastEvent.Add(-rollback)
	if !result.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestCalculateReplaySinceWithClock_ZeroTime(t *testing.T) {
	fixedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &testClock{t: fixedTime}

	result := CalculateReplaySinceWithClock(time.Time{}, DefaultReplayRollback, clock)

	if !result.Equal(fixedTime) {
		t.Errorf("expected %v, got %v", fixedTime, result)
	}
}

func TestCalculateReplaySinceWithClock_WithLastEvent(t *testing.T) {
	fixedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &testClock{t: fixedTime}

	lastEvent := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	rollback := 5 * time.Minute

	result := CalculateReplaySinceWithClock(lastEvent, rollback, clock)

	// Clock should not be used when lastEvent is not zero
	expected := lastEvent.Add(-rollback)
	if !result.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

type testClock struct {
	t time.Time
}

func (c *testClock) Now() time.Time {
	return c.t
}
