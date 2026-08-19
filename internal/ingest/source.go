// Package ingest drives the per-Record ingest pipeline: a RecordSource
// yields vrclog.Record values, the Engine turns each into a Result, and
// Runner persists that Result atomically via Store.CommitRecord before
// notifying the rest of the app about newly inserted Observations.
package ingest

import (
	"context"
	"iter"

	vrclog "github.com/vrclog/vrclog-go"
)

// RecordSource yields a stream of Records from one log source.
type RecordSource interface {
	Records(ctx context.Context) iter.Seq2[vrclog.Record, error]
}

// RecordSourceFactory constructs a RecordSource resuming from cursor (nil
// means start fresh). Runner calls this once at startup and again whenever
// the current RecordSource ends with a fatal error, so a fresh, restartable
// RecordSource is always available without the source implementation itself
// needing restart logic.
type RecordSourceFactory interface {
	NewSource(ctx context.Context, cursor *vrclog.Cursor) (RecordSource, error)
}

// RecordSourceFactoryFunc adapts a plain function to RecordSourceFactory.
type RecordSourceFactoryFunc func(ctx context.Context, cursor *vrclog.Cursor) (RecordSource, error)

// NewSource implements RecordSourceFactory.
func (f RecordSourceFactoryFunc) NewSource(ctx context.Context, cursor *vrclog.Cursor) (RecordSource, error) {
	return f(ctx, cursor)
}
