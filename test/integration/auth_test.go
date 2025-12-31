//go:build integration

package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// TestAuth_HealthNoAuthRequired tests that health endpoint doesn't require auth.
func TestAuth_HealthNoAuthRequired(t *testing.T) {
	app := NewTestApp(t, WithAuth("admin", "secret123"))
	defer app.Close()

	resp, err := http.Get(app.URL() + "/api/v1/health")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Health endpoint should work without auth
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

// TestAuth_EventsRequiresAuth tests that events endpoint requires auth when enabled.
func TestAuth_EventsRequiresAuth(t *testing.T) {
	app := NewTestApp(t, WithAuth("admin", "secret123"))
	defer app.Close()

	// Request without auth
	resp, err := http.Get(app.URL() + "/api/v1/events")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", resp.StatusCode)
	}

	// Check WWW-Authenticate header
	wwwAuth := resp.Header.Get("WWW-Authenticate")
	if wwwAuth == "" {
		t.Error("expected WWW-Authenticate header")
	}
}

// TestAuth_BasicAuth tests successful authentication with Basic Auth.
func TestAuth_BasicAuth(t *testing.T) {
	app := NewTestApp(t, WithAuth("admin", "secret123"))
	defer app.Close()

	// Insert a test event
	app.InsertTestEvent(t, "player_join", "TestPlayer")

	// Request with correct auth
	req, _ := http.NewRequest("GET", app.URL()+"/api/v1/events", nil)
	req.SetBasicAuth("admin", "secret123")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected status 200, got %d: %s", resp.StatusCode, body)
	}
}

// TestAuth_WrongCredentials tests rejection of wrong credentials.
func TestAuth_WrongCredentials(t *testing.T) {
	app := NewTestApp(t, WithAuth("admin", "secret123"))
	defer app.Close()

	req, _ := http.NewRequest("GET", app.URL()+"/api/v1/events", nil)
	req.SetBasicAuth("admin", "wrong-password")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", resp.StatusCode)
	}
}

// TestAuth_SSETokenFlow tests the SSE token issuance and usage flow.
func TestAuth_SSETokenFlow(t *testing.T) {
	app := NewTestApp(t, WithAuth("admin", "secret123"))
	defer app.Close()

	// Step 1: Get SSE token with Basic Auth
	req, _ := http.NewRequest("POST", app.URL()+"/api/v1/auth/token", nil)
	req.SetBasicAuth("admin", "secret123")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, body)
	}

	body, _ := io.ReadAll(resp.Body)
	var tokenResp map[string]interface{}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	token, ok := tokenResp["token"].(string)
	if !ok || token == "" {
		t.Fatalf("expected token in response, got: %v", tokenResp)
	}

	// Step 2: Use token to access SSE stream endpoint
	// Note: We can't fully test SSE streaming here, but we can verify the token is accepted
	streamReq, _ := http.NewRequest("GET", app.URL()+"/api/v1/stream?token="+token, nil)
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("failed to make stream request: %v", err)
	}
	defer streamResp.Body.Close()

	// Should get 200 (not 401) when token is valid
	if streamResp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for stream with token, got %d", streamResp.StatusCode)
	}
}
