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

// MockEventsService implements app.EventsUsecase for testing.
type MockEventsService struct {
	QueryFunc func(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error)
}

func (m *MockEventsService) Query(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error) {
	if m.QueryFunc != nil {
		return m.QueryFunc(ctx, filter)
	}
	return store.QueryResult{}, nil
}

func TestEventsEndpoint_Success(t *testing.T) {
	now := time.Now().UTC()
	mockEvents := &MockEventsService{
		QueryFunc: func(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error) {
			return store.QueryResult{
				Items: []event.Event{
					{ID: 1, Type: event.TypePlayerJoin, Ts: now},
					{ID: 2, Type: event.TypeWorldJoin, Ts: now},
				},
				NextCursor: nil,
			}, nil
		},
	}

	health := app.HealthService{Version: "test"}
	server := NewServer(":8080", health, WithEventsUsecase(mockEvents))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var resp eventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(resp.Items))
	}

	if resp.NextCursor != nil {
		t.Error("expected NextCursor to be nil")
	}
}

func TestEventsEndpoint_WithFilters(t *testing.T) {
	var capturedFilter store.QueryFilter
	mockEvents := &MockEventsService{
		QueryFunc: func(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error) {
			capturedFilter = filter
			return store.QueryResult{Items: []event.Event{}}, nil
		},
	}

	health := app.HealthService{Version: "test"}
	server := NewServer(":8080", health, WithEventsUsecase(mockEvents))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?type=player_join&limit=50", nil)
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if capturedFilter.Type == nil || *capturedFilter.Type != event.TypePlayerJoin {
		t.Error("expected type filter to be player_join")
	}

	if capturedFilter.Limit != 50 {
		t.Errorf("expected limit 50, got %d", capturedFilter.Limit)
	}
}

func TestEventsEndpoint_InvalidType(t *testing.T) {
	mockEvents := &MockEventsService{}

	health := app.HealthService{Version: "test"}
	server := NewServer(":8080", health, WithEventsUsecase(mockEvents))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?type=invalid_type", nil)
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestEventsEndpoint_InvalidCursor(t *testing.T) {
	mockEvents := &MockEventsService{
		QueryFunc: func(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error) {
			return store.QueryResult{}, store.ErrInvalidCursor
		},
	}

	health := app.HealthService{Version: "test"}
	server := NewServer(":8080", health, WithEventsUsecase(mockEvents))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?cursor=invalid", nil)
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestEventsEndpoint_WithTimeFilters(t *testing.T) {
	var capturedFilter store.QueryFilter
	mockEvents := &MockEventsService{
		QueryFunc: func(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error) {
			capturedFilter = filter
			return store.QueryResult{Items: []event.Event{}}, nil
		},
	}

	health := app.HealthService{Version: "test"}
	server := NewServer(":8080", health, WithEventsUsecase(mockEvents))

	since := "2024-01-01T00:00:00Z"
	until := "2024-01-02T00:00:00Z"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?since="+since+"&until="+until, nil)
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if capturedFilter.Since == nil {
		t.Fatal("expected Since filter to be set")
	}
	if capturedFilter.Until == nil {
		t.Fatal("expected Until filter to be set")
	}

	expectedSince := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	expectedUntil := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	if !capturedFilter.Since.Equal(expectedSince) {
		t.Errorf("expected Since %v, got %v", expectedSince, *capturedFilter.Since)
	}
	if !capturedFilter.Until.Equal(expectedUntil) {
		t.Errorf("expected Until %v, got %v", expectedUntil, *capturedFilter.Until)
	}
}

func TestEventsEndpoint_EmptyResult(t *testing.T) {
	mockEvents := &MockEventsService{
		QueryFunc: func(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error) {
			return store.QueryResult{Items: nil}, nil
		},
	}

	health := app.HealthService{Version: "test"}
	server := NewServer(":8080", health, WithEventsUsecase(mockEvents))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp eventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Items should be an empty array, not null
	if resp.Items == nil {
		t.Error("expected Items to be an empty array, not nil")
	}
	if len(resp.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(resp.Items))
	}
}
