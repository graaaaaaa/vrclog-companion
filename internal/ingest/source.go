// Package ingest provides log event ingestion from vrclog-go to SQLite.
package ingest

import (
	"context"
	"time"
)

// EventSource abstracts event production for testing.
// Implementations should close both channels when ctx is cancelled or on fatal error.
type EventSource interface {
	// Start begins producing events. Returns channels that close on ctx.Done().
	// The error channel may receive multiple non-fatal errors during operation.
	Start(ctx context.Context) (<-chan Event, <-chan error, error)
}

// Event represents a parsed VRChat log event.
// This mirrors vrclog.Event fields needed for ingestion.
type Event struct {
	Type       string
	Timestamp  time.Time
	PlayerName string
	PlayerID   string
	WorldID    string
	WorldName  string
	InstanceID string
	RawLine    string
}

// ParseError wraps a parse failure with the original line.
type ParseError struct {
	Line string
	Err  error
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return "parse error"
}

// Unwrap returns the underlying error.
func (e *ParseError) Unwrap() error {
	return e.Err
}
