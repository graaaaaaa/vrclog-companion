package ingest

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// TestPublicErrorMessage_RedactsFilesystemPaths pins the CRITICAL fix: raw
// ingest errors (which can originate from vrclog.Follow's underlying
// os.Open calls) must never carry a local filesystem path into
// ingest.Status.LastError, since that field is exposed verbatim by the
// unauthenticated GET /api/v1/health endpoint.
func TestPublicErrorMessage_RedactsFilesystemPaths(t *testing.T) {
	_, err := os.Open("/Users/someone/AppData/Local/vrclog/output_log_2026-01-01.txt")
	if err == nil {
		t.Fatal("expected os.Open to fail for a nonexistent path")
	}

	msg := publicErrorMessage(err)
	if strings.Contains(msg, "/Users/someone") {
		t.Fatalf("publicErrorMessage leaked a local path: %q", msg)
	}
	// fs.PathError structurally separates Path from the underlying reason
	// (e.g. "no such file or directory"); since publicErrorMessage never
	// touches pathErr.Path, there is no path text to redact here in the
	// first place — the <path> placeholder is only emitted by the regex
	// fallback for non-PathError errors (see the Windows-path test below).
	if !strings.Contains(msg, "open") {
		t.Fatalf("expected the operation to still be visible for diagnosability, got: %q", msg)
	}
}

func TestPublicErrorMessage_RedactsWindowsPaths(t *testing.T) {
	err := errors.New(`open C:\Users\someone\AppData\Local\vrclog\output_log.txt: access is denied`)
	msg := publicErrorMessage(err)
	if strings.Contains(msg, `C:\Users\someone`) {
		t.Fatalf("publicErrorMessage leaked a Windows path: %q", msg)
	}
	if !strings.Contains(msg, "<path>") {
		t.Fatalf("expected redacted path placeholder, got: %q", msg)
	}
}

func TestPublicErrorMessage_EmptyForNil(t *testing.T) {
	if got := publicErrorMessage(nil); got != "" {
		t.Fatalf("publicErrorMessage(nil) = %q, want empty", got)
	}
}

func TestPublicErrorMessage_TruncatesLongMessages(t *testing.T) {
	msg := publicErrorMessage(errors.New(strings.Repeat("x", 2000)))
	if len(msg) > maxStatusErrorLen+len("…(truncated)")+4 {
		t.Fatalf("publicErrorMessage did not truncate: len=%d", len(msg))
	}
}

func TestStatusTracker_RecordErrorRedacts(t *testing.T) {
	tr := newStatusTracker()
	_, openErr := os.Open("/Users/someone/secret/output_log.txt")
	tr.recordError(openErr)

	snap := tr.snapshot()
	if strings.Contains(snap.LastError, "/Users/someone/secret") {
		t.Fatalf("Status.LastError leaked a local path: %q", snap.LastError)
	}
}
