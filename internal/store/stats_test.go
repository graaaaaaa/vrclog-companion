package store

import (
	"context"
	"testing"
	"time"

	vrclog "github.com/vrclog/vrclog-go"
)

func commitPlayerJoined(t *testing.T, s *Store, id, name string, at time.Time) {
	t.Helper()
	rec := makeRecord("src1", int64(len(id)), uint64(len(id)), at)
	obs := makePlayerJoined(id, rec, name)
	if _, err := s.CommitRecord(context.Background(), RecordCommit{
		Record:     rec,
		Result:     vrclog.Result{Observations: []vrclog.Observation{obs}},
		IngestedAt: at,
	}); err != nil {
		t.Fatalf("CommitRecord(%s): %v", id, err)
	}
}

// TestGetBasicStats_RecentPlayersRespectsWindow pins the fix: RecentPlayers
// must only include players who joined within [since, until), matching the
// same window as JoinCount/LeaveCount/WorldChangeCount. Before the fix, the
// RecentPlayers query ignored since/until entirely and scanned the whole
// observations table, so "today's stats" could show players from days ago.
func TestGetBasicStats_RecentPlayersRespectsWindow(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	yesterday := time.Now().UTC().Add(-24 * time.Hour)
	today := time.Now().UTC()

	commitPlayerJoined(t, s, "obs-old", "OldPlayer", yesterday)
	commitPlayerJoined(t, s, "obs-new", "NewPlayer", today)

	since := today.Add(-1 * time.Hour)
	until := today.Add(1 * time.Hour)

	stats, err := s.GetBasicStats(ctx, since, until)
	if err != nil {
		t.Fatalf("GetBasicStats: %v", err)
	}

	for _, name := range stats.RecentPlayers {
		if name == "OldPlayer" {
			t.Errorf("RecentPlayers leaked a player outside the [since,until) window: %v", stats.RecentPlayers)
		}
	}
	found := false
	for _, name := range stats.RecentPlayers {
		if name == "NewPlayer" {
			found = true
		}
	}
	if !found {
		t.Errorf("RecentPlayers missing a player inside the window: %v", stats.RecentPlayers)
	}
}
