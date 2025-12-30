// Package version provides build version information.
package version

// Version is overridden at build time via ldflags.
// Example: go build -ldflags "-X github.com/graaaaa/vrclog-companion/internal/version.Version=0.1.0"
var Version = "dev"

// String returns the current version string.
func String() string {
	return Version
}
