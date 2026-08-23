package ingest

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	vrclog "github.com/vrclog/vrclog-go"
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

// TestPublicErrorMessage_KnownSentinelsGetFixedMessages hardens the
// path-redaction fallback: known vrclog.Follow sentinel errors are
// returned as fixed, non-path-bearing messages via an allowlist, rather
// than relying solely on the best-effort regex fallback (which has a
// known gap for space-containing paths in non-fs.PathError error text).
func TestPublicErrorMessage_KnownSentinelsGetFixedMessages(t *testing.T) {
	if got := publicErrorMessage(vrclog.ErrNoLogDirectory); got != "no log directory available" {
		t.Errorf("publicErrorMessage(ErrNoLogDirectory) = %q", got)
	}
	if got := publicErrorMessage(vrclog.ErrCursorSourceMissing); got != "cursor source file not found" {
		t.Errorf("publicErrorMessage(ErrCursorSourceMissing) = %q", got)
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

// TestStateFailed verifies the terminal-state contract for a fatal ingest
// error: setFailed transitions to StateFailed and records a redacted error
// message, but — unlike recordError — does not increment RetryCount, since
// StateFailed is not a retry attempt.
func TestStateFailed(t *testing.T) {
	tr := newStatusTracker()
	tr.recordError(errors.New("transient failure 1"))
	tr.recordError(errors.New("transient failure 2"))

	before := tr.snapshot()
	if before.RetryCount != 2 {
		t.Fatalf("RetryCount before setFailed = %d, want 2", before.RetryCount)
	}

	tr.setFailed(ErrIntegrityViolation)

	snap := tr.snapshot()
	if snap.State != StateFailed {
		t.Fatalf("State = %s, want %s", snap.State, StateFailed)
	}
	if snap.RetryCount != before.RetryCount {
		t.Fatalf("RetryCount = %d, want unchanged from %d (setFailed is not a retry)", snap.RetryCount, before.RetryCount)
	}
	if snap.LastError != "integrity violation" {
		t.Fatalf("LastError = %q, want the fixed sentinel message", snap.LastError)
	}
}

// TestPublicErrorMessage_IntegrityViolation verifies both fatal-error
// sentinels map to safe, fixed strings for the unauthenticated health
// endpoint, matching the pattern already used for vrclog.Follow sentinels.
func TestPublicErrorMessage_IntegrityViolation(t *testing.T) {
	if got := publicErrorMessage(ErrIntegrityViolation); got != "integrity violation" {
		t.Errorf("publicErrorMessage(ErrIntegrityViolation) = %q", got)
	}
	if got := publicErrorMessage(ErrProjectionFailure); got != "projection failure" {
		t.Errorf("publicErrorMessage(ErrProjectionFailure) = %q", got)
	}

	// The production error chain wraps rich diagnostic detail (adapter ID,
	// rule ID) for internal logs via fmt.Errorf %w — publicErrorMessage
	// must collapse this to the fixed sentinel string, not leak the detail
	// to the unauthenticated health endpoint.
	wrapped := fmt.Errorf(
		"observation obs-123 conflicts with a differently-encoded stored row (adapter=vrchat.core rule=player_joined): %w",
		ErrIntegrityViolation,
	)
	if got := publicErrorMessage(wrapped); got != "integrity violation" {
		t.Errorf("publicErrorMessage(wrapped ErrIntegrityViolation) = %q, want %q", got, "integrity violation")
	}
}
