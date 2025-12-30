package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// InsertParseFailure inserts a parse failure into the database.
// Returns true if the failure was inserted, false if it was a duplicate.
// Uses ON CONFLICT(dedupe_key) DO NOTHING for deduplication.
func (s *Store) InsertParseFailure(ctx context.Context, rawLine, errorMsg string) (inserted bool, err error) {
	if rawLine == "" {
		return false, fmt.Errorf("raw_line is required")
	}

	const query = `
	INSERT INTO parse_failures (ts, raw_line, error_msg, dedupe_key)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(dedupe_key) DO NOTHING
	`

	dedupeKey := sha256Hex(rawLine)
	ts := time.Now().UTC().Format(TimeFormat)

	result, err := s.db.ExecContext(ctx, query, ts, rawLine, errorMsg, dedupeKey)
	if err != nil {
		return false, fmt.Errorf("insert parse failure: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}

	return rowsAffected > 0, nil
}

// sha256Hex returns the SHA256 hash of the input string as a hex string.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
