package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	vrclog "github.com/vrclog/vrclog-go"

	"github.com/vrclog/vrclog-companion/internal/adapter"
	"github.com/vrclog/vrclog-companion/internal/app"
	"github.com/vrclog/vrclog-companion/internal/observation"
	"github.com/vrclog/vrclog-companion/internal/projector"
	"github.com/vrclog/vrclog-companion/internal/sse"
	"github.com/vrclog/vrclog-companion/internal/store"
)

// mockObservations implements app.ObservationsUsecase.
type mockObservations struct {
	ListFunc func(ctx context.Context, q store.ObservationQuery) ([]observation.StoredObservation, *int64, error)
}

func (m *mockObservations) List(ctx context.Context, q store.ObservationQuery) ([]observation.StoredObservation, *int64, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, q)
	}
	return nil, nil, nil
}

// mockState implements app.StateUsecase.
type mockState struct{}

func (m *mockState) GetCurrentState(ctx context.Context) projector.Snapshot {
	return projector.Snapshot{Players: []projector.PlayerInfo{}}
}

// mockSSEStore implements SSEObservationStore for stream tests.
type mockSSEStore struct{}

func (m *mockSSEStore) ObservationByID(ctx context.Context, id vrclog.ObservationID) (*observation.StoredObservation, error) {
	return nil, nil
}

func (m *mockSSEStore) ObservationsAfterSequence(ctx context.Context, sequence int64, limit int) ([]observation.StoredObservation, error) {
	return nil, nil
}

func (m *mockSSEStore) LatestSequence(ctx context.Context) (int64, error) {
	return 0, nil
}

func testHealth() app.HealthService {
	return app.HealthService{LoadedAdapters: 1}
}

func TestHealthEndpoint(t *testing.T) {
	server := NewServer(":8080", testHealth())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var resp app.HealthResult
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != app.StatusOK {
		t.Errorf("expected status %q, got %q", app.StatusOK, resp.Status)
	}
	if resp.LoadedAdapters != 1 {
		t.Errorf("expected loaded_adapters=1, got %d", resp.LoadedAdapters)
	}
}

func TestHealthEndpointMethodNotAllowed(t *testing.T) {
	server := NewServer(":8080", testHealth())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestObservationsEndpoint_RequiresAuthWhenEnabled(t *testing.T) {
	mockObs := &mockObservations{}
	server := NewServer(":8080", testHealth(),
		WithObservationsUsecase(mockObs),
		WithBasicAuth("admin", "secret"),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/observations", nil)
	rec := httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
	if wwwAuth := rec.Header().Get("WWW-Authenticate"); wwwAuth == "" {
		t.Error("expected WWW-Authenticate header")
	}
}

func TestObservationsEndpoint_SucceedsWithValidAuth(t *testing.T) {
	mockObs := &mockObservations{}
	server := NewServer(":8080", testHealth(),
		WithObservationsUsecase(mockObs),
		WithBasicAuth("admin", "secret"),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/observations", nil)
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestObservationsEndpoint_FailsWithInvalidAuth(t *testing.T) {
	mockObs := &mockObservations{}
	server := NewServer(":8080", testHealth(),
		WithObservationsUsecase(mockObs),
		WithBasicAuth("admin", "secret"),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/observations", nil)
	req.SetBasicAuth("admin", "wrongpassword")
	rec := httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestObservationsEndpoint_NoAuthWhenDisabled(t *testing.T) {
	mockObs := &mockObservations{}
	server := NewServer(":8080", testHealth(), WithObservationsUsecase(mockObs))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/observations", nil)
	rec := httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestHealthEndpoint_NoAuthRequired(t *testing.T) {
	mockObs := &mockObservations{}
	server := NewServer(":8080", testHealth(),
		WithObservationsUsecase(mockObs),
		WithBasicAuth("admin", "secret"),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestStreamEndpoint_RequiresAuthWhenEnabled(t *testing.T) {
	b := sse.NewBroadcaster()
	defer b.Stop()

	server := NewServer(":8080", testHealth(),
		WithBroadcaster(b, &mockSSEStore{}),
		WithBasicAuth("admin", "secret"),
	)

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

func TestStreamEndpoint_SucceedsWithValidAuth(t *testing.T) {
	b := sse.NewBroadcaster()
	defer b.Stop()

	server := NewServer(":8080", testHealth(),
		WithBroadcaster(b, &mockSSEStore{}),
		WithBasicAuth("admin", "secret"),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stream", nil)
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(req.Context(), 50*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", ct)
	}
}

func TestWithBasicAuth_EmptyCredentials(t *testing.T) {
	mockObs := &mockObservations{}
	health := testHealth()

	server := NewServer(":8080", health,
		WithObservationsUsecase(mockObs),
		WithBasicAuth("", "secret"),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/observations", nil)
	rec := httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d (auth disabled with empty username), got %d", http.StatusOK, rec.Code)
	}

	server2 := NewServer(":8080", health,
		WithObservationsUsecase(mockObs),
		WithBasicAuth("admin", ""),
	)

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/observations", nil)
	rec2 := httptest.NewRecorder()
	server2.mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("expected status %d (auth disabled with empty password), got %d", http.StatusOK, rec2.Code)
	}
}

func TestStateEndpoint(t *testing.T) {
	server := NewServer(":8080", testHealth(), WithStateUsecase(&mockState{}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/state", nil)
	rec := httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp stateResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.World != nil {
		t.Errorf("expected nil World, got %+v", resp.World)
	}
	if resp.Players == nil {
		t.Error("expected Players to be an empty array, not null")
	}
}

func TestAdaptersEndpoint(t *testing.T) {
	loaded := []adapter.LoadedAdapter{
		{ID: "vrchat.core", Origin: "core"},
		{ID: "community.yamaplayer", Origin: "community"},
	}
	server := NewServer(":8080", testHealth(), WithAdaptersUsecase(app.AdaptersService{Loaded: loaded}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/adapters", nil)
	rec := httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp adaptersResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Adapters) != 2 || resp.Adapters[0].ID != "vrchat.core" {
		t.Fatalf("Adapters = %+v, want [vrchat.core, community.yamaplayer] in order", resp.Adapters)
	}
}

func TestReadyGate_503sUntilReady(t *testing.T) {
	server := NewServer(":8080", testHealth(), WithObservationsUsecase(&mockObservations{}))
	server.SetReady(false)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/observations", nil)
	rec := httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("not-ready: expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	// /health is never gated — it reports rebuilding instead of 503ing.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec = httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health while not-ready: expected %d, got %d", http.StatusOK, rec.Code)
	}
	var health app.HealthResult
	if err := json.NewDecoder(rec.Body).Decode(&health); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if health.Ingest != "rebuilding" {
		t.Errorf("health.Ingest = %q, want %q", health.Ingest, "rebuilding")
	}

	server.SetReady(true)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/observations", nil)
	rec = httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ready: expected %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestOldEndpointsAreAbsent(t *testing.T) {
	server := NewServer(":8080", testHealth(),
		WithObservationsUsecase(&mockObservations{}),
		WithStateUsecase(&mockState{}),
	)

	for _, path := range []string{"/api/v1/events", "/api/v1/now"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		server.mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("legacy path %s: expected 404, got %d", path, rec.Code)
		}
	}
}
