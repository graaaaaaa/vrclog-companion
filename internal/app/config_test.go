package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestConfigService_UpdateConfig_ValidationErrorIsTyped pins the fix: bad
// client input (out-of-range port) must come back as *ValidationError so
// the HTTP handler can safely show its message, unlike a raw wrapped
// load/save error which may carry a filesystem path.
func TestConfigService_UpdateConfig_ValidationErrorIsTyped(t *testing.T) {
	dir := t.TempDir()
	svc := ConfigService{
		ConfigPath:  filepath.Join(dir, "config.json"),
		SecretsPath: filepath.Join(dir, "secrets.json"),
	}

	badPort := 70000
	_, err := svc.UpdateConfig(context.Background(), ConfigUpdateRequest{Port: &badPort})
	if err == nil {
		t.Fatal("expected an error for an out-of-range port")
	}

	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if verr.Message == "" {
		t.Fatal("ValidationError.Message must be non-empty")
	}
}

// TestConfigService_UpdateConfig_LoadFailureIsNotValidationError pins the
// other half of the fix: an internal load/save failure must NOT be a
// *ValidationError, so the API handler never returns its (potentially
// path-carrying) message verbatim to the client.
func TestConfigService_UpdateConfig_LoadFailureIsNotValidationError(t *testing.T) {
	dir := t.TempDir()
	// A config path that is itself a directory forces config.LoadConfigFrom
	// to fail with an OS-level error rather than falling back to defaults.
	badConfigPath := filepath.Join(dir, "config-is-a-dir")
	if err := os.MkdirAll(badConfigPath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	svc := ConfigService{
		ConfigPath:  badConfigPath,
		SecretsPath: filepath.Join(dir, "secrets.json"),
	}

	notify := true
	_, err := svc.UpdateConfig(context.Background(), ConfigUpdateRequest{NotifyOnJoin: &notify})
	if err == nil {
		t.Fatal("expected an error when ConfigPath is a directory")
	}

	var verr *ValidationError
	if errors.As(err, &verr) {
		t.Fatalf("internal load failure must not be a *ValidationError, got: %v", verr)
	}
}
