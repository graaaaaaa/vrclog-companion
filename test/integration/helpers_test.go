//go:build integration

// Package integration provides end-to-end integration tests for the VRClog Companion API.
package integration

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/api"
	"github.com/graaaaa/vrclog-companion/internal/app"
	"github.com/graaaaa/vrclog-companion/internal/event"
	"github.com/graaaaa/vrclog-companion/internal/store"
)

// TestApp holds all dependencies for integration tests.
type TestApp struct {
	Server *httptest.Server
	Store  *store.Store
	Hub    *api.Hub

	// Cleanup function to release resources
	cleanup func()
}

// NewTestApp creates a new test application with all dependencies wired up.
// Call cleanup() when done to release resources.
func NewTestApp(t *testing.T, opts ...TestAppOption) *TestApp {
	t.Helper()

	cfg := &testAppConfig{
		authEnabled: false,
		username:    "admin",
		password:    "password",
		sseSecret:   []byte("test-secret-key-32-bytes-long!!"),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Create temporary directory for test database
	tmpDir, err := os.MkdirTemp("", "vrclog-integration-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.sqlite")
	st, err := store.Open(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open store: %v", err)
	}

	// Create services
	healthService := &app.HealthService{}
	eventsService := &app.EventsService{Store: st}
	hub := api.NewHub()

	// Start hub
	go hub.Run()

	// Build server options
	serverOpts := []api.ServerOption{
		api.WithEventsUsecase(eventsService),
		api.WithHub(hub),
		api.WithSSESecret(cfg.sseSecret),
	}

	if cfg.authEnabled {
		serverOpts = append(serverOpts, api.WithBasicAuth(cfg.username, cfg.password))
	}

	// Create server (addr is ignored for httptest)
	server := api.NewServer("127.0.0.1:0", healthService, serverOpts...)

	// Create test server
	ts := httptest.NewServer(server.Handler())

	cleanup := func() {
		ts.Close()
		hub.Stop()
		st.Close()
		os.RemoveAll(tmpDir)
	}

	return &TestApp{
		Server:  ts,
		Store:   st,
		Hub:     hub,
		cleanup: cleanup,
	}
}

// Close releases all resources.
func (app *TestApp) Close() {
	if app.cleanup != nil {
		app.cleanup()
	}
}

// URL returns the base URL of the test server.
func (app *TestApp) URL() string {
	return app.Server.URL
}

// InsertTestEvent inserts a test event into the store.
func (app *TestApp) InsertTestEvent(t *testing.T, eventType, playerName string) int64 {
	t.Helper()

	now := time.Now().UTC()
	ts := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	ev := &event.Event{
		Type:       eventType,
		Ts:         ts,
		PlayerName: &playerName,
		DedupeKey:  "test-key-" + playerName + "-" + eventType + "-" + now.String(),
		IngestedAt: now,
	}

	id, inserted, err := app.Store.InsertEvent(context.Background(), ev)
	if err != nil {
		t.Fatalf("failed to insert event: %v", err)
	}
	if !inserted {
		t.Fatalf("event was not inserted (duplicate?)")
	}
	return id
}

// testAppConfig holds configuration for test app.
type testAppConfig struct {
	authEnabled bool
	username    string
	password    string
	sseSecret   []byte
}

// TestAppOption configures a test app.
type TestAppOption func(*testAppConfig)

// WithAuth enables authentication for the test app.
func WithAuth(username, password string) TestAppOption {
	return func(cfg *testAppConfig) {
		cfg.authEnabled = true
		cfg.username = username
		cfg.password = password
	}
}
