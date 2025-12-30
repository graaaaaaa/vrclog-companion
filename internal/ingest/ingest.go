package ingest

import (
	"context"
	"errors"
	"log/slog"

	"github.com/graaaaa/vrclog-companion/internal/event"
)

// EventStore defines store operations needed by Ingester.
type EventStore interface {
	InsertEvent(ctx context.Context, e *event.Event) (bool, error)
	InsertParseFailure(ctx context.Context, rawLine, errorMsg string) (bool, error)
}

// Ingester coordinates event ingestion from source to store.
type Ingester struct {
	source EventSource
	store  EventStore
	logger *slog.Logger
	clock  Clock
}

// Option configures an Ingester.
type Option func(*Ingester)

// WithLogger sets the logger for the Ingester.
func WithLogger(logger *slog.Logger) Option {
	return func(i *Ingester) { i.logger = logger }
}

// WithClock sets the clock for the Ingester (for testing).
func WithClock(clock Clock) Option {
	return func(i *Ingester) { i.clock = clock }
}

// New creates a new Ingester.
func New(source EventSource, store EventStore, opts ...Option) *Ingester {
	i := &Ingester{
		source: source,
		store:  store,
		logger: slog.Default(),
		clock:  DefaultClock,
	}
	for _, opt := range opts {
		opt(i)
	}
	return i
}

// Run starts the ingestion loop. Blocks until ctx is cancelled or source closes.
// Returns ctx.Err() on context cancellation, nil on clean source shutdown.
func (i *Ingester) Run(ctx context.Context) error {
	events, errs, err := i.source.Start(ctx)
	if err != nil {
		return err
	}
	if events == nil || errs == nil {
		return errors.New("source returned nil channel")
	}

	i.logger.Info("ingestion started")
	defer i.logger.Info("ingestion stopped")

	// Use nil-channel pattern: nil each channel when closed, exit when both are nil.
	eventsCh := events
	errsCh := errs
	firstClosed := ""

	for eventsCh != nil || errsCh != nil {
		select {
		case ev, ok := <-eventsCh:
			if !ok {
				if firstClosed == "" {
					firstClosed = "events"
				}
				eventsCh = nil
				continue
			}
			i.handleEvent(ctx, ev)
		case err, ok := <-errsCh:
			if !ok {
				if firstClosed == "" {
					firstClosed = "errs"
				}
				errsCh = nil
				continue
			}
			i.handleError(ctx, err)
		case <-ctx.Done():
			if firstClosed != "" {
				i.logger.Debug("channel closed before context", "channel", firstClosed)
			}
			return ctx.Err()
		}
	}

	if firstClosed != "" {
		i.logger.Debug("ingestion channels closed", "first_closed", firstClosed)
	}
	return ctx.Err()
}

// handleEvent processes a single event.
func (i *Ingester) handleEvent(ctx context.Context, ev Event) {
	storeEvent := ToStoreEventWithClock(ev, i.clock)

	inserted, err := i.store.InsertEvent(ctx, storeEvent)
	if err != nil {
		i.logger.Error("failed to insert event",
			"type", ev.Type,
			"error", err,
		)
		return
	}

	if inserted {
		i.logger.Debug("event inserted",
			"type", ev.Type,
			"ts", ev.Timestamp,
		)
	}
}

// handleError processes an error from the source.
func (i *Ingester) handleError(ctx context.Context, err error) {
	var parseErr *ParseError
	if errors.As(err, &parseErr) {
		i.handleParseError(ctx, parseErr)
		return
	}

	// Log non-parse errors
	i.logger.Warn("source error", "error", err)
}

// handleParseError saves a parse failure to the database.
func (i *Ingester) handleParseError(ctx context.Context, parseErr *ParseError) {
	errMsg := ""
	if parseErr.Err != nil {
		errMsg = parseErr.Err.Error()
	}

	inserted, err := i.store.InsertParseFailure(ctx, parseErr.Line, errMsg)
	if err != nil {
		i.logger.Error("failed to insert parse failure",
			"error", err,
		)
		return
	}

	if inserted {
		i.logger.Debug("parse failure recorded",
			"line_length", len(parseErr.Line),
		)
	}
}
