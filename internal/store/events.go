package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/event"
)

const (
	defaultLimit = 100
	maxLimit     = 500
)

// InsertEvent inserts an event into the database.
// Returns the inserted ID if successful, or 0 if the event was a duplicate.
// Uses ON CONFLICT(dedupe_key) DO NOTHING for deduplication.
// On success, sets e.ID to the inserted row's ID.
func (s *Store) InsertEvent(ctx context.Context, e *event.Event) (id int64, inserted bool, err error) {
	if err := validateEvent(e); err != nil {
		return 0, false, err
	}

	const query = `
	INSERT INTO events
	(ts, type, player_name, player_id, world_id, world_name, instance_id, meta_json, dedupe_key, ingested_at, schema_version)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(dedupe_key) DO NOTHING
	`

	row := eventToRow(e)
	result, err := s.db.ExecContext(ctx, query,
		row.Ts,
		row.Type,
		row.PlayerName,
		row.PlayerID,
		row.WorldID,
		row.WorldName,
		row.InstanceID,
		row.MetaJSON,
		row.DedupeKey,
		row.IngestedAt,
		CurrentSchemaVersion,
	)
	if err != nil {
		return 0, false, fmt.Errorf("insert event: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, false, fmt.Errorf("rows affected: %w", err)
	}

	if rowsAffected > 0 {
		id, err = result.LastInsertId()
		if err != nil {
			return 0, false, fmt.Errorf("last insert id: %w", err)
		}
		e.ID = id
		return id, true, nil
	}

	return 0, false, nil
}

// QueryFilter contains filter options for querying events.
type QueryFilter struct {
	Since  *time.Time
	Until  *time.Time
	Type   *string
	Limit  int
	Cursor *string
}

// QueryResult contains the result of a query.
type QueryResult struct {
	Items      []event.Event
	NextCursor *string
}

// QueryEvents queries events with optional filters and cursor-based pagination.
func (s *Store) QueryEvents(ctx context.Context, f QueryFilter) (QueryResult, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	} else if limit > maxLimit {
		limit = maxLimit
	}

	var (
		sb   strings.Builder
		args []any
	)

	sb.WriteString(`
SELECT id, ts, type, player_name, player_id, world_id, world_name, instance_id, meta_json, dedupe_key, ingested_at, schema_version
FROM events
WHERE 1=1
`)

	if f.Since != nil {
		sb.WriteString(" AND ts >= ?")
		args = append(args, f.Since.UTC().Format(TimeFormat))
	}
	if f.Until != nil {
		sb.WriteString(" AND ts < ?")
		args = append(args, f.Until.UTC().Format(TimeFormat))
	}
	if f.Type != nil && *f.Type != "" {
		sb.WriteString(" AND type = ?")
		args = append(args, *f.Type)
	}

	// Cursor handling (composite cursor: ts|id)
	if f.Cursor != nil && *f.Cursor != "" {
		cursorTime, cursorID, err := decodeCursor(*f.Cursor)
		if err != nil {
			return QueryResult{}, fmt.Errorf("decode cursor: %w", err)
		}
		sb.WriteString(" AND (ts > ? OR (ts = ? AND id > ?))")
		cursorTimeStr := cursorTime.UTC().Format(TimeFormat)
		args = append(args, cursorTimeStr, cursorTimeStr, cursorID)
	}

	sb.WriteString(" ORDER BY ts ASC, id ASC")
	sb.WriteString(" LIMIT ?")
	args = append(args, limit+1) // fetch one extra to detect next page

	rows, err := s.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return QueryResult{}, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	items := make([]event.Event, 0, limit+1)
	for rows.Next() {
		var r eventRow
		if err := rows.Scan(
			&r.ID, &r.Ts, &r.Type, &r.PlayerName, &r.PlayerID,
			&r.WorldID, &r.WorldName, &r.InstanceID, &r.MetaJSON,
			&r.DedupeKey, &r.IngestedAt, &r.SchemaVersion,
		); err != nil {
			return QueryResult{}, fmt.Errorf("scan event: %w", err)
		}
		e, err := r.toEvent()
		if err != nil {
			return QueryResult{}, err
		}
		items = append(items, *e)
	}
	if err := rows.Err(); err != nil {
		return QueryResult{}, fmt.Errorf("rows error: %w", err)
	}

	var nextCursor *string
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		c := encodeCursor(last.Ts, last.ID)
		nextCursor = &c
	}

	return QueryResult{Items: items, NextCursor: nextCursor}, nil
}

// GetLastEventTime returns the timestamp of the most recent event.
// Returns zero time if no events exist.
func (s *Store) GetLastEventTime(ctx context.Context) (time.Time, error) {
	const query = `SELECT ts FROM events ORDER BY ts DESC, id DESC LIMIT 1`

	var ts string
	err := s.db.QueryRowContext(ctx, query).Scan(&ts)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("get last event time: %w", err)
	}

	t, err := time.Parse(TimeFormat, ts)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", ts, err)
	}

	return t, nil
}

// CountEvents returns the total number of events in the database.
func (s *Store) CountEvents(ctx context.Context) (int64, error) {
	const query = `SELECT COUNT(*) FROM events`

	var count int64
	if err := s.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("count events: %w", err)
	}
	return count, nil
}
