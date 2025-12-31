package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/api/sseauth"
)

// CORSConfig holds CORS middleware configuration.
type CORSConfig struct {
	AllowedOrigins   []string
	AllowCredentials bool
}

// corsMiddleware returns a middleware that handles CORS headers.
// Only origins in the allowlist are permitted.
func corsMiddleware(cfg CORSConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is in allowlist
			allowed := false
			for _, o := range cfg.AllowedOrigins {
				if o == origin {
					allowed = true
					break
				}
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				if cfg.AllowCredentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
			}

			// Handle preflight requests
			if r.Method == http.MethodOptions {
				if allowed {
					w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours
					w.WriteHeader(http.StatusNoContent)
				} else {
					w.WriteHeader(http.StatusForbidden)
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// csrfMiddleware returns a middleware that validates Origin/Referer headers
// for state-changing requests (POST, PUT, DELETE) to prevent CSRF attacks.
func csrfMiddleware(allowedHosts []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only check state-changing methods
			if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodDelete {
				next.ServeHTTP(w, r)
				return
			}

			// Check Origin header first
			origin := r.Header.Get("Origin")
			if origin != "" {
				originURL, err := url.Parse(origin)
				if err != nil || !isAllowedHost(originURL.Host, allowedHosts) {
					http.Error(w, "Forbidden: invalid origin", http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			// Fall back to Referer header
			referer := r.Header.Get("Referer")
			if referer != "" {
				refererURL, err := url.Parse(referer)
				if err != nil || !isAllowedHost(refererURL.Host, allowedHosts) {
					http.Error(w, "Forbidden: invalid referer", http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			// Neither Origin nor Referer present - reject for safety
			http.Error(w, "Forbidden: missing origin/referer", http.StatusForbidden)
		})
	}
}

// isAllowedHost checks if the host is in the allowed list.
// Allows localhost variants by default.
func isAllowedHost(host string, allowedHosts []string) bool {
	// Strip port from host
	hostWithoutPort := host
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		hostWithoutPort = host[:idx]
	}

	// Always allow localhost variants
	if hostWithoutPort == "localhost" || hostWithoutPort == "127.0.0.1" || hostWithoutPort == "::1" {
		return true
	}

	// Check against allowlist
	for _, allowed := range allowedHosts {
		allowedWithoutPort := allowed
		if idx := strings.LastIndex(allowed, ":"); idx != -1 {
			allowedWithoutPort = allowed[:idx]
		}
		if hostWithoutPort == allowedWithoutPort {
			return true
		}
	}

	return false
}

// securityHeadersMiddleware adds security headers to all responses.
// These headers protect against common web vulnerabilities.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "DENY")

		// Control referrer information
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Content Security Policy
		// Note: 'unsafe-inline' for style-src is needed for React inline styles
		csp := strings.Join([]string{
			"default-src 'self'",
			"script-src 'self'",
			"style-src 'self' 'unsafe-inline'",
			"img-src 'self' data:",
			"connect-src 'self'",
			"font-src 'self'",
			"base-uri 'none'",
			"frame-ancestors 'none'",
			"form-action 'self'",
		}, "; ")
		w.Header().Set("Content-Security-Policy", csp)

		// Restrict browser features
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		// Prevent cross-origin attacks
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")

		next.ServeHTTP(w, r)
	})
}

// constantTimeEqualString compares two strings in constant time.
// Uses SHA-256 hashing to ensure comparison time is independent of input lengths.
func constantTimeEqualString(a, b string) bool {
	ah := sha256.Sum256([]byte(a))
	bh := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ah[:], bh[:]) == 1
}

// basicAuthMiddleware returns a middleware that checks HTTP Basic Auth credentials.
// Uses constant-time comparison to prevent timing attacks.
func basicAuthMiddleware(username, password string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			if !ok {
				w.Header().Set("WWW-Authenticate", `Basic realm="VRClog Companion"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Constant-time comparison to prevent timing attacks
			usernameMatch := constantTimeEqualString(u, username)
			passwordMatch := constantTimeEqualString(p, password)

			if !usernameMatch || !passwordMatch {
				w.Header().Set("WWW-Authenticate", `Basic realm="VRClog Companion"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// sseTokenMiddleware returns a middleware that accepts either Basic Auth or SSE token.
// For SSE endpoints, token is passed via ?token=xxx query parameter.
func sseTokenMiddleware(username, password string, sseSecret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Try Basic Auth first
			if u, p, ok := r.BasicAuth(); ok {
				usernameMatch := constantTimeEqualString(u, username)
				passwordMatch := constantTimeEqualString(p, password)
				if usernameMatch && passwordMatch {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Try SSE token from query parameter
			token := r.URL.Query().Get("token")
			if token != "" && len(sseSecret) > 0 {
				_, err := sseauth.ValidateToken(token, sseSecret, sseauth.ScopeSSE, time.Now())
				if err == nil {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Neither auth method succeeded
			w.Header().Set("WWW-Authenticate", `Basic realm="VRClog Companion"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		})
	}
}
