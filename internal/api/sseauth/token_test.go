package sseauth

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateToken(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-key!!")
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	token, err := GenerateToken(secret, ScopeSSE, now)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	// Verify token format: prefix.payload.signature
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Errorf("expected 3 parts, got %d", len(parts))
	}

	if parts[0] != TokenPrefix {
		t.Errorf("expected prefix %q, got %q", TokenPrefix, parts[0])
	}

	// Token should start with "sse1."
	if !strings.HasPrefix(token, "sse1.") {
		t.Errorf("expected token to start with 'sse1.', got %q", token)
	}
}

func TestGenerateToken_EmptySecret(t *testing.T) {
	now := time.Now()
	_, err := GenerateToken(nil, ScopeSSE, now)
	if err == nil {
		t.Error("expected error for empty secret")
	}
}

func TestValidateToken_Success(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-key!!")
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	token, err := GenerateToken(secret, ScopeSSE, now)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	// Validate within TTL
	validateTime := now.Add(2 * time.Minute)
	claims, err := ValidateToken(token, secret, ScopeSSE, validateTime)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	if claims.Scope != ScopeSSE {
		t.Errorf("expected scope %q, got %q", ScopeSSE, claims.Scope)
	}

	expectedExp := now.Add(DefaultTTL).Unix()
	if claims.Exp != expectedExp {
		t.Errorf("expected exp %d, got %d", expectedExp, claims.Exp)
	}

	if claims.Iat != now.Unix() {
		t.Errorf("expected iat %d, got %d", now.Unix(), claims.Iat)
	}
}

func TestValidateToken_Expired(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-key!!")
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	token, err := GenerateToken(secret, ScopeSSE, now)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	// Validate after TTL
	validateTime := now.Add(DefaultTTL + time.Minute)
	_, err = ValidateToken(token, secret, ScopeSSE, validateTime)
	if err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestValidateToken_InvalidSignature(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-key!!")
	wrongSecret := []byte("wrong-secret-32-bytes-long-key!")
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	token, err := GenerateToken(secret, ScopeSSE, now)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	_, err = ValidateToken(token, wrongSecret, ScopeSSE, now)
	if err != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestValidateToken_InvalidScope(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-key!!")
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	token, err := GenerateToken(secret, ScopeSSE, now)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	_, err = ValidateToken(token, secret, "other-scope", now)
	if err != ErrInvalidScope {
		t.Errorf("expected ErrInvalidScope, got %v", err)
	}
}

func TestValidateToken_InvalidFormat(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-key!!")
	now := time.Now()

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"no dots", "nodots"},
		{"one dot", "one.dot"},
		{"wrong prefix", "xxx.payload.sig"},
		{"invalid base64 payload", "sse1.!!!.sig"},
		{"invalid base64 sig", "sse1.eyJleHAiOjE3MDQxMTA0MDAsImlhdCI6MTcwNDEwNjgwMCwic2NvcGUiOiJzc2UifQ.!!!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateToken(tt.token, secret, ScopeSSE, now)
			if err == nil {
				t.Error("expected error for invalid token")
			}
		})
	}
}

func TestValidateToken_EmptySecret(t *testing.T) {
	now := time.Now()
	_, err := ValidateToken("sse1.payload.sig", nil, ScopeSSE, now)
	if err == nil {
		t.Error("expected error for empty secret")
	}
}

func TestTokenRoundTrip(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-key!!")

	// Generate at time T
	genTime := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	token, err := GenerateToken(secret, ScopeSSE, genTime)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	// Should be valid at T+1min
	claims, err := ValidateToken(token, secret, ScopeSSE, genTime.Add(time.Minute))
	if err != nil {
		t.Fatalf("ValidateToken at T+1min failed: %v", err)
	}
	if claims.Scope != ScopeSSE {
		t.Errorf("wrong scope: %v", claims.Scope)
	}

	// Should be valid at T+4min (before expiry)
	_, err = ValidateToken(token, secret, ScopeSSE, genTime.Add(4*time.Minute))
	if err != nil {
		t.Fatalf("ValidateToken at T+4min failed: %v", err)
	}

	// Should be invalid at T+6min (after expiry)
	_, err = ValidateToken(token, secret, ScopeSSE, genTime.Add(6*time.Minute))
	if err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired at T+6min, got %v", err)
	}
}
