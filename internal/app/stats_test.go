package app

import (
	"context"
	"testing"
	"time"

	"github.com/vrclog/vrclog-companion/internal/store"
)

type fakeStatsStore struct {
	stats *store.BasicStats
	err   error
}

func (f *fakeStatsStore) GetBasicStats(ctx context.Context, since, until time.Time) (*store.BasicStats, error) {
	return f.stats, f.err
}

func TestStatsService_GetBasicStats(t *testing.T) {
	fake := &fakeStatsStore{stats: &store.BasicStats{
		JoinCount:             2,
		LeaveCount:            1,
		WorldChangeCount:      1,
		RecentPlayers:         []string{"Alice", "Bob"},
		ObservationsByType:    map[string]int{"player.joined": 2},
		ObservationsByAdapter: map[string]int{"vrchat.core": 3},
	}}

	svc := NewStatsService(fake, nil)
	result, err := svc.GetBasicStats(context.Background())
	if err != nil {
		t.Fatalf("GetBasicStats: %v", err)
	}
	if result.TodayJoins != 2 || result.TodayLeaves != 1 || result.TodayWorldChanges != 1 {
		t.Fatalf("counts = %+v, want joins=2 leaves=1 world=1", result)
	}
	if len(result.RecentPlayers) != 2 {
		t.Fatalf("RecentPlayers = %v, want 2 entries", result.RecentPlayers)
	}
}
