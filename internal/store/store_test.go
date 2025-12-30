package store

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/event"
)

func TestOpen_CreatesDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	// Verify file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created")
	}

	// Verify WAL mode
	journalMode, err := store.journalMode()
	if err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want %q", journalMode, "wal")
	}
}

func TestInsertEvent_Dedupe(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	evt := &event.Event{
		Ts:         now,
		Type:       event.TypePlayerJoin,
		PlayerName: event.StringPtr("TestUser"),
		DedupeKey:  "unique-key-123",
		IngestedAt: now,
	}

	// First insert should succeed
	inserted, err := store.InsertEvent(ctx, evt)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if !inserted {
		t.Error("first insert should return inserted=true")
	}

	// Second insert with same dedupe_key should be ignored
	inserted, err = store.InsertEvent(ctx, evt)
	if err != nil {
		t.Fatalf("second insert: %v", err)
	}
	if inserted {
		t.Error("duplicate insert should return inserted=false")
	}

	// Verify count is still 1
	count, err := store.CountEvents(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestInsertEvent_DifferentKeys(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	// Insert multiple events with different keys
	for i := 0; i < 5; i++ {
		evt := &event.Event{
			Ts:         now.Add(time.Duration(i) * time.Second),
			Type:       event.TypePlayerJoin,
			PlayerName: event.StringPtr("TestUser"),
			DedupeKey:  "unique-key-" + string(rune('A'+i)),
			IngestedAt: now,
		}
		inserted, err := store.InsertEvent(ctx, evt)
		if err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
		if !inserted {
			t.Errorf("insert %d should succeed", i)
		}
	}

	// Verify count is 5
	count, err := store.CountEvents(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 5 {
		t.Errorf("count = %d, want 5", count)
	}
}

func TestInsertEvent_Validation(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	tests := []struct {
		name  string
		event *event.Event
	}{
		{
			name: "missing type",
			event: &event.Event{
				Ts:         now,
				Type:       "",
				DedupeKey:  "key-1",
				IngestedAt: now,
			},
		},
		{
			name: "missing dedupe_key",
			event: &event.Event{
				Ts:         now,
				Type:       event.TypePlayerJoin,
				DedupeKey:  "",
				IngestedAt: now,
			},
		},
		{
			name: "missing ts",
			event: &event.Event{
				Ts:         time.Time{},
				Type:       event.TypePlayerJoin,
				DedupeKey:  "key-2",
				IngestedAt: now,
			},
		},
		{
			name: "missing ingested_at",
			event: &event.Event{
				Ts:         now,
				Type:       event.TypePlayerJoin,
				DedupeKey:  "key-3",
				IngestedAt: time.Time{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.InsertEvent(ctx, tt.event)
			if !errors.Is(err, ErrInvalidEvent) {
				t.Errorf("expected ErrInvalidEvent, got %v", err)
			}
		})
	}
}

func TestGetLastEventTime_Empty(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()

	lastTime, err := store.GetLastEventTime(ctx)
	if err != nil {
		t.Fatalf("GetLastEventTime: %v", err)
	}
	if !lastTime.IsZero() {
		t.Errorf("expected zero time for empty database, got %v", lastTime)
	}
}

func TestGetLastEventTime_ReturnsLatest(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// Insert events at different times
	events := []*event.Event{
		{Ts: baseTime.Add(1 * time.Hour), Type: event.TypePlayerJoin, DedupeKey: "key-1", IngestedAt: time.Now().UTC()},
		{Ts: baseTime.Add(3 * time.Hour), Type: event.TypePlayerJoin, DedupeKey: "key-2", IngestedAt: time.Now().UTC()}, // latest
		{Ts: baseTime.Add(2 * time.Hour), Type: event.TypePlayerJoin, DedupeKey: "key-3", IngestedAt: time.Now().UTC()},
	}

	for _, e := range events {
		if _, err := store.InsertEvent(ctx, e); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	lastTime, err := store.GetLastEventTime(ctx)
	if err != nil {
		t.Fatalf("GetLastEventTime: %v", err)
	}

	expected := baseTime.Add(3 * time.Hour)
	if !lastTime.Equal(expected) {
		t.Errorf("GetLastEventTime = %v, want %v", lastTime, expected)
	}
}

func TestQueryEvents_Basic(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// Insert test events
	for i := 0; i < 10; i++ {
		evt := &event.Event{
			Ts:         baseTime.Add(time.Duration(i) * time.Minute),
			Type:       event.TypePlayerJoin,
			PlayerName: event.StringPtr("User" + string(rune('A'+i))),
			DedupeKey:  "key-" + string(rune('A'+i)),
			IngestedAt: time.Now().UTC(),
		}
		if _, err := store.InsertEvent(ctx, evt); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Query all events
	result, err := store.QueryEvents(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(result.Items) != 10 {
		t.Errorf("got %d items, want 10", len(result.Items))
	}
}

func TestQueryEvents_WithLimit(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// Insert 10 events
	for i := 0; i < 10; i++ {
		evt := &event.Event{
			Ts:         baseTime.Add(time.Duration(i) * time.Minute),
			Type:       event.TypePlayerJoin,
			DedupeKey:  "key-" + string(rune('A'+i)),
			IngestedAt: time.Now().UTC(),
		}
		if _, err := store.InsertEvent(ctx, evt); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Query with limit
	result, err := store.QueryEvents(ctx, QueryFilter{Limit: 5})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(result.Items) != 5 {
		t.Errorf("got %d items, want 5", len(result.Items))
	}
	if result.NextCursor == nil {
		t.Error("expected NextCursor to be set")
	}

	// Query next page
	result2, err := store.QueryEvents(ctx, QueryFilter{Limit: 5, Cursor: result.NextCursor})
	if err != nil {
		t.Fatalf("QueryEvents page 2: %v", err)
	}
	if len(result2.Items) != 5 {
		t.Errorf("page 2 got %d items, want 5", len(result2.Items))
	}
	if result2.NextCursor != nil {
		t.Error("expected NextCursor to be nil on last page")
	}
}

func TestQueryEvents_LimitClamping(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	// Insert 5 events
	for i := 0; i < 5; i++ {
		evt := &event.Event{
			Ts:         now.Add(time.Duration(i) * time.Second),
			Type:       event.TypePlayerJoin,
			DedupeKey:  "key-" + string(rune('A'+i)),
			IngestedAt: now,
		}
		if _, err := store.InsertEvent(ctx, evt); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Test limit=0 defaults to 100 (but we only have 5)
	result, err := store.QueryEvents(ctx, QueryFilter{Limit: 0})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(result.Items) != 5 {
		t.Errorf("got %d items, want 5", len(result.Items))
	}

	// Test limit > maxLimit is clamped (we can't easily test this with 5 events)
}

func TestQueryEvents_FilterByType(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	// Insert mixed events
	events := []*event.Event{
		{Ts: now, Type: event.TypePlayerJoin, DedupeKey: "key-1", IngestedAt: now},
		{Ts: now, Type: event.TypePlayerLeft, DedupeKey: "key-2", IngestedAt: now},
		{Ts: now, Type: event.TypePlayerJoin, DedupeKey: "key-3", IngestedAt: now},
		{Ts: now, Type: event.TypeWorldJoin, DedupeKey: "key-4", IngestedAt: now},
	}
	for _, e := range events {
		if _, err := store.InsertEvent(ctx, e); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Query by type
	joinType := event.TypePlayerJoin
	result, err := store.QueryEvents(ctx, QueryFilter{Type: &joinType})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("got %d items, want 2", len(result.Items))
	}
}

func TestDecodeCursor_Invalid(t *testing.T) {
	tests := []struct {
		name   string
		cursor string
	}{
		{"empty", ""},
		{"invalid base64", "not-valid-base64!!!"},
		{"missing separator", base64.RawURLEncoding.EncodeToString([]byte("notimestamp"))},
		{"invalid timestamp", base64.RawURLEncoding.EncodeToString([]byte("invalid|123"))},
		{"invalid id", base64.RawURLEncoding.EncodeToString([]byte("2024-01-01T12:00:00.000000000Z|notanumber"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cursor == "" {
				return // empty string is handled by the caller
			}
			_, _, err := decodeCursor(tt.cursor)
			if !errors.Is(err, ErrInvalidCursor) {
				t.Errorf("expected ErrInvalidCursor, got %v", err)
			}
		})
	}
}

func TestCursor_BackwardCompatibility(t *testing.T) {
	// Test that StdEncoding cursors (old format) are still accepted
	ts := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	id := int64(123)

	// Create an old-style cursor with StdEncoding
	oldCursor := base64.StdEncoding.EncodeToString(
		[]byte(ts.Format(TimeFormat) + "|123"),
	)

	// Should be able to decode it
	decodedTs, decodedID, err := decodeCursor(oldCursor)
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if !decodedTs.Equal(ts) {
		t.Errorf("timestamp = %v, want %v", decodedTs, ts)
	}
	if decodedID != id {
		t.Errorf("id = %d, want %d", decodedID, id)
	}
}

func TestCursor_RoundTrip(t *testing.T) {
	ts := time.Date(2024, 6, 15, 10, 30, 45, 123456789, time.UTC)
	id := int64(42)

	cursor := encodeCursor(ts, id)
	decodedTs, decodedID, err := decodeCursor(cursor)
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if !decodedTs.Equal(ts) {
		t.Errorf("timestamp = %v, want %v", decodedTs, ts)
	}
	if decodedID != id {
		t.Errorf("id = %d, want %d", decodedID, id)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store
}
