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

	if result["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", result["status"])
	}
	for _, secretLike := range []string{"secret", "password", "webhook_url", "path"} {
		if _, present := result[secretLike]; present {
			t.Errorf("health response must not include %q", secretLike)
		}
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

	csp := resp.Header.Get("Content-Security-Policy")
	if csp == "" {
		t.Error("Content-Security-Policy header is missing")
	}
}

// TestObservationsEndpoint_NoAuth tests the /api/v1/observations endpoint without auth.
func TestObservationsEndpoint_NoAuth(t *testing.T) {
	app := NewTestApp(t)
	defer app.Close()

	app.InsertPlayerJoined(t, "TestPlayer")

	resp, err := http.Get(app.URL() + "/api/v1/observations")
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
		t.Errorf("expected 1 observation, got %d", len(items))
	}

	item := items[0].(map[string]interface{})
	for _, forbidden := range []string{"path", "raw", "raw_line"} {
		if _, present := item[forbidden]; present {
			t.Errorf("observation item must not include %q", forbidden)
		}
	}
	if item["type"] != "player.joined" {
		t.Errorf("expected type player.joined, got %v", item["type"])
	}
}

// TestObservationsEndpoint_Pagination tests cursor pagination.
func TestObservationsEndpoint_Pagination(t *testing.T) {
	app := NewTestApp(t)
	defer app.Close()

	for i := 0; i < 5; i++ {
		app.InsertPlayerJoined(t, "Player"+string(rune('A'+i)))
	}

	resp, err := http.Get(app.URL() + "/api/v1/observations?limit=2")
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
		t.Errorf("expected 2 observations, got %d", len(items))
	}

	if result["next_cursor"] == nil {
		t.Error("expected next_cursor in response")
	}
}

// TestStateEndpoint reflects Projector state built from committed Observations.
func TestStateEndpoint(t *testing.T) {
	app := NewTestApp(t)
	defer app.Close()

	app.InsertPlayerJoined(t, "Alice")

	resp, err := http.Get(app.URL() + "/api/v1/state")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	players, ok := result["players"].([]interface{})
	if !ok || len(players) != 1 {
		t.Fatalf("expected 1 player, got %v", result["players"])
	}
}

// TestOldEndpointsGone verifies the pre-renewal /events and /now paths are
// no longer registered.
func TestOldEndpointsGone(t *testing.T) {
	app := NewTestApp(t)
	defer app.Close()

	for _, path := range []string{"/api/v1/events", "/api/v1/now"} {
		resp, err := http.Get(app.URL() + path)
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("legacy path %s: expected 404, got %d", path, resp.StatusCode)
		}
	}
}
