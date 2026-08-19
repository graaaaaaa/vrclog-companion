package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	vrclog "github.com/vrclog/vrclog-go"
)

// LatestCursor returns the ingest cursor with the most recent updated_at
// across all sources, or nil if no cursor has been committed yet. Because
// VRChat log rotation changes the SourceID per file, the most-recently-
// updated cursor row is the correct resume point.
func (s *Store) LatestCursor(ctx context.Context) (*vrclog.Cursor, error) {
	const q = `SELECT source_id, path, byte_offset, line_number FROM ingest_cursors ORDER BY updated_at DESC LIMIT 1`

	var sourceID, path string
	var offset int64
	var line uint64
	err := s.db.QueryRowContext(ctx, q).Scan(&sourceID, &path, &offset, &line)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest cursor: %w", err)
	}

	return &vrclog.Cursor{
		SourceID: vrclog.SourceID(sourceID),
		Path:     path,
		Offset:   offset,
		Line:     line,
	}, nil
}

func upsertCursorTx(ctx context.Context, tx *sql.Tx, cursor vrclog.Cursor, now time.Time) error {
	const q = `
	INSERT INTO ingest_cursors (source_id, path, byte_offset, line_number, updated_at)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(source_id) DO UPDATE SET
		path = excluded.path,
		byte_offset = excluded.byte_offset,
		line_number = excluded.line_number,
		updated_at = excluded.updated_at
	`
	_, err := tx.ExecContext(ctx, q,
		string(cursor.SourceID), cursor.Path, cursor.Offset, cursor.Line, now.UTC().Format(TimeFormat),
	)
	if err != nil {
		return fmt.Errorf("upsert cursor: %w", err)
	}
	return nil
}
