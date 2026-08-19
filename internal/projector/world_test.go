package projector

import (
	"testing"
	"time"

	vrclog "github.com/vrclog/vrclog-go"

	"github.com/vrclog/vrclog-companion/internal/observation"
)

func joiningObs(id, worldID, instanceID string, at time.Time) observation.StoredObservation {
	obs, _ := observation.FromVrclogObservation(vrclog.Observation{
		ID:   vrclog.ObservationID(id),
		Time: at,
		Event: vrclog.WorldJoiningObserved{
			World: vrclog.World{ID: worldID, InstanceID: instanceID},
		},
	}, at)
	obs.OccurredAt = at
	return obs
}

func enteringObs(id, name string, at time.Time) observation.StoredObservation {
	obs, _ := observation.FromVrclogObservation(vrclog.Observation{
		ID:   vrclog.ObservationID(id),
		Time: at,
		Event: vrclog.WorldEnteringObserved{
			World: vrclog.World{Name: name},
		},
	}, at)
	obs.OccurredAt = at
	return obs
}

func applyOne(t *testing.T, m *Manager, obs observation.StoredObservation) []Change {
	t.Helper()
	changes, err := m.Apply(obs)
	if err != nil {
		t.Fatalf("Apply(%s): %v", obs.ID, err)
	}
	return changes
}

func TestWorld_EnteringThenJoiningMergesName(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()

	applyOne(t, m, enteringObs("e1", "Cool World", base))
	changes := applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base.Add(2*time.Second)))

	if len(changes) != 1 {
		t.Fatalf("changes = %+v, want exactly 1 WorldChanged", changes)
	}
	wc, ok := changes[0].(WorldChanged)
	if !ok {
		t.Fatalf("changes[0] = %T, want WorldChanged", changes[0])
	}
	if wc.Current.Name != "Cool World" {
		t.Fatalf("Current.Name = %q, want %q", wc.Current.Name, "Cool World")
	}
}

func TestWorld_JoiningThenEnteringMergesName(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()

	changes := applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))
	if len(changes) != 1 {
		t.Fatalf("joining changes = %+v, want 1 WorldChanged", changes)
	}
	if wc := changes[0].(WorldChanged); wc.Current.Name != "" {
		t.Fatalf("Current.Name before entering = %q, want empty", wc.Current.Name)
	}

	changes = applyOne(t, m, enteringObs("e1", "Cool World", base.Add(2*time.Second)))
	if len(changes) != 1 {
		t.Fatalf("entering changes = %+v, want 1 WorldNameUpdated", changes)
	}
	wnu, ok := changes[0].(WorldNameUpdated)
	if !ok {
		t.Fatalf("changes[0] = %T, want WorldNameUpdated (not a second WorldChanged)", changes[0])
	}
	if wnu.Current.Name != "Cool World" {
		t.Fatalf("Current.Name = %q, want %q", wnu.Current.Name, "Cool World")
	}

	snap := m.Snapshot()
	if snap.World == nil || snap.World.Name != "Cool World" {
		t.Fatalf("Snapshot().World = %+v, want Name=Cool World", snap.World)
	}
}

func TestWorld_PendingOlderThan15sNotMerged(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()

	applyOne(t, m, enteringObs("e1", "Stale Name", base))
	changes := applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base.Add(16*time.Second)))

	wc := changes[0].(WorldChanged)
	if wc.Current.Name != "" {
		t.Fatalf("Current.Name = %q, want empty (pending name older than 15s must not merge)", wc.Current.Name)
	}
}

func TestWorld_SameInstanceJoiningDuplicateNoSecondTransition(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()

	changes := applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))
	if len(changes) != 1 {
		t.Fatalf("first joining changes = %+v, want 1", changes)
	}

	changes = applyOne(t, m, joiningObs("j2", "wrld_1", "inst_1", base.Add(time.Second)))
	if len(changes) != 0 {
		t.Fatalf("duplicate joining changes = %+v, want none", changes)
	}
}

func TestWorld_NewInstanceEmitsOneWorldChanged(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()

	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))
	changes := applyOne(t, m, joiningObs("j2", "wrld_2", "inst_2", base.Add(time.Minute)))

	if len(changes) != 1 {
		t.Fatalf("changes = %+v, want exactly 1", changes)
	}
	wc, ok := changes[0].(WorldChanged)
	if !ok {
		t.Fatalf("changes[0] = %T, want WorldChanged", changes[0])
	}
	if wc.Previous == nil || wc.Previous.InstanceID != "inst_1" {
		t.Fatalf("Previous = %+v, want inst_1", wc.Previous)
	}
	if wc.Current.InstanceID != "inst_2" {
		t.Fatalf("Current = %+v, want inst_2", wc.Current)
	}
}

func TestWorld_EnteringAloneDoesNotClearPlayers(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()

	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))
	applyOne(t, m, playerJoinedObs("p1", "Alice", base.Add(time.Second)))

	applyOne(t, m, enteringObs("e1", "Room Name", base.Add(2*time.Second)))

	snap := m.Snapshot()
	if len(snap.Players) != 1 {
		t.Fatalf("Players = %+v, want 1 (entering alone must not clear players)", snap.Players)
	}
}

func TestWorld_JoiningClearsPlayersOnce(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()

	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))
	applyOne(t, m, playerJoinedObs("p1", "Alice", base.Add(time.Second)))
	if snap := m.Snapshot(); len(snap.Players) != 1 {
		t.Fatalf("Players before transition = %+v, want 1", snap.Players)
	}

	applyOne(t, m, joiningObs("j2", "wrld_2", "inst_2", base.Add(time.Minute)))

	snap := m.Snapshot()
	if len(snap.Players) != 0 {
		t.Fatalf("Players after transition = %+v, want 0", snap.Players)
	}
}

func TestWorld_LateNameUpdateNoDuplicateNotification(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()

	changes := applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))
	if _, ok := changes[0].(WorldChanged); !ok {
		t.Fatalf("expected WorldChanged, got %T", changes[0])
	}

	changes = applyOne(t, m, enteringObs("e1", "Late Name", base.Add(3*time.Second)))
	for _, c := range changes {
		if _, ok := c.(WorldChanged); ok {
			t.Fatalf("late name merge must not emit a second WorldChanged: %+v", changes)
		}
	}
}
