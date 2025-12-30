package singleinstance

import "testing"

func TestAcquireLock_Success(t *testing.T) {
	// AcquireLock should succeed (on macOS it's a no-op, on Windows it creates a mutex)
	release, ok, err := AcquireLock()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected lock to be acquired")
	}
	if release == nil {
		t.Error("release function should not be nil")
	}

	// Call release to clean up
	release()
}
