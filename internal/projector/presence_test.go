package projector

import (
	"testing"
	"time"

	vrclog "github.com/vrclog/vrclog-go"

	"github.com/vrclog/vrclog-companion/internal/observation"
)

func playerJoinedObs(id, name string, at time.Time) observation.StoredObservation {
	obs, _ := observation.FromVrclogObservation(vrclog.Observation{
		ID:   vrclog.ObservationID(id),
		Time: at,
		Event: vrclog.PlayerJoined{
			Player: vrclog.Player{ID: "usr_" + name, DisplayName: name},
		},
	}, at)
	obs.OccurredAt = at
	return obs
}

func playerLeftObs(id, name string, at time.Time) observation.StoredObservation {
	obs, _ := observation.FromVrclogObservation(vrclog.Observation{
		ID:   vrclog.ObservationID(id),
		Time: at,
		Event: vrclog.PlayerLeft{
			Player: vrclog.Player{ID: "usr_" + name, DisplayName: name},
		},
	}, at)
	obs.OccurredAt = at
	return obs
}

func TestPresence_JoinAdds(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()

	changes := applyOne(t, m, playerJoinedObs("p1", "Alice", base))
	if len(changes) != 1 {
		t.Fatalf("changes = %+v, want 1 PlayerJoined", changes)
	}
	if _, ok := changes[0].(PlayerJoined); !ok {
		t.Fatalf("changes[0] = %T, want PlayerJoined", changes[0])
	}

	snap := m.Snapshot()
	if len(snap.Players) != 1 || snap.Players[0].DisplayName != "Alice" {
		t.Fatalf("Players = %+v, want [Alice]", snap.Players)
	}
}

func TestPresence_DuplicateJoinNoOp(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()

	applyOne(t, m, playerJoinedObs("p1", "Alice", base))
	changes := applyOne(t, m, playerJoinedObs("p2", "Alice", base.Add(time.Second)))

	if len(changes) != 0 {
		t.Fatalf("duplicate join changes = %+v, want none", changes)
	}
	if snap := m.Snapshot(); len(snap.Players) != 1 {
		t.Fatalf("Players = %+v, want still 1", snap.Players)
	}
}

func TestPresence_LeftRemoves(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()

	applyOne(t, m, playerJoinedObs("p1", "Alice", base))
	changes := applyOne(t, m, playerLeftObs("p2", "Alice", base.Add(time.Second)))

	if len(changes) != 1 {
		t.Fatalf("changes = %+v, want 1 PlayerLeft", changes)
	}
	if _, ok := changes[0].(PlayerLeft); !ok {
		t.Fatalf("changes[0] = %T, want PlayerLeft", changes[0])
	}
	if snap := m.Snapshot(); len(snap.Players) != 0 {
		t.Fatalf("Players = %+v, want none", snap.Players)
	}
}

func TestPresence_UnknownLeftNoOp(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()

	changes := applyOne(t, m, playerLeftObs("p1", "Ghost", base))
	if len(changes) != 0 {
		t.Fatalf("unknown left changes = %+v, want none", changes)
	}
}

func TestPresence_KeyByIDFallbackName(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()

	// Player with no ID, keyed by trimmed display name.
	obs, _ := observation.FromVrclogObservation(vrclog.Observation{
		ID:    "p1",
		Time:  base,
		Event: vrclog.PlayerJoined{Player: vrclog.Player{DisplayName: " Bob "}},
	}, base)
	obs.OccurredAt = base
	applyOne(t, m, obs)

	leftObs, _ := observation.FromVrclogObservation(vrclog.Observation{
		ID:    "p2",
		Time:  base.Add(time.Second),
		Event: vrclog.PlayerLeft{Player: vrclog.Player{DisplayName: "Bob"}},
	}, base.Add(time.Second))
	leftObs.OccurredAt = base.Add(time.Second)
	changes := applyOne(t, m, leftObs)

	if len(changes) != 1 {
		t.Fatalf("left-by-name changes = %+v, want 1 PlayerLeft", changes)
	}
}

func TestPresence_WorldResetNoLeaveNotifications(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()

	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))
	applyOne(t, m, playerJoinedObs("p1", "Alice", base.Add(time.Second)))
	applyOne(t, m, playerJoinedObs("p2", "Bob", base.Add(2*time.Second)))

	changes := applyOne(t, m, joiningObs("j2", "wrld_2", "inst_2", base.Add(time.Minute)))

	for _, c := range changes {
		if _, ok := c.(PlayerLeft); ok {
			t.Fatalf("world transition must not emit PlayerLeft changes: %+v", changes)
		}
	}
	if snap := m.Snapshot(); len(snap.Players) != 0 {
		t.Fatalf("Players after reset = %+v, want none", snap.Players)
	}
}
