// Package projector turns persisted Observations into derived,
// user-facing state (current world, current players, recent media
// attempts). Observations are the source of truth; Projector state is
// rebuildable from the Store at any time via Manager.Rebuild.
package projector

import "time"

// Change is a domain-level state transition emitted when a Projector
// applies a live (non-rebuild) Observation. Notifications and other
// external side effects key off Change values, never off the raw
// canonical vrclog.Event — this keeps "what happened" (Observation) and
// "what it means for the user" (Change) cleanly separated.
type Change interface {
	changeMarker()
}

// WorldChanged is emitted exactly once per definitive world/instance
// transition. At is the triggering Observation's OccurredAt.
type WorldChanged struct {
	Current  *CurrentWorld
	Previous *CurrentWorld
	At       time.Time
}

func (WorldChanged) changeMarker() {}

// WorldNameUpdated is emitted when a pending "entering" name is merged into
// the current world without constituting a new transition.
type WorldNameUpdated struct {
	Current *CurrentWorld
	At      time.Time
}

func (WorldNameUpdated) changeMarker() {}

// PlayerJoined is emitted when a genuinely new player joins the current
// world (not a duplicate join, not a world-transition reset).
type PlayerJoined struct {
	Player PlayerInfo
	At     time.Time
}

func (PlayerJoined) changeMarker() {}

// PlayerLeft is emitted when a currently-tracked player leaves. At is the
// leave Observation's OccurredAt — Player.JoinedAt still reflects when they
// originally joined.
type PlayerLeft struct {
	Player PlayerInfo
	At     time.Time
}

func (PlayerLeft) changeMarker() {}

// MediaAttemptUpdated is emitted whenever a MediaAttempt is created or
// modified by a new resource/error Observation.
type MediaAttemptUpdated struct {
	Attempt *MediaAttempt
	At      time.Time
}

func (MediaAttemptUpdated) changeMarker() {}

// CurrentWorld is the WorldProjector's live state.
type CurrentWorld struct {
	ID         string
	Name       string
	InstanceID string
	JoinedAt   time.Time
}

// PlayerInfo is one entry in the PresenceProjector's live state.
type PlayerInfo struct {
	ID            string
	DisplayName   string
	JoinedAt      time.Time
	ObservationID string
}

// Snapshot is a point-in-time, thread-safe read of all Projector state.
type Snapshot struct {
	World               *CurrentWorld
	Players             []PlayerInfo
	LatestOpenableMedia *LatestOpenableMedia
}
