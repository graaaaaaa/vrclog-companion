package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter_Allow(t *testing.T) {
	cfg := RateLimiterConfig{
		Rate:            10,
		Burst:           5,
		CleanupInterval: time.Hour, // Long interval for test
	}
	rl := NewRateLimiter(cfg)
	defer rl.Stop()

	ip := "192.168.1.100"

	// First 5 requests should be allowed (burst)
	for i := 0; i < 5; i++ {
		if !rl.Allow(ip) {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 6th request should be denied (burst exceeded)
	if rl.Allow(ip) {
		t.Error("6th request should be denied")
	}
}

func TestRateLimiter_DifferentIPs(t *testing.T) {
	cfg := RateLimiterConfig{
		Rate:            10,
		Burst:           2,
		CleanupInterval: time.Hour,
	}
	rl := NewRateLimiter(cfg)
	defer rl.Stop()

	ip1 := "192.168.1.100"
	ip2 := "192.168.1.101"

	// Exhaust burst for ip1
	rl.Allow(ip1)
	rl.Allow(ip1)
	if rl.Allow(ip1) {
		t.Error("ip1 should be rate limited")
	}

	// ip2 should still be allowed
	if !rl.Allow(ip2) {
		t.Error("ip2 should be allowed")
	}
}

func TestRateLimiter_Middleware(t *testing.T) {
	cfg := RateLimiterConfig{
		Rate:            10,
		Burst:           2,
		CleanupInterval: time.Hour,
	}
	rl := NewRateLimiter(cfg)
	defer rl.Stop()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := rl.Middleware(handler)

	// First 2 requests should succeed
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		rec := httptest.NewRecorder()
		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	// 3rd request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec.Code)
	}

	if rec.Header().Get("Retry-After") != "1" {
		t.Errorf("expected Retry-After header")
	}
}

func TestAuthFailureLimiter_Lockout(t *testing.T) {
	cfg := AuthFailureLimiterConfig{
		MaxFailures:   3,
		Window:        time.Minute,
		LockoutPeriod: time.Minute,
	}
	afl := NewAuthFailureLimiter(cfg)

	ip := "192.168.1.100"

	// Not locked initially
	if afl.IsLocked(ip) {
		t.Error("should not be locked initially")
	}

	// Record 2 failures
	remaining := afl.RecordFailure(ip)
	if remaining != 2 {
		t.Errorf("expected 2 remaining, got %d", remaining)
	}

	remaining = afl.RecordFailure(ip)
	if remaining != 1 {
		t.Errorf("expected 1 remaining, got %d", remaining)
	}

	// 3rd failure should trigger lockout
	remaining = afl.RecordFailure(ip)
	if remaining != -1 {
		t.Errorf("expected -1 (locked), got %d", remaining)
	}

	// Should now be locked
	if !afl.IsLocked(ip) {
		t.Error("should be locked after max failures")
	}

	// Lockout seconds should be > 0
	if afl.LockoutSecondsRemaining(ip) <= 0 {
		t.Error("lockout seconds should be > 0")
	}
}

func TestAuthFailureLimiter_SuccessClears(t *testing.T) {
	cfg := AuthFailureLimiterConfig{
		MaxFailures:   3,
		Window:        time.Minute,
		LockoutPeriod: time.Minute,
	}
	afl := NewAuthFailureLimiter(cfg)

	ip := "192.168.1.100"

	// Record some failures
	afl.RecordFailure(ip)
	afl.RecordFailure(ip)

	// Successful auth should clear failures
	afl.RecordSuccess(ip)

	// Should be able to fail 3 times again
	remaining := afl.RecordFailure(ip)
	if remaining != 2 {
		t.Errorf("expected 2 remaining after success cleared, got %d", remaining)
	}
}
