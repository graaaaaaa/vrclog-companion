//go:build windows

package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// writeJSONAtomic writes data to path atomically using tmp->MoveFileEx pattern.
// This ensures the file is never in a partial state.
// On Windows, MoveFileEx with MOVEFILE_REPLACE_EXISTING is used to atomically
// replace the destination file (os.Rename fails if destination exists).
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

	// Convert paths to UTF16 for Windows API
	src, err := windows.UTF16PtrFromString(tmpName)
	if err != nil {
		return err
	}
	dst, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}

	// Atomic rename with replace (Windows-specific)
	// MOVEFILE_REPLACE_EXISTING = 0x1
	if err := windows.MoveFileEx(src, dst, windows.MOVEFILE_REPLACE_EXISTING); err != nil {
		return err
	}

	success = true
	return nil
}
