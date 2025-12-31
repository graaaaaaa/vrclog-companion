package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
)

// CurrentSchemaVersion is the current config schema version.
const CurrentSchemaVersion = 1

// Environment variable names for config overrides.
// Priority: Environment > Config File > Default
const (
	EnvPort              = "VRCLOG_PORT"
	EnvLanEnabled        = "VRCLOG_LAN_ENABLED"
	EnvLogPath           = "VRCLOG_LOG_PATH"
	EnvDiscordBatchSec   = "VRCLOG_DISCORD_BATCH_SEC"
	EnvAutoStart         = "VRCLOG_AUTO_START"
	EnvNotifyOnJoin      = "VRCLOG_NOTIFY_ON_JOIN"
	EnvNotifyOnLeave     = "VRCLOG_NOTIFY_ON_LEAVE"
	EnvNotifyOnWorldJoin = "VRCLOG_NOTIFY_ON_WORLD_JOIN"
)

// Config holds non-sensitive application configuration.
type Config struct {
	SchemaVersion      int    `json:"schema_version"`
	Port               int    `json:"port"`
	LanEnabled         bool   `json:"lan_enabled"`
	LogPath            string `json:"log_path"`
	DiscordBatchSec    int    `json:"discord_batch_sec"`
	AutoStartEnabled   bool   `json:"auto_start_enabled"`
	NotifyOnJoin       bool   `json:"notify_on_join"`
	NotifyOnLeave      bool   `json:"notify_on_leave"`
	NotifyOnWorldJoin  bool   `json:"notify_on_world_join"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		SchemaVersion:      CurrentSchemaVersion,
		Port:               8080,
		LanEnabled:         false,
		LogPath:            "", // auto-detect
		DiscordBatchSec:    3,
		AutoStartEnabled:   false,
		NotifyOnJoin:       true,
		NotifyOnLeave:      true,
		NotifyOnWorldJoin:  true,
	}
}

// LoadConfig reads config from disk. If the file doesn't exist or is corrupt,
// it returns DefaultConfig with a warning logged (non-fatal).
func LoadConfig() (Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return DefaultConfig(), err
	}

	return LoadConfigFrom(path)
}

// LoadConfigFrom reads config from the specified path.
func LoadConfigFrom(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// File doesn't exist, use defaults (not an error)
			return cfg, nil
		}
		log.Printf("Warning: failed to read config file: %v, using defaults", err)
		return cfg, nil
	}

	// Try to parse JSON
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&cfg); err != nil {
		log.Printf("Warning: config file is corrupt: %v, using defaults", err)
		return DefaultConfig(), nil
	}

	// Check schema version
	if cfg.SchemaVersion != CurrentSchemaVersion {
		log.Printf("Warning: config schema version mismatch (got %d, expected %d), using defaults",
			cfg.SchemaVersion, CurrentSchemaVersion)
		return DefaultConfig(), nil
	}

	// Normalize/validate values
	cfg = normalizeConfig(cfg)

	return cfg, nil
}

// normalizeConfig validates and normalizes config values.
func normalizeConfig(cfg Config) Config {
	defaults := DefaultConfig()

	// Ensure schema version
	cfg.SchemaVersion = CurrentSchemaVersion

	// Validate port
	if cfg.Port <= 0 || cfg.Port > 65535 {
		cfg.Port = defaults.Port
	}

	// Validate batch seconds
	if cfg.DiscordBatchSec < 0 {
		cfg.DiscordBatchSec = defaults.DiscordBatchSec
	}

	return cfg
}

// SaveConfig writes config to disk atomically.
func SaveConfig(cfg Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	return SaveConfigTo(cfg, path)
}

// SaveConfigTo writes config to the specified path atomically.
func SaveConfigTo(cfg Config, path string) error {
	// Ensure schema version is set
	cfg.SchemaVersion = CurrentSchemaVersion

	return writeJSONAtomic(path, cfg)
}

// ApplyEnvOverrides applies environment variable overrides to the config.
// Environment variables take highest priority over config file values.
func ApplyEnvOverrides(cfg Config) Config {
	// Port
	if v := os.Getenv(EnvPort); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 && port <= 65535 {
			cfg.Port = port
		}
	}

	// LAN enabled
	if v := os.Getenv(EnvLanEnabled); v != "" {
		cfg.LanEnabled = parseBool(v)
	}

	// Log path
	if v := os.Getenv(EnvLogPath); v != "" {
		cfg.LogPath = v
	}

	// Discord batch seconds
	if v := os.Getenv(EnvDiscordBatchSec); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec >= 0 {
			cfg.DiscordBatchSec = sec
		}
	}

	// Auto start
	if v := os.Getenv(EnvAutoStart); v != "" {
		cfg.AutoStartEnabled = parseBool(v)
	}

	// Notify on join
	if v := os.Getenv(EnvNotifyOnJoin); v != "" {
		cfg.NotifyOnJoin = parseBool(v)
	}

	// Notify on leave
	if v := os.Getenv(EnvNotifyOnLeave); v != "" {
		cfg.NotifyOnLeave = parseBool(v)
	}

	// Notify on world join
	if v := os.Getenv(EnvNotifyOnWorldJoin); v != "" {
		cfg.NotifyOnWorldJoin = parseBool(v)
	}

	return cfg
}

// parseBool parses a boolean from various string representations.
// Accepts: "true", "1", "yes", "on" (case-insensitive) as true.
// All other values are treated as false.
func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "1" || s == "yes" || s == "on"
}
