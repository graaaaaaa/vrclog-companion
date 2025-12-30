package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFrom_NotExist(t *testing.T) {
	// Load from non-existent file should return defaults
	cfg, err := LoadConfigFrom("/nonexistent/path/config.json")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	defaults := DefaultConfig()
	if cfg.Port != defaults.Port {
		t.Errorf("expected port %d, got %d", defaults.Port, cfg.Port)
	}
	if cfg.SchemaVersion != defaults.SchemaVersion {
		t.Errorf("expected schema version %d, got %d", defaults.SchemaVersion, cfg.SchemaVersion)
	}
}

func TestLoadConfigFrom_Corrupt(t *testing.T) {
	// Create temp file with corrupt JSON
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	if err := os.WriteFile(path, []byte("not valid json{{{"), 0600); err != nil {
		t.Fatal(err)
	}

	// Load should return defaults (with warning logged)
	cfg, err := LoadConfigFrom(path)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	defaults := DefaultConfig()
	if cfg.Port != defaults.Port {
		t.Errorf("expected default port %d, got %d", defaults.Port, cfg.Port)
	}
}

func TestLoadConfigFrom_InvalidVersion(t *testing.T) {
	// Create temp file with wrong schema version
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	content := `{"schema_version": 999, "port": 9999}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	// Load should return defaults due to version mismatch
	cfg, err := LoadConfigFrom(path)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	defaults := DefaultConfig()
	if cfg.Port != defaults.Port {
		t.Errorf("expected default port %d, got %d", defaults.Port, cfg.Port)
	}
}

func TestSaveLoadConfig_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	// Create custom config
	original := Config{
		SchemaVersion:     CurrentSchemaVersion,
		Port:              9000,
		LanEnabled:        true,
		LogPath:           "/custom/path",
		DiscordBatchSec:   5,
		AutoStartEnabled:  true,
		NotifyOnJoin:      false,
		NotifyOnLeave:     true,
		NotifyOnWorldJoin: false,
	}

	// Save
	if err := SaveConfigTo(original, path); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Load
	loaded, err := LoadConfigFrom(path)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Compare
	if loaded.Port != original.Port {
		t.Errorf("port mismatch: expected %d, got %d", original.Port, loaded.Port)
	}
	if loaded.LanEnabled != original.LanEnabled {
		t.Errorf("lan_enabled mismatch: expected %v, got %v", original.LanEnabled, loaded.LanEnabled)
	}
	if loaded.LogPath != original.LogPath {
		t.Errorf("log_path mismatch: expected %s, got %s", original.LogPath, loaded.LogPath)
	}
	if loaded.DiscordBatchSec != original.DiscordBatchSec {
		t.Errorf("discord_batch_sec mismatch: expected %d, got %d", original.DiscordBatchSec, loaded.DiscordBatchSec)
	}
	if loaded.NotifyOnJoin != original.NotifyOnJoin {
		t.Errorf("notify_on_join mismatch: expected %v, got %v", original.NotifyOnJoin, loaded.NotifyOnJoin)
	}
}

func TestLoadConfigFrom_NormalizesInvalidPort(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	// Create config with invalid port
	content := `{"schema_version": 1, "port": -1}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFrom(path)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	defaults := DefaultConfig()
	if cfg.Port != defaults.Port {
		t.Errorf("expected normalized port %d, got %d", defaults.Port, cfg.Port)
	}
}

func TestSecret_StringMasking(t *testing.T) {
	secret := Secret("my-super-secret-password")

	// String() should return [REDACTED]
	if s := secret.String(); s != "[REDACTED]" {
		t.Errorf("String() should return [REDACTED], got %s", s)
	}

	// GoString() should return [REDACTED]
	if s := secret.GoString(); s != "[REDACTED]" {
		t.Errorf("GoString() should return [REDACTED], got %s", s)
	}

	// Value() should return the actual value
	if v := secret.Value(); v != "my-super-secret-password" {
		t.Errorf("Value() should return actual value, got %s", v)
	}

	// fmt.Sprintf with %s should use String()
	formatted := fmt.Sprintf("%s", secret)
	if formatted != "[REDACTED]" {
		t.Errorf("%%s formatting should return [REDACTED], got %s", formatted)
	}

	// fmt.Sprintf with %v should use String()
	formatted = fmt.Sprintf("%v", secret)
	if formatted != "[REDACTED]" {
		t.Errorf("%%v formatting should return [REDACTED], got %s", formatted)
	}
}

func TestSecret_IsEmpty(t *testing.T) {
	empty := Secret("")
	if !empty.IsEmpty() {
		t.Error("empty secret should return IsEmpty() = true")
	}

	nonEmpty := Secret("value")
	if nonEmpty.IsEmpty() {
		t.Error("non-empty secret should return IsEmpty() = false")
	}
}

func TestSaveLoadSecrets_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "secrets.json")

	// Create custom secrets
	original := Secrets{
		SchemaVersion:     CurrentSchemaVersion,
		DiscordWebhookURL: Secret("https://discord.com/api/webhooks/xxx"),
		BasicAuthPassword: Secret("super-secret"),
	}

	// Save
	if err := SaveSecretsTo(original, path); err != nil {
		t.Fatalf("failed to save secrets: %v", err)
	}

	// Load
	loaded, err := LoadSecretsFrom(path)
	if err != nil {
		t.Fatalf("failed to load secrets: %v", err)
	}

	// Compare (using Value() to get actual values)
	if loaded.DiscordWebhookURL.Value() != original.DiscordWebhookURL.Value() {
		t.Errorf("discord_webhook_url mismatch")
	}
	if loaded.BasicAuthPassword.Value() != original.BasicAuthPassword.Value() {
		t.Errorf("basic_auth_password mismatch")
	}
}
