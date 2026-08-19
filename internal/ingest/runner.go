package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	vrclog "github.com/vrclog/vrclog-go"

	"github.com/vrclog/vrclog-companion/internal/observation"
	"github.com/vrclog/vrclog-companion/internal/store"
)

// diagnosticCodeObservationConflict marks a Companion-synthesized
// Diagnostic recording that a conflicting Observation was dropped from
// ingest rather than retried forever. It is not one of vrclog-go's own
// DiagnosticCode values — the diagnostics table's code column is free text.
const diagnosticCodeObservationConflict vrclog.DiagnosticCode = "observation_conflict"

// RecordStore is the subset of *store.Store the Runner depends on.
type RecordStore interface {
	CommitRecord(ctx context.Context, commit store.RecordCommit) (store.CommitResult, error)
	LatestCursor(ctx context.Context) (*vrclog.Cursor, error)
}

// Engine processes a Record into a Result. Implemented by *vrclog.Engine.
type Engine interface {
	Process(record vrclog.Record) vrclog.Result
}

// Clock abstracts time.Now for deterministic tests.
type Clock func() time.Time

// OnInsertFunc is called once per newly inserted Observation, in Engine
// emission order, after its enclosing CommitRecord transaction has
// committed. It must not block for long — it typically fans out to
// Projectors, SSE, and notifications.
type OnInsertFunc func(ctx context.Context, obs observation.StoredObservation)

const (
	dbRetryInitialDelay = 1 * time.Second
	dbRetryMaxDelay     = 30 * time.Second

	sourceRetryInitialDelay = 1 * time.Second
	sourceRetryMaxDelay     = 30 * time.Second
)

// Runner drives the per-Record ingest pipeline: RecordSource -> Engine ->
// Store.CommitRecord -> OnInsert callback.
type Runner struct {
	sourceFactory RecordSourceFactory
	engine        Engine
	store         RecordStore
	onInsert      OnInsertFunc
	clock         Clock
	logger        *slog.Logger
	status        *statusTracker

	dbRetryInitialDelay     time.Duration
	dbRetryMaxDelay         time.Duration
	sourceRetryInitialDelay time.Duration
	sourceRetryMaxDelay     time.Duration
}

// RunnerOption configures a Runner.
type RunnerOption func(*Runner)

// WithOnInsert sets the callback invoked for each newly inserted Observation.
func WithOnInsert(f OnInsertFunc) RunnerOption {
	return func(r *Runner) { r.onInsert = f }
}

// WithClock overrides the clock used to stamp IngestedAt (for tests).
func WithClock(c Clock) RunnerOption {
	return func(r *Runner) { r.clock = c }
}

// WithLogger overrides the Runner's logger.
func WithLogger(l *slog.Logger) RunnerOption {
	return func(r *Runner) {
		if l != nil {
			r.logger = l
		}
	}
}

// WithDBRetryDelays overrides the bounded backoff range used when
// CommitRecord fails (default 1s..30s). Intended for tests.
func WithDBRetryDelays(initial, max time.Duration) RunnerOption {
	return func(r *Runner) {
		r.dbRetryInitialDelay = initial
		r.dbRetryMaxDelay = max
	}
}

// WithSourceRetryDelays overrides the bounded backoff range used when the
// RecordSource fails fatally (default 1s..30s). Intended for tests.
func WithSourceRetryDelays(initial, max time.Duration) RunnerOption {
	return func(r *Runner) {
		r.sourceRetryInitialDelay = initial
		r.sourceRetryMaxDelay = max
	}
}

// NewRunner creates a Runner. sourceFactory constructs (and reconstructs, on
// fatal source errors) the underlying RecordSource; engine processes each
// Record; st persists Observations/Diagnostics/cursor atomically.
func NewRunner(sourceFactory RecordSourceFactory, engine Engine, st RecordStore, opts ...RunnerOption) *Runner {
	r := &Runner{
		sourceFactory:           sourceFactory,
		engine:                  engine,
		store:                   st,
		clock:                   time.Now,
		logger:                  slog.Default(),
		status:                  newStatusTracker(),
		dbRetryInitialDelay:     dbRetryInitialDelay,
		dbRetryMaxDelay:         dbRetryMaxDelay,
		sourceRetryInitialDelay: sourceRetryInitialDelay,
		sourceRetryMaxDelay:     sourceRetryMaxDelay,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Status returns the current ingest health status. Safe for concurrent use.
func (r *Runner) Status() Status {
	return r.status.snapshot()
}

// Run drives the ingest loop until ctx is cancelled, returning nil on clean
// shutdown. Source-level failures (including the source factory itself
// failing) are retried with bounded backoff, rebuilding the RecordSource
// from the last successfully committed cursor; they do not stop the Runner.
func (r *Runner) Run(ctx context.Context) error {
	cursor, err := r.store.LatestCursor(ctx)
	if err != nil {
		return fmt.Errorf("load latest cursor: %w", err)
	}

	attempt := 0
	for {
		if ctx.Err() != nil {
			r.status.setState(StateStopped)
			return nil
		}

		source, err := r.sourceFactory.NewSource(ctx, cursor)
		if err != nil {
			r.status.recordError(err)
			if !sleepBackoff(ctx, r.sourceRetryInitialDelay, r.sourceRetryMaxDelay, attempt) {
				r.status.setState(StateStopped)
				return nil
			}
			attempt++
			continue
		}

		r.status.setState(StateRunning)
		newCursor, runErr := r.runSource(ctx, source)
		if newCursor != nil {
			cursor = newCursor
		}

		if ctx.Err() != nil {
			r.status.setState(StateStopped)
			return nil
		}

		if runErr != nil {
			r.status.recordError(runErr)
		}
		// Whether the source ended cleanly (runErr == nil, e.g. Follow's
		// iterator returned without ctx being done — not expected in
		// practice, but handled the same way) or fatally, rebuild it from
		// the last committed cursor with bounded backoff.
		if !sleepBackoff(ctx, r.sourceRetryInitialDelay, r.sourceRetryMaxDelay, attempt) {
			r.status.setState(StateStopped)
			return nil
		}
		attempt++
	}
}

// runSource consumes one RecordSource until it ends (cleanly, fatally, or
// via ctx cancellation), returning the last cursor committed and, if the
// source ended with a genuine error, that error.
func (r *Runner) runSource(ctx context.Context, source RecordSource) (lastCursor *vrclog.Cursor, err error) {
	dbAttempt := 0

	for record, recErr := range source.Records(ctx) {
		if ctx.Err() != nil {
			return lastCursor, nil
		}
		if recErr != nil {
			return lastCursor, recErr
		}

		result := r.engine.Process(record)

		for {
			commitResult, commitErr := r.store.CommitRecord(ctx, store.RecordCommit{
				Record:     record,
				Result:     result,
				IngestedAt: r.clock(),
			})
			if commitErr == nil {
				dbAttempt = 0
				cur := commitResult.Cursor
				lastCursor = &cur
				r.status.recordSuccess(record.Time)

				for _, obs := range commitResult.InsertedObservations {
					if r.onInsert != nil {
						r.onInsert(ctx, obs)
					}
				}
				break
			}

			// An Observation conflict is deterministic, not transient:
			// retrying the same Result unchanged will fail identically
			// forever, permanently blocking all downstream ingest. Drop the
			// offending Observation, record why, and retry immediately —
			// this still advances the cursor once the rest commits cleanly,
			// rather than silently overwriting the conflicting content.
			var conflictErr *store.ObservationConflictError
			if errors.As(commitErr, &conflictErr) {
				result = dropConflictingObservation(result, conflictErr.ID)
				r.status.recordError(commitErr)
				continue
			}

			r.status.recordError(commitErr)
			if !sleepBackoff(ctx, r.dbRetryInitialDelay, r.dbRetryMaxDelay, dbAttempt) {
				return lastCursor, nil
			}
			dbAttempt++
			// Retry the SAME record on the next loop iteration; the next
			// Record from source is not consumed until this one commits.
		}
	}

	return lastCursor, nil
}

// dropConflictingObservation removes the Observation with the given ID from
// result.Observations and appends a Diagnostic explaining why, so the next
// CommitRecord attempt for this Record can succeed instead of retrying an
// unresolvable conflict forever.
func dropConflictingObservation(result vrclog.Result, id vrclog.ObservationID) vrclog.Result {
	filtered := make([]vrclog.Observation, 0, len(result.Observations))
	var dropped *vrclog.Observation
	for _, obs := range result.Observations {
		if obs.ID == id {
			o := obs
			dropped = &o
			continue
		}
		filtered = append(filtered, obs)
	}
	result.Observations = filtered

	diag := vrclog.Diagnostic{
		Code:    diagnosticCodeObservationConflict,
		Message: fmt.Sprintf("observation %s conflicts with a differently-encoded stored row; dropped to avoid blocking ingest", id),
	}
	if dropped != nil {
		diag.AdapterID = dropped.AdapterID
		diag.RuleID = dropped.RuleID
		diag.Record = dropped.Record
	}
	result.Diagnostics = append(result.Diagnostics, diag)

	return result
}

// sleepBackoff waits for a bounded exponential backoff delay or ctx.Done,
// whichever comes first. Returns false if ctx was cancelled during the wait.
func sleepBackoff(ctx context.Context, initial, max time.Duration, attempt int) bool {
	delay := backoffDelay(initial, max, attempt)
	select {
	case <-time.After(delay):
		return true
	case <-ctx.Done():
		return false
	}
}

func backoffDelay(initial, max time.Duration, attempt int) time.Duration {
	if attempt < 0 || attempt > 30 { // guard against shift overflow
		return max
	}
	delay := initial * time.Duration(uint64(1)<<uint(attempt))
	if delay <= 0 || delay > max {
		return max
	}
	return delay
}
