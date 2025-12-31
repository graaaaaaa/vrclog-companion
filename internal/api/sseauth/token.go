// Package sseauth provides SSE token generation and validation.
package sseauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// TokenPrefix identifies the token version/type.
	TokenPrefix = "sse1"

	// DefaultTTL is the default token validity duration.
	DefaultTTL = 5 * time.Minute

	// ScopeSSE is the scope claim for SSE tokens.
	ScopeSSE = "sse"
)

// Errors returned by token validation.
var (
	ErrInvalidFormat    = errors.New("invalid token format")
	ErrInvalidSignature = errors.New("invalid token signature")
	ErrTokenExpired     = errors.New("token expired")
	ErrInvalidScope     = errors.New("invalid token scope")
)

// Claims represents the token payload.
type Claims struct {
	Exp   int64  `json:"exp"`   // Expiration time (Unix timestamp)
	Iat   int64  `json:"iat"`   // Issued at time (Unix timestamp)
	Scope string `json:"scope"` // Token scope (e.g., "sse")
}

// GenerateToken creates a new SSE token.
// Format: sse1.<payload_b64>.<sig_b64>
// Payload: {"exp":<unix>, "iat":<unix>, "scope":"sse"}
// Signature: HMAC-SHA256(secret, "sse1."+payload_b64)
func GenerateToken(secret []byte, scope string, now time.Time) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("secret cannot be empty")
	}

	claims := Claims{
		Exp:   now.Add(DefaultTTL).Unix(),
		Iat:   now.Unix(),
		Scope: scope,
	}

	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}

	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)
	sigInput := TokenPrefix + "." + payloadB64

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(sigInput))
	sig := mac.Sum(nil)
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return sigInput + "." + sigB64, nil
}

// ValidateToken verifies a token and returns its claims.
// Uses constant-time comparison for signature verification.
func ValidateToken(token string, secret []byte, expectedScope string, now time.Time) (Claims, error) {
	if len(secret) == 0 {
		return Claims{}, errors.New("secret cannot be empty")
	}

	// Split token into parts: prefix.payload.signature
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrInvalidFormat
	}

	prefix, payloadB64, sigB64 := parts[0], parts[1], parts[2]

	// Verify prefix
	if prefix != TokenPrefix {
		return Claims{}, ErrInvalidFormat
	}

	// Decode signature
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return Claims{}, ErrInvalidFormat
	}

	// Compute expected signature
	sigInput := prefix + "." + payloadB64
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(sigInput))
	expectedSig := mac.Sum(nil)

	// Constant-time comparison to prevent timing attacks
	if !hmac.Equal(sig, expectedSig) {
		return Claims{}, ErrInvalidSignature
	}

	// Decode and parse payload
	payloadJSON, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return Claims{}, ErrInvalidFormat
	}

	var claims Claims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return Claims{}, ErrInvalidFormat
	}

	// Check expiration
	if now.Unix() > claims.Exp {
		return Claims{}, ErrTokenExpired
	}

	// Check scope
	if claims.Scope != expectedScope {
		return Claims{}, ErrInvalidScope
	}

	return claims, nil
}
