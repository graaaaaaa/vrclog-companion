package api

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter provides IP-based rate limiting using token bucket algorithm.
type RateLimiter struct {
	mu       sync.RWMutex
	limiters map[string]*visitorLimiter
	rate     rate.Limit // requests per second
	burst    int        // max burst size
	cleanup  time.Duration
	stopOnce sync.Once
	done     chan struct{}
}

type visitorLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiterConfig configures the rate limiter.
type RateLimiterConfig struct {
	// Rate is requests per second allowed
	Rate float64
	// Burst is the maximum burst size
	Burst int
	// CleanupInterval is how often to clean up old entries
	CleanupInterval time.Duration
}

// DefaultRateLimiterConfig returns sensible defaults for LAN mode.
// 10 requests/second with burst of 20 is generous for normal use
// but protects against abuse.
func DefaultRateLimiterConfig() RateLimiterConfig {
	return RateLimiterConfig{
		Rate:            10,
		Burst:           20,
		CleanupInterval: 5 * time.Minute,
	}
}

// NewRateLimiter creates a new IP-based rate limiter.
func NewRateLimiter(cfg RateLimiterConfig) *RateLimiter {
	rl := &RateLimiter{
		limiters: make(map[string]*visitorLimiter),
		rate:     rate.Limit(cfg.Rate),
		burst:    cfg.Burst,
		cleanup:  cfg.CleanupInterval,
		done:     make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// Allow checks if a request from the given IP should be allowed.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	v, exists := rl.limiters[ip]
	if !exists {
		v = &visitorLimiter{
			limiter:  rate.NewLimiter(rl.rate, rl.burst),
			lastSeen: time.Now(),
		}
		rl.limiters[ip] = v
	} else {
		v.lastSeen = time.Now()
	}
	rl.mu.Unlock()

	return v.limiter.Allow()
}

// cleanupLoop periodically removes old entries.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup_old()
		case <-rl.done:
			return
		}
	}
}

// cleanup_old removes limiters that haven't been used recently.
func (rl *RateLimiter) cleanup_old() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	threshold := time.Now().Add(-rl.cleanup * 2)
	for ip, v := range rl.limiters {
		if v.lastSeen.Before(threshold) {
			delete(rl.limiters, ip)
		}
	}
}

// Stop stops the cleanup goroutine.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() {
		close(rl.done)
	})
}

// Middleware returns an HTTP middleware that applies rate limiting.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)

		if !rl.Allow(ip) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// extractIP extracts the client IP from the request.
// For local/LAN use, RemoteAddr is trusted.
func extractIP(r *http.Request) string {
	// For LAN mode, we trust RemoteAddr directly
	// (no reverse proxy assumed)
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// AuthFailureLimiter tracks authentication failures per IP.
// This provides additional protection against brute force attacks.
type AuthFailureLimiter struct {
	mu       sync.RWMutex
	failures map[string]*authFailure
	maxFails int
	window   time.Duration
	lockout  time.Duration
}

type authFailure struct {
	count    int
	firstAt  time.Time
	lockedAt time.Time
}

// AuthFailureLimiterConfig configures auth failure limiting.
type AuthFailureLimiterConfig struct {
	MaxFailures   int           // Max failures before lockout
	Window        time.Duration // Time window for counting failures
	LockoutPeriod time.Duration // How long to lock out after max failures
}

// DefaultAuthFailureLimiterConfig returns sensible defaults.
func DefaultAuthFailureLimiterConfig() AuthFailureLimiterConfig {
	return AuthFailureLimiterConfig{
		MaxFailures:   5,
		Window:        5 * time.Minute,
		LockoutPeriod: 15 * time.Minute,
	}
}

// NewAuthFailureLimiter creates a new auth failure limiter.
func NewAuthFailureLimiter(cfg AuthFailureLimiterConfig) *AuthFailureLimiter {
	return &AuthFailureLimiter{
		failures: make(map[string]*authFailure),
		maxFails: cfg.MaxFailures,
		window:   cfg.Window,
		lockout:  cfg.LockoutPeriod,
	}
}

// IsLocked checks if an IP is currently locked out.
func (afl *AuthFailureLimiter) IsLocked(ip string) bool {
	afl.mu.RLock()
	defer afl.mu.RUnlock()

	f, exists := afl.failures[ip]
	if !exists {
		return false
	}

	if !f.lockedAt.IsZero() {
		return time.Since(f.lockedAt) < afl.lockout
	}

	return false
}

// RecordFailure records an authentication failure for an IP.
// Returns the number of remaining attempts, or -1 if now locked.
func (afl *AuthFailureLimiter) RecordFailure(ip string) int {
	afl.mu.Lock()
	defer afl.mu.Unlock()

	now := time.Now()
	f, exists := afl.failures[ip]

	if !exists {
		afl.failures[ip] = &authFailure{
			count:   1,
			firstAt: now,
		}
		return afl.maxFails - 1
	}

	// Check if window has expired, reset if so
	if now.Sub(f.firstAt) > afl.window {
		f.count = 1
		f.firstAt = now
		f.lockedAt = time.Time{}
		return afl.maxFails - 1
	}

	f.count++

	if f.count >= afl.maxFails {
		f.lockedAt = now
		return -1
	}

	return afl.maxFails - f.count
}

// RecordSuccess clears the failure record for an IP.
func (afl *AuthFailureLimiter) RecordSuccess(ip string) {
	afl.mu.Lock()
	defer afl.mu.Unlock()
	delete(afl.failures, ip)
}

// LockoutSecondsRemaining returns seconds until lockout expires.
func (afl *AuthFailureLimiter) LockoutSecondsRemaining(ip string) int {
	afl.mu.RLock()
	defer afl.mu.RUnlock()

	f, exists := afl.failures[ip]
	if !exists || f.lockedAt.IsZero() {
		return 0
	}

	remaining := afl.lockout - time.Since(f.lockedAt)
	if remaining <= 0 {
		return 0
	}

	return int(remaining.Seconds()) + 1
}

// Middleware returns an HTTP middleware that blocks locked IPs.
func (afl *AuthFailureLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)

		if afl.IsLocked(ip) {
			seconds := afl.LockoutSecondsRemaining(ip)
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
