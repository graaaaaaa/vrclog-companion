package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/app"
	"github.com/graaaaa/vrclog-companion/internal/event"
	"github.com/graaaaa/vrclog-companion/internal/store"
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

	if resp.Status != "healthy" {
		t.Errorf("expected status 'healthy', got '%s'", resp.Status)
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

func TestEventsEndpoint_RequiresAuthWhenEnabled(t *testing.T) {
	mockEvents := &MockEventsService{
		QueryFunc: func(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error) {
			return store.QueryResult{Items: []event.Event{}}, nil
		},
	}

	health := app.HealthService{Version: "test"}
	server := NewServer(":8080", health,
		WithEventsUsecase(mockEvents),
		WithBasicAuth("admin", "secret"),
	)

	// Request without auth header
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	if wwwAuth := rec.Header().Get("WWW-Authenticate"); wwwAuth == "" {
		t.Error("expected WWW-Authenticate header")
	}
}

func TestEventsEndpoint_SucceedsWithValidAuth(t *testing.T) {
	mockEvents := &MockEventsService{
		QueryFunc: func(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error) {
			return store.QueryResult{Items: []event.Event{}}, nil
		},
	}

	health := app.HealthService{Version: "test"}
	server := NewServer(":8080", health,
		WithEventsUsecase(mockEvents),
		WithBasicAuth("admin", "secret"),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestEventsEndpoint_FailsWithInvalidAuth(t *testing.T) {
	mockEvents := &MockEventsService{
		QueryFunc: func(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error) {
			return store.QueryResult{Items: []event.Event{}}, nil
		},
	}

	health := app.HealthService{Version: "test"}
	server := NewServer(":8080", health,
		WithEventsUsecase(mockEvents),
		WithBasicAuth("admin", "secret"),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.SetBasicAuth("admin", "wrongpassword")
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestStreamEndpoint_RequiresAuthWhenEnabled(t *testing.T) {
	mockEvents := &MockEventsService{
		QueryFunc: func(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error) {
			return store.QueryResult{Items: []event.Event{}}, nil
		},
	}

	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	health := app.HealthService{Version: "test"}
	server := NewServer(":8080", health,
		WithEventsUsecase(mockEvents),
		WithHub(hub),
		WithBasicAuth("admin", "secret"),
	)

	// Request without auth header
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stream", nil)
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	if wwwAuth := rec.Header().Get("WWW-Authenticate"); wwwAuth == "" {
		t.Error("expected WWW-Authenticate header")
	}
}

func TestHealthEndpoint_NoAuthRequired(t *testing.T) {
	mockEvents := &MockEventsService{}

	health := app.HealthService{Version: "test"}
	server := NewServer(":8080", health,
		WithEventsUsecase(mockEvents),
		WithBasicAuth("admin", "secret"),
	)

	// Health endpoint should work without auth even when auth is enabled
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestEventsEndpoint_NoAuthWhenDisabled(t *testing.T) {
	mockEvents := &MockEventsService{
		QueryFunc: func(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error) {
			return store.QueryResult{Items: []event.Event{}}, nil
		},
	}

	health := app.HealthService{Version: "test"}
	// No WithBasicAuth option - auth disabled
	server := NewServer(":8080", health, WithEventsUsecase(mockEvents))

	// Request without auth header should succeed when auth is disabled
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestStreamEndpoint_SucceedsWithValidAuth(t *testing.T) {
	mockEvents := &MockEventsService{
		QueryFunc: func(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error) {
			return store.QueryResult{Items: []event.Event{}}, nil
		},
	}

	hub := NewHub()
	go hub.Run()
	defer hub.Stop()

	health := app.HealthService{Version: "test"}
	server := NewServer(":8080", health,
		WithEventsUsecase(mockEvents),
		WithHub(hub),
		WithBasicAuth("admin", "secret"),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stream", nil)
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()

	// Use a context with timeout to avoid blocking forever
	ctx, cancel := context.WithTimeout(req.Context(), 50*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	server.mux.ServeHTTP(rec, req)

	// With valid auth, the handler should start (context timeout will end it)
	// Status 200 indicates successful auth and handler start
	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	// Verify SSE headers
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", ct)
	}
}

func TestWithBasicAuth_EmptyCredentials(t *testing.T) {
	mockEvents := &MockEventsService{
		QueryFunc: func(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error) {
			return store.QueryResult{Items: []event.Event{}}, nil
		},
	}

	health := app.HealthService{Version: "test"}

	// Empty username - auth should not be enabled
	server := NewServer(":8080", health,
		WithEventsUsecase(mockEvents),
		WithBasicAuth("", "secret"),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	// Should succeed without auth since empty username disables auth
	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d (auth disabled with empty username), got %d", http.StatusOK, rec.Code)
	}

	// Empty password - auth should not be enabled
	server2 := NewServer(":8080", health,
		WithEventsUsecase(mockEvents),
		WithBasicAuth("admin", ""),
	)

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rec2 := httptest.NewRecorder()

	server2.mux.ServeHTTP(rec2, req2)

	// Should succeed without auth since empty password disables auth
	if rec2.Code != http.StatusOK {
		t.Errorf("expected status %d (auth disabled with empty password), got %d", http.StatusOK, rec2.Code)
	}
}
