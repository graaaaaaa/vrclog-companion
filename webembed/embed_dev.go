//go:build dev

// Package webembed provides the embedded web UI filesystem.
package webembed

import (
	"io/fs"
)

// GetFS returns nil in dev mode (no embedded files).
func GetFS() (fs.FS, error) {
	return nil, nil
}
