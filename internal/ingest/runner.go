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

// ErrIntegrityViolation marks a fatal, non-retryable ingest failure caused
// by the committed Observation stream itself being inconsistent — a
// same-ID-different-content conflict (store.ErrObservationConflict). This
// is never retried and never silently dropped: Run returns the error,
// StateFailed is set, and the process is expected to exit non-zero so the
// operator can rename/delete the DB file and restart.
var ErrIntegrityViolation = errors.New("ingest integrity violation")

// ErrProjectionFailure marks a fatal, non-retryable failure of the
// OnInsert callback (Projector Apply) for an Observation that has already
// been committed. It is intentionally distinct from ErrIntegrityViolation:
// the DB itself is not inconsistent — replaying it via Rebuild on restart
// is expected to restore a consistent Projector state — so this is not a
// DB-corruption signal an operator should respond to by deleting the DB.
var ErrProjectionFailure = errors.New("post-commit projection failure")

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
// committed. It must not block for long — it typically applies the
// Observation to the Projector and, for phase == DeliveryLive, fans out to
// SSE and notifications. A non-nil return is fatal: the DB row is already
// committed and cannot be rolled back, so the Runner stops with
// ErrProjectionFailure rather than silently leaving Projector state
// inconsistent; a restart's Projector Rebuild replays from the DB to
// recover.
type OnInsertFunc func(ctx context.Context, phase DeliveryPhase, obs observation.StoredObservation) error

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
//
// A fatal error from runSource (ErrIntegrityViolation or
// ErrProjectionFailure) is never retried: Run returns it immediately with
// StateFailed already set, for the caller to treat as a controlled-shutdown
// trigger.
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
		newCursor, madeProgress, runErr := r.runSource(ctx, source)
		if newCursor != nil {
			cursor = newCursor
		}

		if isFatal(runErr) {
			return runErr
		}

		if ctx.Err() != nil {
			r.status.setState(StateStopped)
			return nil
		}

		if runErr != nil {
			r.status.recordError(runErr)
		}
		if madeProgress {
			// At least one Record committed successfully during this source
			// run: the prior failure streak, if any, is resolved. The next
			// source failure (if it happens) starts backoff from scratch
			// rather than continuing to escalate from attempts that
			// happened before recovery.
			attempt = 0
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

// isFatal reports whether err is a non-retryable ingest failure that must
// stop the Runner entirely rather than trigger source-level backoff/retry.
func isFatal(err error) bool {
	return errors.Is(err, ErrIntegrityViolation) || errors.Is(err, ErrProjectionFailure)
}

// runSource consumes one RecordSource until it ends (cleanly, fatally, or
// via ctx cancellation), returning the last cursor committed, whether at
// least one Record was committed successfully during this run, and — if
// the source ended with a genuine error — that error. A fatal error
// (ErrIntegrityViolation, ErrProjectionFailure) already has StateFailed set
// by the time it is returned.
func (r *Runner) runSource(ctx context.Context, source RecordSource) (lastCursor *vrclog.Cursor, madeProgress bool, err error) {
	dbAttempt := 0

	for sr, recErr := range source.Records(ctx) {
		if ctx.Err() != nil {
			return lastCursor, madeProgress, nil
		}
		if recErr != nil {
			return lastCursor, madeProgress, recErr
		}
		record := sr.Record

		result := r.engine.Process(record)

		for {
			commitResult, commitErr := r.store.CommitRecord(ctx, store.RecordCommit{
				Record:     record,
				Result:     result,
				IngestedAt: r.clock(),
			})
			if commitErr == nil {
				dbAttempt = 0
				madeProgress = true
				cur := commitResult.Cursor
				lastCursor = &cur
				r.status.recordSuccess(record.Time)

				for _, obs := range commitResult.InsertedObservations {
					if r.onInsert == nil {
						continue
					}
					if insertErr := r.onInsert(ctx, sr.Phase, obs); insertErr != nil {
						fatalErr := fmt.Errorf(
							"projector apply failed for observation %s (adapter=%s rule=%s): %w",
							obs.ID, obs.AdapterID, obs.RuleID, ErrProjectionFailure,
						)
						r.status.setFailed(fatalErr)
						return lastCursor, madeProgress, fatalErr
					}
				}
				break
			}

			// A same-ID-different-content Observation conflict is a fatal
			// integrity violation, not a transient failure: the store
			// transaction has already rolled back (cursor unchanged), and
			// retrying the same Result would fail identically forever. It
			// is never dropped and never retried — the operator must
			// rename/delete the DB file and restart (spec §4.3).
			var conflictErr *store.ObservationConflictError
			if errors.As(commitErr, &conflictErr) {
				conflictingAdapterID, conflictingRuleID := conflictingObservationDiagnostics(result, conflictErr.ID)
				fatalErr := fmt.Errorf(
					"observation %s conflicts with a differently-encoded stored row (adapter=%s rule=%s): %w: %w",
					conflictErr.ID, conflictingAdapterID, conflictingRuleID, ErrIntegrityViolation, commitErr,
				)
				r.status.setFailed(fatalErr)
				return lastCursor, madeProgress, fatalErr
			}

			r.status.recordError(commitErr)
			if !sleepBackoff(ctx, r.dbRetryInitialDelay, r.dbRetryMaxDelay, dbAttempt) {
				return lastCursor, madeProgress, nil
			}
			dbAttempt++
			// Retry the SAME record on the next loop iteration; the next
			// Record from source is not consumed until this one commits.
		}
	}

	return lastCursor, madeProgress, nil
}

// conflictingObservationDiagnostics finds the AdapterID/RuleID of the
// Observation in result that caused a conflict, for fatal-error diagnostic
// context. Returns empty strings if not found (should not happen in
// practice, since the conflict came from committing this exact result).
func conflictingObservationDiagnostics(result vrclog.Result, id vrclog.ObservationID) (adapterID, ruleID string) {
	for _, obs := range result.Observations {
		if obs.ID == id {
			return string(obs.AdapterID), string(obs.RuleID)
		}
	}
	return "", ""
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
