//go:build !dev

// Package webembed provides the embedded web UI filesystem.
package webembed

import (
	"embed"
	"io/fs"
)

//go:embed dist
var webFS embed.FS

// GetFS returns the embedded web filesystem, or nil if not available.
func GetFS() (fs.FS, error) {
	return fs.Sub(webFS, "dist")
}
