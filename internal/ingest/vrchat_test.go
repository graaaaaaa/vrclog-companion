package ingest

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	vrclog "github.com/vrclog/vrclog-go"
)

// countingHandler counts every slog record handled, regardless of level.
type countingHandler struct {
	count *atomic.Int64
}

func (h countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h countingHandler) Handle(context.Context, slog.Record) error {
	h.count.Add(1)
	return nil
}
func (h countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h countingHandler) WithGroup(string) slog.Handler      { return h }

func TestVRChatSource_CursorMissingFallbackRunsOnce(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "output_log_2024-01-01_08-00-00.txt")
	if err := os.WriteFile(logPath, []byte("2024.01.01 08:00:00 Log        -  hello world\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	var warnCount atomic.Int64
	logger := slog.New(countingHandler{count: &warnCount})

	// A cursor whose SourceID cannot match any real file forces
	// vrclog.Follow to return ErrCursorSourceMissing on the first attempt.
	badCursor := &vrclog.Cursor{
		SourceID: "does-not-exist",
		Path:     filepath.Join(dir, "output_log_1999-01-01_00-00-00.txt"),
		Offset:   0,
		Line:     1,
	}

	factory := NewVRChatSourceFactory(VRChatSourceConfig{
		LogDir:       dir,
		PollInterval: 100 * time.Millisecond,
		Logger:       logger,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	source, err := factory.NewSource(ctx, badCursor)
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}

	var got []vrclog.Record
	for rec, recErr := range source.Records(ctx) {
		if recErr != nil {
			t.Fatalf("unexpected error from fallback read: %v", recErr)
		}
		got = append(got, rec)
	}

	if len(got) != 1 {
		t.Fatalf("got %d records, want 1 (fallback should read the existing file from the start)", len(got))
	}
	if warnCount.Load() != 1 {
		t.Fatalf("cursor-missing warning logged %d times, want exactly 1", warnCount.Load())
	}
}

func TestVRChatSource_NoCursorReadsLatestFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "output_log_2024-01-01_08-00-00.txt")
	if err := os.WriteFile(logPath, []byte("2024.01.01 08:00:00 Log        -  a\n2024.01.01 08:00:01 Log        -  b\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	factory := NewVRChatSourceFactory(VRChatSourceConfig{LogDir: dir, PollInterval: 100 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	source, err := factory.NewSource(ctx, nil)
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}

	var got []vrclog.Record
	for rec, recErr := range source.Records(ctx) {
		if recErr != nil {
			t.Fatalf("unexpected error: %v", recErr)
		}
		got = append(got, rec)
	}

	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
}

// TestVRChatSource_RotationIsFollowedAcrossFiles is an integration check
// that a second, later log file (log rotation) is picked up mid-stream and
// its Records carry the new SourceID. The exact rotation-detection timing
// is vrclog-go's responsibility; this only verifies Companion's thin
// wrapper forwards rotated Records without dropping or duplicating them.
func TestVRChatSource_RotationIsFollowedAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "output_log_2024-01-01_08-00-00.txt")
	if err := os.WriteFile(first, []byte("2024.01.01 08:00:00 Log        -  first\n"), 0o644); err != nil {
		t.Fatalf("write first log: %v", err)
	}

	factory := NewVRChatSourceFactory(VRChatSourceConfig{LogDir: dir, PollInterval: 100 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	source, err := factory.NewSource(ctx, nil)
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}

	recCh := make(chan vrclog.Record, 8)
	errCh := make(chan error, 1)
	go func() {
		for rec, recErr := range source.Records(ctx) {
			if recErr != nil {
				select {
				case errCh <- recErr:
				default:
				}
				return
			}
			recCh <- rec
		}
	}()

	first1 := <-recCh
	if first1.SourceID == "" || string(first1.Message) == "" {
		t.Fatalf("unexpected first record: %+v", first1)
	}

	// Write the rotated (newer-timestamped) file after a brief settle delay
	// so vrclog-go's rotation detection treats it as genuinely newer.
	time.Sleep(150 * time.Millisecond)
	second := filepath.Join(dir, "output_log_2024-01-01_09-00-00.txt")
	if err := os.WriteFile(second, []byte("2024.01.01 09:00:00 Log        -  second\n"), 0o644); err != nil {
		t.Fatalf("write rotated log: %v", err)
	}

	select {
	case rec := <-recCh:
		if rec.SourceID == first1.SourceID {
			t.Fatalf("expected rotated record to carry a new SourceID, got the same one: %s", rec.SourceID)
		}
	case err := <-errCh:
		t.Fatalf("unexpected error while waiting for rotated record: %v", err)
	case <-ctx.Done():
		t.Fatal("timed out waiting for rotated file's record")
	}
}
