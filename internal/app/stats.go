package app

import (
	"context"
	"time"

	"github.com/vrclog/vrclog-companion/internal/projector"
	"github.com/vrclog/vrclog-companion/internal/store"
)

// StatsResult represents the response for /api/v1/stats. Fields are
// derived from canonical EventKind/AdapterID, not a fixed allowlist.
type StatsResult struct {
	TodayJoins            int            `json:"today_joins"`
	TodayLeaves           int            `json:"today_leaves"`
	TodayWorldChanges     int            `json:"today_world_changes"`
	RecentPlayers         []string       `json:"recent_players"`
	LastObservationAt     *string        `json:"last_observation_at,omitempty"`
	ObservationsByType    map[string]int `json:"observations_by_type"`
	ObservationsByAdapter map[string]int `json:"observations_by_adapter"`
	MediaAttempts         int            `json:"media_attempts"`
	MediaFailures         int            `json:"media_failures"`
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
	store   StatsStore
	manager *projector.Manager
}

// NewStatsService creates a new StatsService.
func NewStatsService(st StatsStore, manager *projector.Manager) *StatsService {
	return &StatsService{store: st, manager: manager}
}

// GetBasicStats retrieves today's statistics (local time) plus live media
// attempt/failure counts from the Projector Manager.
func (s *StatsService) GetBasicStats(ctx context.Context) (*StatsResult, error) {
	since, until := store.GetTodayBoundary()

	stats, err := s.store.GetBasicStats(ctx, since, until)
	if err != nil {
		return nil, err
	}

	result := &StatsResult{
		TodayJoins:            stats.JoinCount,
		TodayLeaves:           stats.LeaveCount,
		TodayWorldChanges:     stats.WorldChangeCount,
		RecentPlayers:         stats.RecentPlayers,
		LastObservationAt:     stats.LastObservationAt,
		ObservationsByType:    stats.ObservationsByType,
		ObservationsByAdapter: stats.ObservationsByAdapter,
	}

	if s.manager != nil {
		attempts := s.manager.RecentMedia(0)
		result.MediaAttempts = len(attempts)
		for _, a := range attempts {
			if a.Status == projector.MediaStatusFailed {
				result.MediaFailures++
			}
		}
	}

	return result, nil
}
