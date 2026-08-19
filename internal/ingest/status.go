package ingest

import (
	"errors"
	"io/fs"
	"regexp"
	"sync"
	"time"
	"unicode/utf8"
)

// State is the coarse ingest health state reported by /api/v1/health.
type State string

const (
	StateRunning  State = "running"
	StateRetrying State = "retrying"
	StateStopped  State = "stopped"
)

// Status is a point-in-time snapshot of ingest health. It contains no
// path/URL data — only what health/diagnostics UIs need.
type Status struct {
	State        State
	LastRecordAt time.Time
	LastError    string
	RetryCount   uint64
}

// statusTracker is the thread-safe holder behind Runner.Status().
type statusTracker struct {
	mu     sync.RWMutex
	status Status
}

func newStatusTracker() *statusTracker {
	return &statusTracker{status: Status{State: StateRunning}}
}

func (t *statusTracker) snapshot() Status {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.status
}

func (t *statusTracker) setState(s State) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.State = s
}

func (t *statusTracker) recordSuccess(recordTime time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.State = StateRunning
	t.status.LastRecordAt = recordTime
	t.status.LastError = ""
	t.status.RetryCount = 0
}

func (t *statusTracker) recordError(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.State = StateRetrying
	t.status.LastError = publicErrorMessage(err)
	t.status.RetryCount++
}

// maxStatusErrorLen bounds the redacted message so a pathological error
// chain cannot grow the in-memory/health-exposed string unbounded.
const maxStatusErrorLen = 512

// windowsAbsPathPattern and unixAbsPathPattern match absolute filesystem
// paths so they can be redacted before ever leaving the process. This is
// deliberately conservative (it also catches non-path text that merely
// looks like one) since GET /api/v1/health is unauthenticated and must
// never leak local filesystem structure.
var (
	windowsAbsPathPattern = regexp.MustCompile(`(?i)[a-z]:\\[^\s:"]+`)
	unixAbsPathPattern    = regexp.MustCompile(`/[^\s:"]+`)
)

// publicErrorMessage redacts err into a string safe for the unauthenticated
// /api/v1/health endpoint (and any other future reader of ingest.Status).
// Redaction happens here, at write time, so every caller of Status()
// automatically gets a safe value rather than each call site having to
// remember to sanitize it.
func publicErrorMessage(err error) string {
	if err == nil {
		return ""
	}

	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		reason := "file error"
		if pathErr.Err != nil {
			reason = redactPaths(pathErr.Err.Error())
		}
		if pathErr.Op == "" {
			return truncateUTF8(reason, maxStatusErrorLen)
		}
		return truncateUTF8(pathErr.Op+": "+reason, maxStatusErrorLen)
	}

	return truncateUTF8(redactPaths(err.Error()), maxStatusErrorLen)
}

func redactPaths(s string) string {
	s = windowsAbsPathPattern.ReplaceAllString(s, "<path>")
	s = unixAbsPathPattern.ReplaceAllString(s, "<path>")
	return s
}

func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	b := []byte(s)[:max]
	for len(b) > 0 && !utf8.RuneStart(b[len(b)-1]) {
		b = b[:len(b)-1]
	}
	return string(b) + "…(truncated)"
}
