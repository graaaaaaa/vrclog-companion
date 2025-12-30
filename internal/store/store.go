// Package store provides SQLite persistence for VRClog Companion.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite"
)

// TimeFormat is the fixed-width RFC3339 format used for timestamps.
// Using fixed width ensures lexicographic ordering matches chronological ordering.
const TimeFormat = "2006-01-02T15:04:05.000000000Z"

// Store wraps a SQLite database connection.
type Store struct {
	db *sql.DB
}

// Open opens a SQLite database with WAL mode and busy_timeout.
// The path should be an absolute path to the database file.
func Open(path string) (*Store, error) {
	// URL-escape the path to handle special characters (?, #, spaces, etc.)
	escapedPath := url.PathEscape(path)

	// DSN with WAL mode and busy_timeout for per-connection settings
	dsn := fmt.Sprintf("file:%s?mode=rwc&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", escapedPath)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Verify connection and PRAGMAs
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	// Allow multiple readers with single writer (WAL mode supports concurrent reads)
	// Using more than 1 connection allows read parallelism while writes are serialized
	db.SetMaxOpenConns(4)

	store := &Store{db: db}

	// Run migrations
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return store, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// journalMode returns the current journal mode (for testing).
func (s *Store) journalMode() (string, error) {
	var mode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		return "", err
	}
	return mode, nil
}
