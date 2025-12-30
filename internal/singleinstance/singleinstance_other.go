//go:build !windows

// Package singleinstance provides single instance control for the application.
package singleinstance

// AcquireLock is a no-op on non-Windows platforms.
// Single instance control is only implemented for Windows (the target platform).
//
// This allows development and testing on macOS/Linux without blocking.
//
// Returns:
//   - release: no-op function
//   - ok: always true
//   - err: always nil
func AcquireLock() (release func(), ok bool, err error) {
	return func() {}, true, nil
}
