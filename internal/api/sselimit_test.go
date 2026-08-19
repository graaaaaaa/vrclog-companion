package api

import "testing"

func TestSSEConnLimiter_EnforcesPerIPLimit(t *testing.T) {
	l := newSSEConnLimiter()

	for i := 0; i < maxSSEConnectionsPerIP; i++ {
		if !l.acquire("1.2.3.4") {
			t.Fatalf("acquire %d/%d unexpectedly failed", i+1, maxSSEConnectionsPerIP)
		}
	}
	if l.acquire("1.2.3.4") {
		t.Fatal("expected acquire to fail once per-IP limit is reached")
	}

	// A different IP is unaffected by the first IP's exhausted budget.
	if !l.acquire("5.6.7.8") {
		t.Fatal("expected a different IP to still be able to acquire")
	}

	l.release("1.2.3.4")
	if !l.acquire("1.2.3.4") {
		t.Fatal("expected acquire to succeed again after a release")
	}
}

func TestSSEConnLimiter_EnforcesGlobalLimit(t *testing.T) {
	l := newSSEConnLimiter()

	for i := 0; i < maxGlobalSSEConnections; i++ {
		ip := "10.0.0." + string(rune('A'+i%26)) // spread across many IPs to isolate the global cap
		if !l.acquire(ip) {
			t.Fatalf("acquire %d/%d unexpectedly failed", i+1, maxGlobalSSEConnections)
		}
	}
	if l.acquire("10.0.0.zzz") {
		t.Fatal("expected acquire to fail once the global limit is reached, even from a fresh IP")
	}
}

func TestSSEConnLimiter_ReleaseCleansUpZeroEntries(t *testing.T) {
	l := newSSEConnLimiter()

	l.acquire("1.2.3.4")
	l.release("1.2.3.4")

	if _, ok := l.perIP["1.2.3.4"]; ok {
		t.Error("expected perIP entry to be removed once its count reaches zero")
	}
}
