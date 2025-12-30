// Package config provides configuration management for VRClog Companion.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/graaaaa/vrclog-companion/internal/appinfo"
)

// DataDir returns the application data directory path.
// On Windows: %LOCALAPPDATA%/vrclog/
// On other platforms: ~/.config/vrclog/ or equivalent
func DataDir() (string, error) {
	var base string

	// On Windows, use LOCALAPPDATA; on other platforms, use UserConfigDir
	if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			base = localAppData
		} else {
			// Fallback if LOCALAPPDATA is not set (unusual for Windows)
			dir, err := os.UserConfigDir()
			if err != nil {
				return "", fmt.Errorf("get user config dir: %w", err)
			}
			base = dir
		}
	} else {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("get user config dir: %w", err)
		}
		base = dir
	}

	return filepath.Join(base, appinfo.DirName), nil
}

// EnsureDataDir creates the data directory if it doesn't exist.
func EnsureDataDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create data dir %q: %w", dir, err)
	}

	return dir, nil
}

// dataPath returns the full path for a file in the data directory.
func dataPath(filename string) (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filename), nil
}

// ConfigPath returns the path to config.json.
func ConfigPath() (string, error) {
	return dataPath(appinfo.ConfigFileName)
}

// SecretsPath returns the path to secrets.json.
func SecretsPath() (string, error) {
	return dataPath(appinfo.SecretsFileName)
}

// LockFilePath returns the path to the lock file for single instance control.
func LockFilePath() (string, error) {
	return dataPath(appinfo.LockFileName)
}

// DatabasePath returns the path to the SQLite database.
func DatabasePath() (string, error) {
	return dataPath(appinfo.DatabaseFileName)
}
