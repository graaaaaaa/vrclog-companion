package store

import (
	"errors"
	"fmt"

	vrclog "github.com/vrclog/vrclog-go"
)

// Sentinel errors for the store package.
var (
	// ErrUnsupportedSchema is returned when the database's schema version
	// cannot be used as-is and cannot be automatically migrated. The caller
	// must stop the app and rename or delete the database file.
	ErrUnsupportedSchema = errors.New("unsupported database schema")

	// ErrObservationConflict is returned by CommitRecord when an incoming
	// Observation shares an ID with a stored Observation but its canonical
	// fields differ. The enclosing transaction is rolled back. Match it with
	// errors.Is, or use errors.As with *ObservationConflictError to recover
	// the specific Observation ID that conflicted.
	ErrObservationConflict = errors.New("observation conflict: existing observation has different canonical fields")

	// ErrInvalidCursor is returned when a query cursor cannot be parsed.
	ErrInvalidCursor = errors.New("invalid cursor")
)

// ObservationConflictError wraps ErrObservationConflict with the specific
// Observation ID that conflicted. Unlike a transient DB error, retrying the
// same Record unchanged will deterministically fail again — a caller must
// either drop that Observation from the Result and retry, or otherwise
// resolve the conflict, rather than backoff-retrying indefinitely.
type ObservationConflictError struct {
	ID vrclog.ObservationID
}

func (e *ObservationConflictError) Error() string {
	return fmt.Sprintf("%s: id=%s", ErrObservationConflict, e.ID)
}

func (e *ObservationConflictError) Unwrap() error {
	return ErrObservationConflict
}
