package store

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"time"

	vrclog "github.com/vrclog/vrclog-go"

	"github.com/vrclog/vrclog-companion/internal/observation"
)

// RecordCommit bundles one processed Record with the Engine's Result for a
// single atomic CommitRecord transaction.
type RecordCommit struct {
	Record     vrclog.Record
	Result     vrclog.Result
	IngestedAt time.Time
}

// CommitResult reports what CommitRecord actually persisted.
type CommitResult struct {
	// InsertedObservations contains only the Observations newly inserted in
	// this transaction, in Engine emission order. Duplicates already present
	// in the database are excluded.
	InsertedObservations []observation.StoredObservation
	Cursor               vrclog.Cursor
}

// CommitRecord persists one Record's Observations, Diagnostics, and cursor
// advancement in a single SQLite transaction.
//
// Invariant: the cursor is committed together with the Observations/
// Diagnostics it produced, in the same transaction. This holds even when
// the Record produced zero Observations and zero Diagnostics. On any
// failure the whole transaction rolls back and the cursor does not advance.
func (s *Store) CommitRecord(ctx context.Context, commit RecordCommit) (CommitResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CommitResult{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	inserted := make([]observation.StoredObservation, 0, len(commit.Result.Observations))
	for _, obs := range commit.Result.Observations {
		stored, err := observation.FromVrclogObservation(obs, commit.IngestedAt)
		if err != nil {
			return CommitResult{}, fmt.Errorf("encode observation %s: %w", obs.ID, err)
		}

		isNew, err := insertObservationTx(ctx, tx, &stored)
		if err != nil {
			return CommitResult{}, err
		}
		if isNew {
			inserted = append(inserted, stored)
		}
	}

	for _, diag := range commit.Result.Diagnostics {
		if err := insertDiagnosticTx(ctx, tx, diag, commit.IngestedAt); err != nil {
			return CommitResult{}, err
		}
	}

	cursor := commit.Record.Cursor()
	if err := upsertCursorTx(ctx, tx, cursor, commit.IngestedAt); err != nil {
		return CommitResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return CommitResult{}, fmt.Errorf("commit transaction: %w", err)
	}

	return CommitResult{InsertedObservations: inserted, Cursor: cursor}, nil
}

// insertObservationTx inserts stored, filling in stored.Sequence on success.
// If an Observation with the same ID already exists, its canonical fields
// are compared against stored: an exact match is treated as an already-seen
// duplicate (isNew=false, no error); a mismatch returns ErrObservationConflict
// and the caller must roll back.
func insertObservationTx(ctx context.Context, tx *sql.Tx, stored *observation.StoredObservation) (isNew bool, err error) {
	const insertQ = `
	INSERT INTO observations
		(id, occurred_at, type, payload_json, adapter_id, rule_id, record_id, source_id, source_offset, source_line, ingested_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO NOTHING
	`
	res, err := tx.ExecContext(ctx, insertQ,
		string(stored.ID),
		stored.OccurredAt.UTC().Format(TimeFormat),
		string(stored.Type),
		string(stored.Payload),
		string(stored.AdapterID),
		string(stored.RuleID),
		string(stored.RecordID),
		string(stored.SourceID),
		stored.SourceOffset,
		stored.SourceLine,
		stored.IngestedAt.UTC().Format(TimeFormat),
	)
	if err != nil {
		return false, fmt.Errorf("insert observation %s: %w", stored.ID, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("insert observation %s: rows affected: %w", stored.ID, err)
	}
	if n > 0 {
		seq, err := res.LastInsertId()
		if err != nil {
			return false, fmt.Errorf("insert observation %s: last insert id: %w", stored.ID, err)
		}
		stored.Sequence = seq
		return true, nil
	}

	existing, err := queryObservationByIDTx(ctx, tx, stored.ID)
	if err != nil {
		return false, fmt.Errorf("fetch conflicting observation %s: %w", stored.ID, err)
	}
	if existing == nil {
		// Row disappeared between the failed insert and this read — treat as
		// a transient race and surface it as a conflict rather than silently
		// dropping the Observation.
		return false, fmt.Errorf("%w (row vanished after conflict)", &ObservationConflictError{ID: stored.ID})
	}
	if !sameCanonicalFields(*existing, *stored) {
		return false, &ObservationConflictError{ID: stored.ID}
	}
	return false, nil
}

// sameCanonicalFields compares the fields that define Observation identity
// per the duplicate-detection contract: occurred_at, type, payload_json,
// adapter_id, rule_id, record_id, source_id, source_offset, source_line.
// sequence and ingested_at are intentionally excluded — sequence is
// insertion-order metadata and ingested_at legitimately differs on replay.
func sameCanonicalFields(a, b observation.StoredObservation) bool {
	return a.OccurredAt.Equal(b.OccurredAt) &&
		a.Type == b.Type &&
		bytes.Equal(a.Payload, b.Payload) &&
		a.AdapterID == b.AdapterID &&
		a.RuleID == b.RuleID &&
		a.RecordID == b.RecordID &&
		a.SourceID == b.SourceID &&
		a.SourceOffset == b.SourceOffset &&
		a.SourceLine == b.SourceLine
}
