package ingest

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestNewVRClogSource_DefaultValues verifies default initialization.
func TestNewVRClogSource_DefaultValues(t *testing.T) {
	replaySince := time.Now().Add(-1 * time.Hour)
	src := NewVRClogSource(replaySince)

	if src.replaySince != replaySince {
		t.Errorf("expected replaySince=%v, got %v", replaySince, src.replaySince)
	}
	if src.logDir != "" {
		t.Errorf("expected empty logDir, got %q", src.logDir)
	}
	if src.waitForLogs != nil {
		t.Errorf("expected nil waitForLogs, got %v", *src.waitForLogs)
	}
	if src.logger == nil {
		t.Error("expected non-nil logger")
	}
	if src.eventBufferSize != DefaultEventBufferSize {
		t.Errorf("expected eventBufferSize=%d, got %d", DefaultEventBufferSize, src.eventBufferSize)
	}
	if src.errorBufferSize != DefaultErrorBufferSize {
		t.Errorf("expected errorBufferSize=%d, got %d", DefaultErrorBufferSize, src.errorBufferSize)
	}
}

// TestWithLogDir verifies logDir option.
func TestWithLogDir(t *testing.T) {
	src := NewVRClogSource(time.Time{}, WithLogDir("/custom/path"))
	if src.logDir != "/custom/path" {
		t.Errorf("expected logDir=/custom/path, got %q", src.logDir)
	}
}

// TestWithSourceLogger verifies logger option.
func TestWithSourceLogger(t *testing.T) {
	customLogger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	src := NewVRClogSource(time.Time{}, WithSourceLogger(customLogger))
	if src.logger != customLogger {
		t.Error("expected custom logger to be set")
	}
}

// TestWithSourceLogger_Nil verifies nil logger is ignored.
func TestWithSourceLogger_Nil(t *testing.T) {
	src := NewVRClogSource(time.Time{}, WithSourceLogger(nil))
	if src.logger == nil {
		t.Error("expected default logger when nil is passed")
	}
}

// TestWithWaitForLogsOption verifies waitForLogs option.
func TestWithWaitForLogsOption(t *testing.T) {
	tests := []struct {
		name     string
		wait     bool
		expected bool
	}{
		{"explicit true", true, true},
		{"explicit false", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := NewVRClogSource(time.Time{}, WithWaitForLogsOption(tt.wait))
			if src.waitForLogs == nil {
				t.Fatal("expected waitForLogs to be set")
			}
			if *src.waitForLogs != tt.expected {
				t.Errorf("expected waitForLogs=%v, got %v", tt.expected, *src.waitForLogs)
			}
		})
	}
}

// TestWithEventBufferSize verifies event buffer size option.
func TestWithEventBufferSize(t *testing.T) {
	src := NewVRClogSource(time.Time{}, WithEventBufferSize(128))
	if src.eventBufferSize != 128 {
		t.Errorf("expected eventBufferSize=128, got %d", src.eventBufferSize)
	}
}

// TestWithErrorBufferSize verifies error buffer size option.
func TestWithErrorBufferSize(t *testing.T) {
	src := NewVRClogSource(time.Time{}, WithErrorBufferSize(32))
	if src.errorBufferSize != 32 {
		t.Errorf("expected errorBufferSize=32, got %d", src.errorBufferSize)
	}
}

// TestWaitForLogsLogic verifies the wait-for-logs decision logic.
// This test documents the behavior without actually starting the watcher.
func TestWaitForLogsLogic(t *testing.T) {
	tests := []struct {
		name             string
		logDir           string
		waitForLogsOpt   *bool
		expectedWait     bool
		description      string
	}{
		{
			name:         "auto-detect default",
			logDir:       "",
			waitForLogsOpt: nil,
			expectedWait: true,
			description:  "auto-detect (empty logDir) should wait by default",
		},
		{
			name:         "explicit logDir default",
			logDir:       "/custom/path",
			waitForLogsOpt: nil,
			expectedWait: false,
			description:  "explicit logDir should not wait by default (fail fast)",
		},
		{
			name:         "auto-detect with explicit true",
			logDir:       "",
			waitForLogsOpt: boolPtr(true),
			expectedWait: true,
			description:  "auto-detect with explicit true should wait",
		},
		{
			name:         "auto-detect with explicit false",
			logDir:       "",
			waitForLogsOpt: boolPtr(false),
			expectedWait: false,
			description:  "auto-detect with explicit false should not wait",
		},
		{
			name:         "explicit logDir with override true",
			logDir:       "/custom/path",
			waitForLogsOpt: boolPtr(true),
			expectedWait: true,
			description:  "explicit logDir with override true should wait",
		},
		{
			name:         "explicit logDir with override false",
			logDir:       "/custom/path",
			waitForLogsOpt: boolPtr(false),
			expectedWait: false,
			description:  "explicit logDir with override false should not wait",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []SourceOption
			if tt.logDir != "" {
				opts = append(opts, WithLogDir(tt.logDir))
			}
			if tt.waitForLogsOpt != nil {
				opts = append(opts, WithWaitForLogsOption(*tt.waitForLogsOpt))
			}

			src := NewVRClogSource(time.Time{}, opts...)

			// Use the same logic as Start()
			waitForLogs := src.computeWaitForLogs()

			if waitForLogs != tt.expectedWait {
				t.Errorf("%s: expected waitForLogs=%v, got %v",
					tt.description, tt.expectedWait, waitForLogs)
			}
		})
	}
}

// boolPtr is a helper to create *bool for tests.
func boolPtr(b bool) *bool {
	return &b
}
