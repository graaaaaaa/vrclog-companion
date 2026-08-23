package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
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
	server.SetReady(true)

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
	server.SetReady(true)

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
	server.SetReady(true)

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
	server.SetReady(true)

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
	server.SetReady(true)

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
	server2.SetReady(true)

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/observations", nil)
	rec2 := httptest.NewRecorder()
	server2.mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("expected status %d (auth disabled with empty password), got %d", http.StatusOK, rec2.Code)
	}
}

func TestStateEndpoint(t *testing.T) {
	server := NewServer(":8080", testHealth(), WithStateUsecase(&mockState{}))
	server.SetReady(true)

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
	server.SetReady(true)

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

// TestNewServer_DefaultNotReady pins hardening spec §9.1: the constructor
// itself must default to not-ready, matching its own doc comment — callers
// that manage a startup rebuild call SetReady(true) once it completes, not
// SetReady(false) to opt into a behavior that should already be the
// default.
func TestNewServer_DefaultNotReady(t *testing.T) {
	server := NewServer(":8080", testHealth(), WithObservationsUsecase(&mockObservations{}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/observations", nil)
	rec := httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("NewServer without SetReady: expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
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

// TestNonSSERoute_WriteDeadline pins hardening spec §9.2: a non-SSE
// handler that never returns must be cut off (503, "request timed out")
// rather than holding the connection/response indefinitely — the
// server-global WriteTimeout is 0 to keep SSE alive, so this per-route
// http.TimeoutHandler bound is what actually protects normal endpoints.
func TestNonSSERoute_WriteDeadline(t *testing.T) {
	mockObs := &mockObservations{
		ListFunc: func(ctx context.Context, q store.ObservationQuery) ([]observation.StoredObservation, *int64, error) {
			<-ctx.Done() // never returns on its own; only the timeout ends this
			return nil, nil, ctx.Err()
		},
	}
	server := NewServer(":8080", testHealth(), WithObservationsUsecase(mockObs), withNonSSEHandlerTimeout(50*time.Millisecond))
	server.SetReady(true)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	start := time.Now()
	resp, err := ts.Client().Get(ts.URL + "/api/v1/observations")
	if err != nil {
		t.Fatalf("GET /api/v1/observations: %v", err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (TimeoutHandler cutoff)", resp.StatusCode, http.StatusServiceUnavailable)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("request took %v, want well under 2s (bounded by the 50ms test timeout)", elapsed)
	}
}

// TestSSERoute_NoWriteDeadline pins hardening spec §9.2's other half: the
// SSE route must NOT be wrapped in the same handler timeout — a long-lived
// SSE connection must survive well past the non-SSE bound.
func TestSSERoute_NoWriteDeadline(t *testing.T) {
	b := sse.NewBroadcaster()
	defer b.Stop()

	server := NewServer("127.0.0.1:0", testHealth(), WithBroadcaster(b, &mockSSEStore{}), withNonSSEHandlerTimeout(50*time.Millisecond))
	server.SetReady(true)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/stream", nil)
	resp, err := ts.Client().Do(req)
	if err != nil && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("request failed: %v", err)
	}
	if resp == nil {
		t.Fatal("no response received")
	}
	defer resp.Body.Close()

	// Even though the test's 50ms non-SSE timeout is far shorter than the
	// 300ms context deadline, the SSE connection must still be alive (200,
	// text/event-stream) when the context deadline — not the handler
	// timeout — ends it.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (SSE must not be cut off by the non-SSE handler timeout)", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
}

// TestStaticRoute_GatedByReadiness pins the code-review fix: the static
// SPA catch-all must respect the same readiness gate as every other
// non-SSE route, not bypass it — a browser opening the web UI during
// startup rebuild gets 503 for the shell itself, consistent with every
// API route, rather than silently loading an inconsistent early UI.
func TestStaticRoute_GatedByReadiness(t *testing.T) {
	webFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>ok</html>")},
	}
	server := NewServer(":8080", testHealth(), WithWebFS(webFS))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("not-ready: expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	server.SetReady(true)
	req = httptest.NewRequest(http.MethodGet, "/", nil)
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
