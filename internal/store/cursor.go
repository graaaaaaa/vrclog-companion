package store

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// EncodeCursor creates a URL-safe base64-encoded cursor from timestamp and ID.
// Uses RawURLEncoding for safe use in HTTP query parameters.
func EncodeCursor(t time.Time, id int64) string {
	s := fmt.Sprintf("%s|%d", t.UTC().Format(TimeFormat), id)
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

// encodeCursor is an internal alias for backward compatibility.
func encodeCursor(t time.Time, id int64) string {
	return EncodeCursor(t, id)
}

// decodeCursor parses a base64-encoded cursor into timestamp and ID.
// Supports both RawURLEncoding (preferred) and StdEncoding (backward compatibility).
func decodeCursor(cur string) (time.Time, int64, error) {
	// Try RawURLEncoding first (preferred)
	b, err := base64.RawURLEncoding.DecodeString(cur)
	if err != nil {
		// Fallback to StdEncoding for backward compatibility
		b, err = base64.StdEncoding.DecodeString(cur)
		if err != nil {
			return time.Time{}, 0, fmt.Errorf("%w: base64 decode failed", ErrInvalidCursor)
		}
	}

	// Parse using strings.Cut for flexibility
	tsStr, idStr, ok := strings.Cut(string(b), "|")
	if !ok {
		return time.Time{}, 0, fmt.Errorf("%w: missing separator", ErrInvalidCursor)
	}

	t, err := time.Parse(TimeFormat, tsStr)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("%w: invalid timestamp", ErrInvalidCursor)
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("%w: invalid id", ErrInvalidCursor)
	}

	return t, id, nil
}
