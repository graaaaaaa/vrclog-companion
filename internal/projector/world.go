package projector

import (
	"time"

	vrclog "github.com/vrclog/vrclog-go"
)

// worldNameMergeWindow bounds how far apart (by OccurredAt, not wall clock)
// a "world.entering_observed" name and a "world.joining_observed"
// transition may be to still be considered the same room-join event.
const worldNameMergeWindow = 15 * time.Second

type pendingWorldName struct {
	Name string
	At   time.Time
}

// worldProjector tracks the current world/instance and merges the
// human-readable room name (from entering_observed) with the definitive
// world/instance ID (from joining_observed), regardless of which arrives
// first.
type worldProjector struct {
	current *CurrentWorld
	pending *pendingWorldName
}

func newWorldProjector() *worldProjector {
	return &worldProjector{}
}

// applyJoining handles world.joining_observed, the definitive transition
// event. Same world/instance as current is a duplicate logical transition
// (no Change). A genuinely new world/instance replaces current, merging any
// still-fresh pending name, and emits exactly one WorldChanged.
func (w *worldProjector) applyJoining(ev vrclog.WorldJoiningObserved, occurredAt time.Time) []Change {
	if w.current != nil && w.current.ID == ev.World.ID && w.current.InstanceID == ev.World.InstanceID {
		return nil
	}

	name := ev.World.Name
	if name == "" && w.pending != nil && withinWindow(w.pending.At, occurredAt, worldNameMergeWindow) {
		name = w.pending.Name
	}
	w.pending = nil

	prevCopy := w.currentCopy()
	next := &CurrentWorld{
		ID:         ev.World.ID,
		Name:       name,
		InstanceID: ev.World.InstanceID,
		JoinedAt:   occurredAt,
	}
	w.current = next

	return []Change{WorldChanged{Current: *next, Previous: prevCopy, At: occurredAt}}
}

// applyEntering handles world.entering_observed. The name is always stored
// as pending (so a later joining within the merge window can pick it up).
// If a joining transition already happened within the merge window and the
// current world still has no name, the name is merged in-place and a
// WorldNameUpdated (not WorldChanged) is emitted — this must never produce
// a duplicate World notification.
func (w *worldProjector) applyEntering(ev vrclog.WorldEnteringObserved, occurredAt time.Time) []Change {
	w.pending = &pendingWorldName{Name: ev.World.Name, At: occurredAt}

	if w.current == nil || ev.World.Name == "" || w.current.Name != "" {
		return nil
	}
	if !withinWindow(w.current.JoinedAt, occurredAt, worldNameMergeWindow) {
		return nil
	}

	w.current.Name = ev.World.Name
	w.pending = nil

	return []Change{WorldNameUpdated{Current: *w.current, At: occurredAt}}
}

// currentCopy returns a defensive copy of the current world state, or nil.
func (w *worldProjector) currentCopy() *CurrentWorld {
	if w.current == nil {
		return nil
	}
	cp := *w.current
	return &cp
}

func withinWindow(reference, occurredAt time.Time, window time.Duration) bool {
	delta := occurredAt.Sub(reference)
	if delta < 0 {
		delta = -delta
	}
	return delta <= window
}
