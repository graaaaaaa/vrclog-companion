package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/graaaaa/vrclog-companion/internal/app"
)

func TestHealthEndpoint(t *testing.T) {
	health := app.HealthService{Version: "test-version"}
	server := NewServer(":8080", health)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}

	var resp app.HealthResult
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("expected status 'ok', got '%s'", resp.Status)
	}

	if resp.Version != "test-version" {
		t.Errorf("expected version 'test-version', got '%s'", resp.Version)
	}
}

func TestHealthEndpointMethodNotAllowed(t *testing.T) {
	health := app.HealthService{Version: "test-version"}
	server := NewServer(":8080", health)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/health", nil)
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	// Go 1.22's ServeMux returns 405 for wrong method
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}
