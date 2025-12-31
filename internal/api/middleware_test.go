package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// okHandler is a simple handler that returns 200 OK.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// --- CSRF Middleware Tests ---

func TestCSRFMiddleware_AllowsValidOrigin(t *testing.T) {
	mw := csrfMiddleware([]string{"example.com"})

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	mw(okHandler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_AllowsValidReferer(t *testing.T) {
	mw := csrfMiddleware([]string{"example.com"})

	req := httptest.NewRequest(http.MethodPut, "/test", nil)
	req.Header.Set("Referer", "https://example.com/page")
	rec := httptest.NewRecorder()

	mw(okHandler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_AllowsLocalhost(t *testing.T) {
	mw := csrfMiddleware([]string{}) // Empty allowlist

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("Origin", "http://localhost:8080")
	rec := httptest.NewRecorder()

	mw(okHandler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for localhost, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_RejectsInvalidOrigin(t *testing.T) {
	mw := csrfMiddleware([]string{"example.com"})

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()

	mw(okHandler).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_RejectsMissingOriginAndReferer(t *testing.T) {
	mw := csrfMiddleware([]string{"example.com"})

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	// No Origin or Referer header
	rec := httptest.NewRecorder()

	mw(okHandler).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_AllowsGETWithoutOrigin(t *testing.T) {
	mw := csrfMiddleware([]string{"example.com"})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// No Origin or Referer header
	rec := httptest.NewRecorder()

	mw(okHandler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for GET, got %d", rec.Code)
	}
}

// --- Basic Auth Middleware Tests ---

func TestBasicAuthMiddleware_ValidCredentials(t *testing.T) {
	mw := basicAuthMiddleware("admin", "secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()

	mw(okHandler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestBasicAuthMiddleware_MissingCredentials(t *testing.T) {
	mw := basicAuthMiddleware("admin", "secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// No Authorization header
	rec := httptest.NewRecorder()

	mw(okHandler).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header")
	}
}

func TestBasicAuthMiddleware_InvalidCredentials(t *testing.T) {
	mw := basicAuthMiddleware("admin", "secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.SetBasicAuth("admin", "wrong")
	rec := httptest.NewRecorder()

	mw(okHandler).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// --- Auth Failure Limiter Integration Tests ---

func TestBasicAuthMiddleware_LockoutAfterFailures(t *testing.T) {
	afl := NewAuthFailureLimiter(AuthFailureLimiterConfig{
		MaxFailures:   2,
		Window:        time.Minute,
		LockoutPeriod: 50 * time.Millisecond,
	})
	mw := basicAuthMiddleware("admin", "secret", afl)

	// First failure
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req1.RemoteAddr = "192.0.2.1:1234"
	req1.SetBasicAuth("admin", "wrong")
	rec1 := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on first failure, got %d", rec1.Code)
	}

	// Second failure - should trigger lockout
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.RemoteAddr = "192.0.2.1:1234"
	req2.SetBasicAuth("admin", "wrong")
	rec2 := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 on lockout, got %d", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on lockout")
	}

	// Valid credentials during lockout - should still be blocked
	req3 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req3.RemoteAddr = "192.0.2.1:1234"
	req3.SetBasicAuth("admin", "secret")
	rec3 := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 during lockout, got %d", rec3.Code)
	}
}

func TestBasicAuthMiddleware_LockoutRelease(t *testing.T) {
	afl := NewAuthFailureLimiter(AuthFailureLimiterConfig{
		MaxFailures:   1,
		Window:        time.Minute,
		LockoutPeriod: 30 * time.Millisecond,
	})
	mw := basicAuthMiddleware("admin", "secret", afl)

	// Trigger lockout
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req1.RemoteAddr = "192.0.2.1:1234"
	req1.SetBasicAuth("admin", "wrong")
	rec1 := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(rec1, req1)

	// Wait for lockout to expire
	time.Sleep(50 * time.Millisecond)

	// Should succeed now
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.RemoteAddr = "192.0.2.1:1234"
	req2.SetBasicAuth("admin", "secret")
	rec2 := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("expected 200 after lockout release, got %d", rec2.Code)
	}
}

func TestBasicAuthMiddleware_SuccessClearsFailures(t *testing.T) {
	afl := NewAuthFailureLimiter(AuthFailureLimiterConfig{
		MaxFailures:   2,
		Window:        time.Minute,
		LockoutPeriod: time.Minute,
	})
	mw := basicAuthMiddleware("admin", "secret", afl)

	// One failure
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req1.RemoteAddr = "192.0.2.1:1234"
	req1.SetBasicAuth("admin", "wrong")
	rec1 := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(rec1, req1)

	// Success - should clear failure count
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.RemoteAddr = "192.0.2.1:1234"
	req2.SetBasicAuth("admin", "secret")
	rec2 := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("expected 200 on success, got %d", rec2.Code)
	}

	// Another failure - should not trigger lockout since count was cleared
	req3 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req3.RemoteAddr = "192.0.2.1:1234"
	req3.SetBasicAuth("admin", "wrong")
	rec3 := httptest.NewRecorder()
	mw(okHandler).ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (not locked out), got %d", rec3.Code)
	}
}

// --- Security Headers Middleware Tests ---

func TestSecurityHeadersMiddleware(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	securityHeadersMiddleware(okHandler).ServeHTTP(rec, req)

	expectedHeaders := []string{
		"X-Content-Type-Options",
		"X-Frame-Options",
		"Referrer-Policy",
		"Content-Security-Policy",
		"Permissions-Policy",
		"Cross-Origin-Opener-Policy",
		"Cross-Origin-Resource-Policy",
	}

	for _, h := range expectedHeaders {
		if rec.Header().Get(h) == "" {
			t.Errorf("expected %s header to be set", h)
		}
	}
}

// --- Constant Time Comparison Tests ---

func TestConstantTimeEqualString(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"hello", "hello", true},
		{"hello", "world", false},
		{"", "", true},
		{"a", "", false},
		{"", "a", false},
		{"abc", "abd", false},
	}

	for _, tc := range tests {
		got := constantTimeEqualString(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("constantTimeEqualString(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
