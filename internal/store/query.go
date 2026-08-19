package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"time"

	vrclog "github.com/vrclog/vrclog-go"

	"github.com/vrclog/vrclog-companion/internal/observation"
)

// QueryOrder controls sequence ordering for ListObservations.
type QueryOrder int

const (
	// OrderDesc returns the newest Observations first. This is the default
	// order for the /api/v1/observations history endpoint.
	OrderDesc QueryOrder = iota
	// OrderAsc returns the oldest Observations first.
	OrderAsc
)

// Pagination limits for ListObservations.
const (
	DefaultObservationLimit = 100
	MaxObservationLimit     = 500
)

// ObservationQuery filters ListObservations. All filters are optional and
// combine with AND. Type/AdapterID are matched by exact equality via SQL
// bind parameters — there is no fixed allowlist of accepted values.
type ObservationQuery struct {
	// AfterSequence bounds results relative to a cursor sequence. For
	// OrderDesc it means "sequence < AfterSequence" (strictly older); for
	// OrderAsc it means "sequence > AfterSequence" (strictly newer). Nil
	// means unbounded (start from the newest/oldest row).
	AfterSequence *int64
	Limit         int
	Type          *string
	AdapterID     *string
	Since         *time.Time
	Until         *time.Time
	Order         QueryOrder
}

const observationColumns = "sequence, id, occurred_at, type, payload_json, adapter_id, rule_id, record_id, source_id, source_offset, source_line, ingested_at"

// ListObservations returns a page of Observations matching q, plus the
// cursor to pass as AfterSequence for the next page (nil when the page was
// short, i.e. there is nothing more to fetch in this direction).
func (s *Store) ListObservations(ctx context.Context, q ObservationQuery) ([]observation.StoredObservation, *int64, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultObservationLimit
	}
	if limit > MaxObservationLimit {
		limit = MaxObservationLimit
	}

	var where []string
	var args []any

	if q.AfterSequence != nil {
		if q.Order == OrderAsc {
			where = append(where, "sequence > ?")
		} else {
			where = append(where, "sequence < ?")
		}
		args = append(args, *q.AfterSequence)
	}
	if q.Type != nil {
		where = append(where, "type = ?")
		args = append(args, *q.Type)
	}
	if q.AdapterID != nil {
		where = append(where, "adapter_id = ?")
		args = append(args, *q.AdapterID)
	}
	if q.Since != nil {
		where = append(where, "occurred_at >= ?")
		args = append(args, q.Since.UTC().Format(TimeFormat))
	}
	if q.Until != nil {
		where = append(where, "occurred_at <= ?")
		args = append(args, q.Until.UTC().Format(TimeFormat))
	}

	order := "DESC"
	if q.Order == OrderAsc {
		order = "ASC"
	}

	query := "SELECT " + observationColumns + " FROM observations"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY sequence %s LIMIT ?", order)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("list observations: %w", err)
	}
	defer rows.Close()

	items, err := scanObservations(rows)
	if err != nil {
		return nil, nil, err
	}

	var next *int64
	if len(items) == limit {
		last := items[len(items)-1].Sequence
		next = &last
	}

	return items, next, nil
}

// ObservationsAfterSequence returns up to limit Observations with
// sequence > sequence, ordered ascending. Used for SSE Last-Event-ID
// backlog delivery.
func (s *Store) ObservationsAfterSequence(ctx context.Context, sequence int64, limit int) ([]observation.StoredObservation, error) {
	if limit <= 0 {
		limit = DefaultObservationLimit
	}
	const q = "SELECT " + observationColumns + " FROM observations WHERE sequence > ? ORDER BY sequence ASC LIMIT ?"

	rows, err := s.db.QueryContext(ctx, q, sequence, limit)
	if err != nil {
		return nil, fmt.Errorf("observations after sequence: %w", err)
	}
	defer rows.Close()

	return scanObservations(rows)
}

// LatestSequence returns the highest sequence currently stored, or 0 if the
// observations table is empty. This is the DB-backed source of truth for
// SSE reconnect bounds — unlike an in-memory broadcaster's high-water mark,
// it is correct immediately after a process restart, when the broadcaster
// has not yet re-observed any of the already-persisted rows.
func (s *Store) LatestSequence(ctx context.Context) (int64, error) {
	const q = "SELECT COALESCE(MAX(sequence), 0) FROM observations"

	var sequence int64
	if err := s.db.QueryRowContext(ctx, q).Scan(&sequence); err != nil {
		return 0, fmt.Errorf("latest observation sequence: %w", err)
	}
	return sequence, nil
}

// ObservationByID looks up a single Observation, or returns (nil, nil) if
// it does not exist.
func (s *Store) ObservationByID(ctx context.Context, id vrclog.ObservationID) (*observation.StoredObservation, error) {
	const q = "SELECT " + observationColumns + " FROM observations WHERE id = ?"

	obs, err := scanObservationRow(s.db.QueryRowContext(ctx, q, string(id)))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("observation by id: %w", err)
	}
	return &obs, nil
}

// queryObservationByIDTx is the transaction-scoped variant used by
// CommitRecord's duplicate-conflict check.
func queryObservationByIDTx(ctx context.Context, tx *sql.Tx, id vrclog.ObservationID) (*observation.StoredObservation, error) {
	const q = "SELECT " + observationColumns + " FROM observations WHERE id = ?"

	obs, err := scanObservationRow(tx.QueryRowContext(ctx, q, string(id)))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &obs, nil
}

// AllObservations iterates every Observation in sequence-ascending order.
// Used for the startup Projector rebuild.
func (s *Store) AllObservations(ctx context.Context) iter.Seq2[observation.StoredObservation, error] {
	return func(yield func(observation.StoredObservation, error) bool) {
		const q = "SELECT " + observationColumns + " FROM observations ORDER BY sequence ASC"

		rows, err := s.db.QueryContext(ctx, q)
		if err != nil {
			yield(observation.StoredObservation{}, fmt.Errorf("all observations: %w", err))
			return
		}
		defer rows.Close()

		for rows.Next() {
			obs, err := scanObservationRow(rows)
			if err != nil {
				yield(observation.StoredObservation{}, fmt.Errorf("scan observation: %w", err))
				return
			}
			if !yield(obs, nil) {
				return
			}
		}
		if err := rows.Err(); err != nil {
			yield(observation.StoredObservation{}, err)
		}
	}
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanObservationRow(row rowScanner) (observation.StoredObservation, error) {
	var (
		o          observation.StoredObservation
		idStr      string
		occurredAt string
		typeStr    string
		payload    string
		adapterID  string
		ruleID     string
		recordID   string
		sourceID   string
		ingestedAt string
	)
	if err := row.Scan(
		&o.Sequence, &idStr, &occurredAt, &typeStr, &payload,
		&adapterID, &ruleID, &recordID, &sourceID,
		&o.SourceOffset, &o.SourceLine, &ingestedAt,
	); err != nil {
		return observation.StoredObservation{}, err
	}

	occurredT, err := time.Parse(TimeFormat, occurredAt)
	if err != nil {
		return observation.StoredObservation{}, fmt.Errorf("parse occurred_at: %w", err)
	}
	ingestedT, err := time.Parse(TimeFormat, ingestedAt)
	if err != nil {
		return observation.StoredObservation{}, fmt.Errorf("parse ingested_at: %w", err)
	}

	o.ID = vrclog.ObservationID(idStr)
	o.OccurredAt = occurredT
	o.Type = vrclog.EventKind(typeStr)
	o.Payload = json.RawMessage(payload)
	o.AdapterID = vrclog.AdapterID(adapterID)
	o.RuleID = vrclog.RuleID(ruleID)
	o.RecordID = vrclog.RecordID(recordID)
	o.SourceID = vrclog.SourceID(sourceID)
	o.IngestedAt = ingestedT

	return o, nil
}

func scanObservations(rows *sql.Rows) ([]observation.StoredObservation, error) {
	items := make([]observation.StoredObservation, 0)
	for rows.Next() {
		obs, err := scanObservationRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan observation: %w", err)
		}
		items = append(items, obs)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
