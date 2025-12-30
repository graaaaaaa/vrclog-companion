package store

import "errors"

// Sentinel errors for the store package.
var (
	// ErrInvalidCursor is returned when a cursor cannot be decoded.
	ErrInvalidCursor = errors.New("invalid cursor format")

	// ErrInvalidEvent is returned when an event fails validation.
	ErrInvalidEvent = errors.New("invalid event")
)
