package ingest

import "time"

// DefaultReplayRollback is the default time to rollback from the last event.
const DefaultReplayRollback = 5 * time.Minute

// CalculateReplaySince calculates the replay-since time based on the last event time.
// If lastEventTime is zero (no previous events), returns the current time.
// Otherwise, returns lastEventTime minus the rollback duration.
func CalculateReplaySince(lastEventTime time.Time, rollback time.Duration) time.Time {
	if lastEventTime.IsZero() {
		return time.Now()
	}
	return lastEventTime.Add(-rollback)
}

// CalculateReplaySinceWithClock is like CalculateReplaySince but uses a custom clock.
// Useful for deterministic testing. If clk is nil, uses DefaultClock.
func CalculateReplaySinceWithClock(lastEventTime time.Time, rollback time.Duration, clk Clock) time.Time {
	if lastEventTime.IsZero() {
		if clk == nil {
			clk = DefaultClock
		}
		return clk.Now()
	}
	return lastEventTime.Add(-rollback)
}
