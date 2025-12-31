package store

import (
	"context"
	"testing"
	"time"
)

func TestVacuumIfNeeded_FirstRun(t *testing.T) {
	st := openTestStore(t)
	defer st.Close()

	ctx := context.Background()

	// First run should trigger VACUUM (no last_vacuum_at record)
	vacuumed, err := st.VacuumIfNeeded(ctx)
	if err != nil {
		t.Fatalf("VacuumIfNeeded failed: %v", err)
	}
	if !vacuumed {
		t.Error("expected VACUUM to run on first call")
	}

	// Second run should skip (just ran)
	vacuumed, err = st.VacuumIfNeeded(ctx)
	if err != nil {
		t.Fatalf("VacuumIfNeeded failed: %v", err)
	}
	if vacuumed {
		t.Error("expected VACUUM to be skipped on second call")
	}
}

func TestVacuumIfNeeded_OldTimestamp(t *testing.T) {
	st := openTestStore(t)
	defer st.Close()

	ctx := context.Background()

	// Set last vacuum to 31 days ago
	oldTime := time.Now().Add(-31 * 24 * time.Hour)
	if err := st.setLastVacuumTime(ctx, oldTime); err != nil {
		t.Fatalf("setLastVacuumTime failed: %v", err)
	}

	// Should trigger VACUUM
	vacuumed, err := st.VacuumIfNeeded(ctx)
	if err != nil {
		t.Fatalf("VacuumIfNeeded failed: %v", err)
	}
	if !vacuumed {
		t.Error("expected VACUUM to run when last vacuum was 31 days ago")
	}
}

func TestVacuumIfNeeded_RecentTimestamp(t *testing.T) {
	st := openTestStore(t)
	defer st.Close()

	ctx := context.Background()

	// Set last vacuum to 1 day ago
	recentTime := time.Now().Add(-24 * time.Hour)
	if err := st.setLastVacuumTime(ctx, recentTime); err != nil {
		t.Fatalf("setLastVacuumTime failed: %v", err)
	}

	// Should skip VACUUM
	vacuumed, err := st.VacuumIfNeeded(ctx)
	if err != nil {
		t.Fatalf("VacuumIfNeeded failed: %v", err)
	}
	if vacuumed {
		t.Error("expected VACUUM to be skipped when last vacuum was 1 day ago")
	}
}
