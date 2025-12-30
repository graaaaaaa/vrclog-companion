package store

import (
	"context"
	"fmt"
)

// CurrentSchemaVersion is the current database schema version.
const CurrentSchemaVersion = 1

// migrate runs database migrations.
func (s *Store) migrate(ctx context.Context) error {
	// Create events table
	if err := s.createEventsTable(ctx); err != nil {
		return err
	}

	// Create ingest_cursor table (for future use)
	if err := s.createIngestCursorTable(ctx); err != nil {
		return err
	}

	return nil
}

func (s *Store) createEventsTable(ctx context.Context) error {
	const schema = `
	CREATE TABLE IF NOT EXISTS events (
		id             INTEGER PRIMARY KEY,
		ts             TEXT NOT NULL,
		type           TEXT NOT NULL,
		player_name    TEXT,
		player_id      TEXT,
		world_id       TEXT,
		world_name     TEXT,
		instance_id    TEXT,
		meta_json      TEXT,
		dedupe_key     TEXT NOT NULL,
		ingested_at    TEXT NOT NULL,
		schema_version INTEGER NOT NULL,
		UNIQUE(dedupe_key)
	);

	CREATE INDEX IF NOT EXISTS idx_events_ts ON events(ts);
	CREATE INDEX IF NOT EXISTS idx_events_type_ts ON events(type, ts);
	CREATE INDEX IF NOT EXISTS idx_events_ts_id ON events(ts, id);
	`

	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create events table: %w", err)
	}
	return nil
}

func (s *Store) createIngestCursorTable(ctx context.Context) error {
	const schema = `
	CREATE TABLE IF NOT EXISTS ingest_cursor (
		id              INTEGER PRIMARY KEY,
		source_path     TEXT NOT NULL,
		source_identity TEXT NOT NULL,
		byte_offset     INTEGER NOT NULL,
		updated_at      TEXT NOT NULL
	);
	`

	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create ingest_cursor table: %w", err)
	}
	return nil
}
