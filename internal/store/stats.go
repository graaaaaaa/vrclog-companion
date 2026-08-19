package store

import (
	"context"
	"database/sql"
	"time"

	vrclog "github.com/vrclog/vrclog-go"
)

// BasicStats holds aggregated statistics for a time period, derived from
// canonical EventKind rather than a fixed type allowlist.
type BasicStats struct {
	JoinCount             int
	LeaveCount            int
	WorldChangeCount      int
	RecentPlayers         []string
	LastObservationAt     *string
	ObservationsByType    map[string]int
	ObservationsByAdapter map[string]int
}

// GetBasicStats retrieves aggregated statistics for the [since, until) time
// range, plus the global (not time-bounded) last observation timestamp.
func (s *Store) GetBasicStats(ctx context.Context, since, until time.Time) (*BasicStats, error) {
	stats := &BasicStats{
		RecentPlayers:         []string{},
		ObservationsByType:    make(map[string]int),
		ObservationsByAdapter: make(map[string]int),
	}

	sinceStr := since.UTC().Format(TimeFormat)
	untilStr := until.UTC().Format(TimeFormat)

	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN type = ? THEN 1 ELSE 0 END), 0) AS join_count,
			COALESCE(SUM(CASE WHEN type = ? THEN 1 ELSE 0 END), 0) AS leave_count,
			COALESCE(SUM(CASE WHEN type = ? THEN 1 ELSE 0 END), 0) AS world_count
		FROM observations
		WHERE occurred_at >= ? AND occurred_at < ?
	`, string(vrclog.EventKindPlayerJoined), string(vrclog.EventKindPlayerLeft), string(vrclog.EventKindWorldJoiningObserved), sinceStr, untilStr).
		Scan(&stats.JoinCount, &stats.LeaveCount, &stats.WorldChangeCount)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT json_extract(payload_json, '$.player.display_name')
		FROM observations
		WHERE type = ?
		ORDER BY sequence DESC
		LIMIT 5
	`, string(vrclog.EventKindPlayerJoined))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name sql.NullString
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		if name.Valid && name.String != "" {
			stats.RecentPlayers = append(stats.RecentPlayers, name.String)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	typeRows, err := s.db.QueryContext(ctx, `
		SELECT type, COUNT(*) FROM observations
		WHERE occurred_at >= ? AND occurred_at < ?
		GROUP BY type
	`, sinceStr, untilStr)
	if err != nil {
		return nil, err
	}
	for typeRows.Next() {
		var t string
		var n int
		if err := typeRows.Scan(&t, &n); err != nil {
			typeRows.Close()
			return nil, err
		}
		stats.ObservationsByType[t] = n
	}
	if err := typeRows.Err(); err != nil {
		typeRows.Close()
		return nil, err
	}
	typeRows.Close()

	adapterRows, err := s.db.QueryContext(ctx, `
		SELECT adapter_id, COUNT(*) FROM observations
		WHERE occurred_at >= ? AND occurred_at < ?
		GROUP BY adapter_id
	`, sinceStr, untilStr)
	if err != nil {
		return nil, err
	}
	for adapterRows.Next() {
		var a string
		var n int
		if err := adapterRows.Scan(&a, &n); err != nil {
			adapterRows.Close()
			return nil, err
		}
		stats.ObservationsByAdapter[a] = n
	}
	if err := adapterRows.Err(); err != nil {
		adapterRows.Close()
		return nil, err
	}
	adapterRows.Close()

	var lastTs sql.NullString
	err = s.db.QueryRowContext(ctx, `
		SELECT occurred_at FROM observations
		ORDER BY sequence DESC
		LIMIT 1
	`).Scan(&lastTs)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if lastTs.Valid {
		stats.LastObservationAt = &lastTs.String
	}

	return stats, nil
}

// GetTodayBoundary returns the start and end times for "today" in local time.
func GetTodayBoundary() (since, until time.Time) {
	now := time.Now()
	y, m, d := now.Date()
	loc := now.Location()
	since = time.Date(y, m, d, 0, 0, 0, 0, loc)
	until = since.AddDate(0, 0, 1)
	return since, until
}
