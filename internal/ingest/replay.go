package ingest

import "time"

// DefaultReplayRollback is the default time to rollback from the last event.
const DefaultReplayRollback = 5 * time.Minute

// DefaultFirstRunRollback is the rollback for first-time runs.
// 24 hours ensures we capture events from the current VRChat session
// even if vrclog.exe is started after VRChat.
const DefaultFirstRunRollback = 24 * time.Hour

// CalculateReplaySince calculates the replay-since time based on the last event time.
// If lastEventTime is zero (no previous events), returns now - rollback.
// Otherwise, returns lastEventTime minus the rollback duration.
func CalculateReplaySince(lastEventTime time.Time, rollback time.Duration) time.Time {
	return CalculateReplaySinceWithClock(lastEventTime, rollback, nil)
}

// CalculateReplaySinceWithClock is like CalculateReplaySince but uses a custom clock.
// Useful for deterministic testing. If clk is nil, uses DefaultClock.
func CalculateReplaySinceWithClock(lastEventTime time.Time, rollback time.Duration, clk Clock) time.Time {
	if clk == nil {
		clk = DefaultClock
	}
	if lastEventTime.IsZero() {
		return clk.Now().Add(-rollback)
	}
	return lastEventTime.Add(-rollback)
}
