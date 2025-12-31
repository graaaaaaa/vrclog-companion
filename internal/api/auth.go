package api

import (
	"net/http"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/api/sseauth"
)

// tokenResponse is the response for POST /api/v1/auth/token.
type tokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"` // seconds
}

// handleAuthToken handles POST /api/v1/auth/token requests.
// Requires Basic Auth. Issues a short-lived SSE token.
func (s *Server) handleAuthToken(w http.ResponseWriter, r *http.Request) {
	if len(s.sseSecret) == 0 {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "SSE tokens not configured"})
		return
	}

	token, err := sseauth.GenerateToken(s.sseSecret, sseauth.ScopeSSE, time.Now())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "failed to generate token"})
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse{
		Token:     token,
		ExpiresIn: int(sseauth.DefaultTTL.Seconds()),
	})
}
