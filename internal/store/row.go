package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/event"
)

// eventRow is the internal type representing a database row.
type eventRow struct {
	ID            int64
	Ts            string
	Type          string
	PlayerName    sql.NullString
	PlayerID      sql.NullString
	WorldID       sql.NullString
	WorldName     sql.NullString
	InstanceID    sql.NullString
	MetaJSON      sql.NullString
	DedupeKey     string
	IngestedAt    string
	SchemaVersion int
}

// toEvent converts a database row to an Event.
func (r *eventRow) toEvent() (*event.Event, error) {
	ts, err := time.Parse(TimeFormat, r.Ts)
	if err != nil {
		return nil, fmt.Errorf("parse ts %q: %w", r.Ts, err)
	}

	ingestedAt, err := time.Parse(TimeFormat, r.IngestedAt)
	if err != nil {
		return nil, fmt.Errorf("parse ingested_at %q: %w", r.IngestedAt, err)
	}

	e := &event.Event{
		ID:            r.ID,
		Ts:            ts,
		Type:          r.Type,
		DedupeKey:     r.DedupeKey,
		IngestedAt:    ingestedAt,
		SchemaVersion: r.SchemaVersion,
	}

	if r.PlayerName.Valid {
		e.PlayerName = &r.PlayerName.String
	}
	if r.PlayerID.Valid {
		e.PlayerID = &r.PlayerID.String
	}
	if r.WorldID.Valid {
		e.WorldID = &r.WorldID.String
	}
	if r.WorldName.Valid {
		e.WorldName = &r.WorldName.String
	}
	if r.InstanceID.Valid {
		e.InstanceID = &r.InstanceID.String
	}
	if r.MetaJSON.Valid && r.MetaJSON.String != "" {
		e.MetaJSON = json.RawMessage(r.MetaJSON.String)
	}

	return e, nil
}

// eventToRow converts an Event to a database row.
func eventToRow(e *event.Event) *eventRow {
	r := &eventRow{
		ID:            e.ID,
		Ts:            e.Ts.UTC().Format(TimeFormat),
		Type:          e.Type,
		DedupeKey:     e.DedupeKey,
		IngestedAt:    e.IngestedAt.UTC().Format(TimeFormat),
		SchemaVersion: e.SchemaVersion,
	}

	if e.PlayerName != nil {
		r.PlayerName = sql.NullString{String: *e.PlayerName, Valid: true}
	}
	if e.PlayerID != nil {
		r.PlayerID = sql.NullString{String: *e.PlayerID, Valid: true}
	}
	if e.WorldID != nil {
		r.WorldID = sql.NullString{String: *e.WorldID, Valid: true}
	}
	if e.WorldName != nil {
		r.WorldName = sql.NullString{String: *e.WorldName, Valid: true}
	}
	if e.InstanceID != nil {
		r.InstanceID = sql.NullString{String: *e.InstanceID, Valid: true}
	}
	if len(e.MetaJSON) > 0 {
		r.MetaJSON = sql.NullString{String: string(e.MetaJSON), Valid: true}
	}

	return r
}

// validateEvent checks that required fields are set.
func validateEvent(e *event.Event) error {
	if e.Type == "" {
		return fmt.Errorf("%w: type is required", ErrInvalidEvent)
	}
	if e.DedupeKey == "" {
		return fmt.Errorf("%w: dedupe_key is required", ErrInvalidEvent)
	}
	if e.Ts.IsZero() {
		return fmt.Errorf("%w: ts is required", ErrInvalidEvent)
	}
	if e.IngestedAt.IsZero() {
		return fmt.Errorf("%w: ingested_at is required", ErrInvalidEvent)
	}
	return nil
}
