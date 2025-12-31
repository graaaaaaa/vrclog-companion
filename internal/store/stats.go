package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/event"
)

// BasicStats holds aggregated statistics for a time period.
type BasicStats struct {
	JoinCount       int       `json:"today_joins"`
	LeaveCount      int       `json:"today_leaves"`
	WorldChangeCount int      `json:"today_world_changes"`
	RecentPlayers   []string  `json:"recent_players"`
	LastEventAt     *string   `json:"last_event_at,omitempty"`
}

// GetBasicStats retrieves basic statistics for the specified time range.
// Uses local time for "today" calculation.
func (s *Store) GetBasicStats(ctx context.Context, since, until time.Time) (*BasicStats, error) {
	stats := &BasicStats{
		RecentPlayers: []string{},
	}

	// Format times for SQL query
	sinceStr := since.UTC().Format(TimeFormat)
	untilStr := until.UTC().Format(TimeFormat)

	// Get aggregated counts in a single query
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN type = ? THEN 1 ELSE 0 END), 0) AS join_count,
			COALESCE(SUM(CASE WHEN type = ? THEN 1 ELSE 0 END), 0) AS leave_count,
			COALESCE(SUM(CASE WHEN type = ? THEN 1 ELSE 0 END), 0) AS world_count
		FROM events
		WHERE ts >= ? AND ts < ?
	`, event.TypePlayerJoin, event.TypePlayerLeft, event.TypeWorldJoin, sinceStr, untilStr).
		Scan(&stats.JoinCount, &stats.LeaveCount, &stats.WorldChangeCount)
	if err != nil {
		return nil, err
	}

	// Get recent unique players (last 5 who joined)
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT player_name FROM events
		WHERE type = ? AND player_name IS NOT NULL AND player_name != ''
		ORDER BY ts DESC
		LIMIT 5
	`, event.TypePlayerJoin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		stats.RecentPlayers = append(stats.RecentPlayers, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Get last event timestamp
	var lastTs sql.NullString
	err = s.db.QueryRowContext(ctx, `
		SELECT ts FROM events
		ORDER BY ts DESC, id DESC
		LIMIT 1
	`).Scan(&lastTs)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if lastTs.Valid {
		stats.LastEventAt = &lastTs.String
	}

	return stats, nil
}

// GetTodayBoundary returns the start and end times for "today" in local time.
func GetTodayBoundary() (since, until time.Time) {
	now := time.Now()
	// Get start of today in local time
	y, m, d := now.Date()
	loc := now.Location()
	since = time.Date(y, m, d, 0, 0, 0, 0, loc)
	// End of today (start of tomorrow)
	until = since.AddDate(0, 0, 1)
	return since, until
}
