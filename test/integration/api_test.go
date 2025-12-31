//go:build integration

package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// TestHealthEndpoint tests the /api/v1/health endpoint.
func TestHealthEndpoint(t *testing.T) {
	app := NewTestApp(t)
	defer app.Close()

	resp, err := http.Get(app.URL() + "/api/v1/health")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result["status"] != "healthy" {
		t.Errorf("expected status 'healthy', got %v", result["status"])
	}
}

// TestSecurityHeaders tests that security headers are present.
func TestSecurityHeaders(t *testing.T) {
	app := NewTestApp(t)
	defer app.Close()

	resp, err := http.Get(app.URL() + "/api/v1/health")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	headers := map[string]string{
		"X-Content-Type-Options":     "nosniff",
		"X-Frame-Options":            "DENY",
		"Referrer-Policy":            "strict-origin-when-cross-origin",
		"Cross-Origin-Opener-Policy": "same-origin",
	}

	for name, expected := range headers {
		actual := resp.Header.Get(name)
		if actual != expected {
			t.Errorf("header %s: expected %q, got %q", name, expected, actual)
		}
	}

	// CSP should be present
	csp := resp.Header.Get("Content-Security-Policy")
	if csp == "" {
		t.Error("Content-Security-Policy header is missing")
	}
}

// TestEventsEndpoint_NoAuth tests the /api/v1/events endpoint without auth.
func TestEventsEndpoint_NoAuth(t *testing.T) {
	app := NewTestApp(t)
	defer app.Close()

	// Insert a test event
	app.InsertTestEvent(t, "player_join", "TestPlayer")

	resp, err := http.Get(app.URL() + "/api/v1/events")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	items, ok := result["items"].([]interface{})
	if !ok {
		t.Fatalf("expected items array, got %T", result["items"])
	}

	if len(items) != 1 {
		t.Errorf("expected 1 event, got %d", len(items))
	}
}

// TestEventsEndpoint_Pagination tests cursor pagination.
func TestEventsEndpoint_Pagination(t *testing.T) {
	app := NewTestApp(t)
	defer app.Close()

	// Insert multiple events
	for i := 0; i < 5; i++ {
		app.InsertTestEvent(t, "player_join", "Player"+string(rune('A'+i)))
	}

	// Request with limit=2
	resp, err := http.Get(app.URL() + "/api/v1/events?limit=2")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	items, ok := result["items"].([]interface{})
	if !ok {
		t.Fatalf("expected items array, got %T", result["items"])
	}
	if len(items) != 2 {
		t.Errorf("expected 2 events, got %d", len(items))
	}

	// Check that next_cursor is present
	if result["next_cursor"] == nil {
		t.Error("expected next_cursor in response")
	}
}
