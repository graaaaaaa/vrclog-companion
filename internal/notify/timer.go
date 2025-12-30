// Package notify provides Discord webhook notification functionality.
package notify

import "time"

// TimerHandle allows stopping a scheduled callback.
type TimerHandle interface {
	Stop() bool
}

// AfterFunc schedules f to run after d. Returns a handle to cancel.
type AfterFunc func(d time.Duration, f func()) TimerHandle

// realTimerHandle wraps *time.Timer to implement TimerHandle.
type realTimerHandle struct {
	timer *time.Timer
}

func (h *realTimerHandle) Stop() bool {
	return h.timer.Stop()
}

// DefaultAfterFunc uses the standard library's time.AfterFunc.
var DefaultAfterFunc AfterFunc = func(d time.Duration, f func()) TimerHandle {
	return &realTimerHandle{timer: time.AfterFunc(d, f)}
}
