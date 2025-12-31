package store

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// VacuumInterval is the minimum interval between VACUUM operations.
const VacuumInterval = 30 * 24 * time.Hour // 30 days

const metadataKeyLastVacuum = "last_vacuum_at"

// VacuumIfNeeded runs VACUUM if the last vacuum was more than VacuumInterval ago.
// Returns true if VACUUM was performed, false if skipped.
func (s *Store) VacuumIfNeeded(ctx context.Context) (bool, error) {
	lastVacuum, err := s.getLastVacuumTime(ctx)
	if err != nil {
		return false, err
	}

	if time.Since(lastVacuum) < VacuumInterval {
		return false, nil
	}

	log.Println("Running VACUUM (last run:", lastVacuum.Format(time.RFC3339), ")")
	start := time.Now()

	if _, err := s.db.ExecContext(ctx, "VACUUM"); err != nil {
		return false, err
	}

	elapsed := time.Since(start)
	log.Printf("VACUUM completed in %v", elapsed)

	if err := s.setLastVacuumTime(ctx, time.Now()); err != nil {
		// Log but don't fail - VACUUM succeeded
		log.Printf("Warning: failed to update last_vacuum_at: %v", err)
	}

	return true, nil
}

func (s *Store) getLastVacuumTime(ctx context.Context) (time.Time, error) {
	var value string
	err := s.db.QueryRowContext(ctx,
		"SELECT value FROM metadata WHERE key = ?",
		metadataKeyLastVacuum,
	).Scan(&value)

	if err == sql.ErrNoRows {
		// Never vacuumed - return zero time to trigger first VACUUM
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}

	t, err := time.Parse(TimeFormat, value)
	if err != nil {
		// Invalid format - trigger VACUUM
		return time.Time{}, nil
	}

	return t, nil
}

func (s *Store) setLastVacuumTime(ctx context.Context, t time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO metadata (key, value) VALUES (?, ?)",
		metadataKeyLastVacuum,
		t.UTC().Format(TimeFormat),
	)
	return err
}
