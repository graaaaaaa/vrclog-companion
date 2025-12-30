//go:build windows

// Package singleinstance provides single instance control for the application.
package singleinstance

import (
	"github.com/graaaaa/vrclog-companion/internal/appinfo"
	"golang.org/x/sys/windows"
)

// AcquireLock attempts to acquire a session-scoped lock to ensure only one
// instance of the application is running per user session.
//
// Returns:
//   - release: function to call when shutting down (use with defer)
//   - ok: true if lock was acquired, false if another instance is running
//   - err: error if something went wrong
//
// Usage:
//
//	release, ok, err := singleinstance.AcquireLock()
//	if err != nil { log.Fatal(err) }
//	if !ok { log.Println("Another instance is running"); return }
//	defer release()
func AcquireLock() (release func(), ok bool, err error) {
	name, err := windows.UTF16PtrFromString(appinfo.MutexName)
	if err != nil {
		return nil, false, err
	}

	// Try to create a named mutex
	h, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		// If ERROR_ALREADY_EXISTS, another instance has the mutex
		if err == windows.ERROR_ALREADY_EXISTS {
			// Close the handle we got (we don't own the mutex)
			if h != 0 {
				windows.CloseHandle(h)
			}
			return nil, false, nil
		}
		return nil, false, err
	}

	// We successfully created/acquired the mutex
	return func() {
		windows.CloseHandle(h)
	}, true, nil
}
