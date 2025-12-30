package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInsertParseFailure(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	rawLine := "invalid log line format"
	errorMsg := "parse failed: unexpected token"

	// First insert should succeed
	inserted, err := store.InsertParseFailure(ctx, rawLine, errorMsg)
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}
	if !inserted {
		t.Error("first insert should return inserted=true")
	}

	// Second insert with same rawLine should be deduplicated
	inserted, err = store.InsertParseFailure(ctx, rawLine, errorMsg)
	if err != nil {
		t.Fatalf("second insert failed: %v", err)
	}
	if inserted {
		t.Error("second insert should return inserted=false (duplicate)")
	}
}

func TestInsertParseFailure_DifferentLines(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Insert different lines
	lines := []string{
		"invalid line 1",
		"invalid line 2",
		"invalid line 3",
	}

	for _, line := range lines {
		inserted, err := store.InsertParseFailure(ctx, line, "error")
		if err != nil {
			t.Fatalf("insert failed for %q: %v", line, err)
		}
		if !inserted {
			t.Errorf("insert should return inserted=true for %q", line)
		}
	}

	// Verify count (by querying directly)
	var count int
	err = store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM parse_failures").Scan(&count)
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 rows, got %d", count)
	}
}

func TestInsertParseFailure_Validation(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Empty rawLine should fail
	_, err = store.InsertParseFailure(ctx, "", "error")
	if err == nil {
		t.Error("expected error for empty rawLine")
	}
}

func TestInsertParseFailure_EmptyErrorMsg(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Empty error message should be allowed
	inserted, err := store.InsertParseFailure(ctx, "some line", "")
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}
	if !inserted {
		t.Error("insert should succeed with empty error message")
	}
}

func TestSha256Hex(t *testing.T) {
	// Test deterministic hashing
	input := "test input"
	hash1 := sha256Hex(input)
	hash2 := sha256Hex(input)

	if hash1 != hash2 {
		t.Errorf("sha256Hex is not deterministic: %s != %s", hash1, hash2)
	}

	// Test expected length (64 hex chars for 256 bits)
	if len(hash1) != 64 {
		t.Errorf("expected hash length 64, got %d", len(hash1))
	}

	// Test different inputs produce different hashes
	hash3 := sha256Hex("different input")
	if hash1 == hash3 {
		t.Error("different inputs should produce different hashes")
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
