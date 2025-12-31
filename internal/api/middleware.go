package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/api/sseauth"
)

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
