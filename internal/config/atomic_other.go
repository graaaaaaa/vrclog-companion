//go:build !windows

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// writeJSONAtomic writes data to path atomically using tmp->rename pattern.
// This ensures the file is never in a partial state.
// On POSIX systems, os.Rename atomically replaces the destination file.
func writeJSONAtomic(path string, v any) error {
	dir := filepath.Dir(path)

	// Ensure directory exists
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// Create temp file in same directory (required for atomic rename)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	// Clean up temp file on any error
	success := false
	defer func() {
		if !success {
			os.Remove(tmpName)
		}
	}()

	// Write JSON with indentation
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		tmp.Close()
		return err
	}

	// Sync to ensure data is written to disk
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}

	// Close before rename
	if err := tmp.Close(); err != nil {
		return err
	}

	// Atomic rename (on POSIX, this atomically replaces existing file)
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}

	success = true
	return nil
}
