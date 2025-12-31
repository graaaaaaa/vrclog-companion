package app

import (
	"context"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/store"
)

// StatsResult represents the response for stats/basic endpoint.
type StatsResult struct {
	TodayJoins        int      `json:"today_joins"`
	TodayLeaves       int      `json:"today_leaves"`
	TodayWorldChanges int      `json:"today_world_changes"`
	RecentPlayers     []string `json:"recent_players"`
	LastEventAt       *string  `json:"last_event_at,omitempty"`
}

// StatsUsecase defines the interface for stats operations.
type StatsUsecase interface {
	GetBasicStats(ctx context.Context) (*StatsResult, error)
}

// StatsStore defines the interface for stats data access.
type StatsStore interface {
	GetBasicStats(ctx context.Context, since, until time.Time) (*store.BasicStats, error)
}

// StatsService implements StatsUsecase.
type StatsService struct {
	store StatsStore
}

// NewStatsService creates a new StatsService.
func NewStatsService(store StatsStore) *StatsService {
	return &StatsService{store: store}
}

// GetBasicStats retrieves basic statistics for today (local time).
func (s *StatsService) GetBasicStats(ctx context.Context) (*StatsResult, error) {
	since, until := store.GetTodayBoundary()

	stats, err := s.store.GetBasicStats(ctx, since, until)
	if err != nil {
		return nil, err
	}

	return &StatsResult{
		TodayJoins:        stats.JoinCount,
		TodayLeaves:       stats.LeaveCount,
		TodayWorldChanges: stats.WorldChangeCount,
		RecentPlayers:     stats.RecentPlayers,
		LastEventAt:       stats.LastEventAt,
	}, nil
}
