package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/store"
)

// stubStatsStore is a test double for StatsStore.
type stubStatsStore struct {
	gotSince time.Time
	gotUntil time.Time
	result   *store.BasicStats
	err      error
}

func (s *stubStatsStore) GetBasicStats(ctx context.Context, since, until time.Time) (*store.BasicStats, error) {
	s.gotSince = since
	s.gotUntil = until
	return s.result, s.err
}

func TestStatsService_GetBasicStats_Success(t *testing.T) {
	lastEvent := "2024-01-01T12:00:00.000000000Z"
	stub := &stubStatsStore{
		result: &store.BasicStats{
			JoinCount:        10,
			LeaveCount:       5,
			WorldChangeCount: 3,
			RecentPlayers:    []string{"alice", "bob"},
			LastEventAt:      &lastEvent,
		},
	}
	svc := NewStatsService(stub)

	result, err := svc.GetBasicStats(context.Background())
	if err != nil {
		t.Fatalf("GetBasicStats error: %v", err)
	}

	// Verify mapping from store.BasicStats to StatsResult
	if result.TodayJoins != 10 {
		t.Errorf("TodayJoins = %d, want 10", result.TodayJoins)
	}
	if result.TodayLeaves != 5 {
		t.Errorf("TodayLeaves = %d, want 5", result.TodayLeaves)
	}
	if result.TodayWorldChanges != 3 {
		t.Errorf("TodayWorldChanges = %d, want 3", result.TodayWorldChanges)
	}
	if len(result.RecentPlayers) != 2 {
		t.Errorf("len(RecentPlayers) = %d, want 2", len(result.RecentPlayers))
	}
	if result.LastEventAt == nil || *result.LastEventAt != lastEvent {
		t.Errorf("LastEventAt = %v, want %v", result.LastEventAt, lastEvent)
	}
}

func TestStatsService_GetBasicStats_DateRange(t *testing.T) {
	stub := &stubStatsStore{
		result: &store.BasicStats{
			RecentPlayers: []string{},
		},
	}
	svc := NewStatsService(stub)

	_, err := svc.GetBasicStats(context.Background())
	if err != nil {
		t.Fatalf("GetBasicStats error: %v", err)
	}

	// Verify that the date range is exactly 24 hours
	diff := stub.gotUntil.Sub(stub.gotSince)
	if diff != 24*time.Hour {
		t.Errorf("date range = %v, want 24h", diff)
	}

	// Verify that since is at midnight (0:00:00)
	if stub.gotSince.Hour() != 0 || stub.gotSince.Minute() != 0 || stub.gotSince.Second() != 0 {
		t.Errorf("since should be midnight, got %v", stub.gotSince)
	}
}

func TestStatsService_GetBasicStats_Error(t *testing.T) {
	stub := &stubStatsStore{
		err: errors.New("database error"),
	}
	svc := NewStatsService(stub)

	_, err := svc.GetBasicStats(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "database error" {
		t.Errorf("error = %q, want %q", err.Error(), "database error")
	}
}

func TestStatsService_GetBasicStats_NilLastEventAt(t *testing.T) {
	stub := &stubStatsStore{
		result: &store.BasicStats{
			JoinCount:        0,
			LeaveCount:       0,
			WorldChangeCount: 0,
			RecentPlayers:    []string{},
			LastEventAt:      nil, // No events
		},
	}
	svc := NewStatsService(stub)

	result, err := svc.GetBasicStats(context.Background())
	if err != nil {
		t.Fatalf("GetBasicStats error: %v", err)
	}

	if result.LastEventAt != nil {
		t.Errorf("LastEventAt = %v, want nil", result.LastEventAt)
	}
}

func TestStatsService_GetBasicStats_EmptyRecentPlayers(t *testing.T) {
	stub := &stubStatsStore{
		result: &store.BasicStats{
			RecentPlayers: []string{},
		},
	}
	svc := NewStatsService(stub)

	result, err := svc.GetBasicStats(context.Background())
	if err != nil {
		t.Fatalf("GetBasicStats error: %v", err)
	}

	if result.RecentPlayers == nil {
		t.Error("RecentPlayers should not be nil")
	}
	if len(result.RecentPlayers) != 0 {
		t.Errorf("len(RecentPlayers) = %d, want 0", len(result.RecentPlayers))
	}
}
