package store

import (
	"context"
	"testing"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/event"
)

// insertTestEvent is a helper to insert events for stats tests.
func insertTestEvent(t *testing.T, st *Store, ts time.Time, typ string, playerName string, dedupeKey string) {
	t.Helper()
	evt := &event.Event{
		Ts:         ts,
		Type:       typ,
		DedupeKey:  dedupeKey,
		IngestedAt: ts,
	}
	if playerName != "" {
		evt.PlayerName = event.StringPtr(playerName)
	}
	if _, _, err := st.InsertEvent(context.Background(), evt); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
}

func TestGetBasicStats_Empty(t *testing.T) {
	st := openTestStore(t)
	defer st.Close()

	since := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)

	stats, err := st.GetBasicStats(context.Background(), since, until)
	if err != nil {
		t.Fatalf("GetBasicStats: %v", err)
	}

	// All counts should be 0
	if stats.JoinCount != 0 {
		t.Errorf("JoinCount = %d, want 0", stats.JoinCount)
	}
	if stats.LeaveCount != 0 {
		t.Errorf("LeaveCount = %d, want 0", stats.LeaveCount)
	}
	if stats.WorldChangeCount != 0 {
		t.Errorf("WorldChangeCount = %d, want 0", stats.WorldChangeCount)
	}

	// RecentPlayers should be empty (not nil)
	if stats.RecentPlayers == nil {
		t.Error("RecentPlayers should not be nil")
	}
	if len(stats.RecentPlayers) != 0 {
		t.Errorf("RecentPlayers = %v, want empty", stats.RecentPlayers)
	}

	// LastEventAt should be nil
	if stats.LastEventAt != nil {
		t.Errorf("LastEventAt = %v, want nil", *stats.LastEventAt)
	}
}

func TestGetBasicStats_Counts(t *testing.T) {
	st := openTestStore(t)
	defer st.Close()

	since := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)

	// Insert events within range
	insertTestEvent(t, st, since.Add(1*time.Hour), event.TypePlayerJoin, "player1", "k1")
	insertTestEvent(t, st, since.Add(2*time.Hour), event.TypePlayerJoin, "player2", "k2")
	insertTestEvent(t, st, since.Add(3*time.Hour), event.TypePlayerLeft, "player1", "k3")
	insertTestEvent(t, st, since.Add(4*time.Hour), event.TypeWorldJoin, "", "k4")

	stats, err := st.GetBasicStats(context.Background(), since, until)
	if err != nil {
		t.Fatalf("GetBasicStats: %v", err)
	}

	if stats.JoinCount != 2 {
		t.Errorf("JoinCount = %d, want 2", stats.JoinCount)
	}
	if stats.LeaveCount != 1 {
		t.Errorf("LeaveCount = %d, want 1", stats.LeaveCount)
	}
	if stats.WorldChangeCount != 1 {
		t.Errorf("WorldChangeCount = %d, want 1", stats.WorldChangeCount)
	}
}

func TestGetBasicStats_Boundary(t *testing.T) {
	st := openTestStore(t)
	defer st.Close()

	since := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)

	// Event exactly at since (should be included)
	insertTestEvent(t, st, since, event.TypePlayerJoin, "p1", "k1")

	// Event exactly at until (should be excluded: ts < until)
	insertTestEvent(t, st, until, event.TypePlayerJoin, "p2", "k2")

	// Event within range
	insertTestEvent(t, st, since.Add(12*time.Hour), event.TypePlayerJoin, "p3", "k3")

	stats, err := st.GetBasicStats(context.Background(), since, until)
	if err != nil {
		t.Fatalf("GetBasicStats: %v", err)
	}

	// Only 2 events should be counted (at since and within range, not at until)
	if stats.JoinCount != 2 {
		t.Errorf("JoinCount = %d, want 2", stats.JoinCount)
	}
}

func TestGetBasicStats_RecentPlayersLimit(t *testing.T) {
	st := openTestStore(t)
	defer st.Close()

	base := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)

	// Insert 7 players
	for i := 0; i < 7; i++ {
		name := string(rune('A' + i))
		insertTestEvent(t, st, base.Add(time.Duration(i)*time.Minute), event.TypePlayerJoin, "player"+name, "k"+name)
	}

	stats, err := st.GetBasicStats(context.Background(), base, base.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("GetBasicStats: %v", err)
	}

	// Should only return 5 recent players
	if len(stats.RecentPlayers) != 5 {
		t.Errorf("len(RecentPlayers) = %d, want 5", len(stats.RecentPlayers))
	}

	// Most recent player should be first (playerG inserted last)
	if stats.RecentPlayers[0] != "playerG" {
		t.Errorf("RecentPlayers[0] = %q, want %q", stats.RecentPlayers[0], "playerG")
	}
}

func TestGetBasicStats_RecentPlayersUnique(t *testing.T) {
	st := openTestStore(t)
	defer st.Close()

	base := time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC)

	// Same player joins multiple times
	insertTestEvent(t, st, base.Add(1*time.Minute), event.TypePlayerJoin, "alice", "k1")
	insertTestEvent(t, st, base.Add(2*time.Minute), event.TypePlayerJoin, "bob", "k2")
	insertTestEvent(t, st, base.Add(3*time.Minute), event.TypePlayerJoin, "alice", "k3") // Duplicate

	stats, err := st.GetBasicStats(context.Background(), base, base.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("GetBasicStats: %v", err)
	}

	// Should have 2 unique players
	if len(stats.RecentPlayers) != 2 {
		t.Errorf("len(RecentPlayers) = %d, want 2 (unique)", len(stats.RecentPlayers))
	}
}

func TestGetBasicStats_LastEventAt(t *testing.T) {
	st := openTestStore(t)
	defer st.Close()

	base := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	lastTs := base.Add(5 * time.Hour)

	insertTestEvent(t, st, base.Add(1*time.Hour), event.TypePlayerJoin, "p1", "k1")
	insertTestEvent(t, st, lastTs, event.TypePlayerJoin, "p2", "k2")
	insertTestEvent(t, st, base.Add(3*time.Hour), event.TypePlayerJoin, "p3", "k3")

	stats, err := st.GetBasicStats(context.Background(), base, base.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("GetBasicStats: %v", err)
	}

	if stats.LastEventAt == nil {
		t.Fatal("LastEventAt should not be nil")
	}

	// Parse the timestamp and verify it's the latest
	lastEventTime, err := time.Parse(TimeFormat, *stats.LastEventAt)
	if err != nil {
		t.Fatalf("Failed to parse LastEventAt: %v", err)
	}

	if !lastEventTime.Equal(lastTs.UTC()) {
		t.Errorf("LastEventAt = %v, want %v", lastEventTime, lastTs.UTC())
	}
}

func TestGetBasicStats_LastEventAtGlobal(t *testing.T) {
	st := openTestStore(t)
	defer st.Close()

	queryRange := time.Date(2024, 1, 6, 0, 0, 0, 0, time.UTC)

	// Insert event outside query range (later)
	laterEvent := queryRange.Add(48 * time.Hour)
	insertTestEvent(t, st, laterEvent, event.TypePlayerJoin, "future", "k1")

	// Insert event within query range
	insertTestEvent(t, st, queryRange.Add(1*time.Hour), event.TypePlayerJoin, "current", "k2")

	stats, err := st.GetBasicStats(context.Background(), queryRange, queryRange.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("GetBasicStats: %v", err)
	}

	// LastEventAt should be the global latest (not filtered by date range)
	if stats.LastEventAt == nil {
		t.Fatal("LastEventAt should not be nil")
	}

	lastEventTime, err := time.Parse(TimeFormat, *stats.LastEventAt)
	if err != nil {
		t.Fatalf("Failed to parse LastEventAt: %v", err)
	}

	if !lastEventTime.Equal(laterEvent.UTC()) {
		t.Errorf("LastEventAt = %v, want %v (global latest)", lastEventTime, laterEvent.UTC())
	}
}

func TestGetTodayBoundary(t *testing.T) {
	since, until := GetTodayBoundary()

	// Check that since is midnight
	if since.Hour() != 0 || since.Minute() != 0 || since.Second() != 0 || since.Nanosecond() != 0 {
		t.Errorf("since should be midnight, got %v", since)
	}

	// Check that until is exactly 24 hours after since
	diff := until.Sub(since)
	if diff != 24*time.Hour {
		t.Errorf("until - since = %v, want 24h", diff)
	}
}
