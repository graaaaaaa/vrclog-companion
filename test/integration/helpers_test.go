//go:build integration

// Package integration provides end-to-end integration tests for the VRClog Companion API.
package integration

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	vrclog "github.com/vrclog/vrclog-go"

	"github.com/vrclog/vrclog-companion/internal/api"
	"github.com/vrclog/vrclog-companion/internal/app"
	"github.com/vrclog/vrclog-companion/internal/projector"
	"github.com/vrclog/vrclog-companion/internal/sse"
	"github.com/vrclog/vrclog-companion/internal/store"
)

// TestApp holds all dependencies for integration tests.
type TestApp struct {
	Server      *httptest.Server
	Store       *store.Store
	Broadcaster *sse.Broadcaster
	Manager     *projector.Manager

	cleanup func()
}

// NewTestApp creates a new test application with all dependencies wired up.
// Call Close() when done to release resources.
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

	manager := projector.NewManager()
	broadcaster := sse.NewBroadcaster()

	healthService := app.HealthService{DB: st}
	observationsService := &app.ObservationsService{Store: st}
	stateService := app.StateService{Manager: manager}
	mediaService := app.MediaService{Manager: manager}
	statsService := app.NewStatsService(st, manager)

	serverOpts := []api.ServerOption{
		api.WithObservationsUsecase(observationsService),
		api.WithStateUsecase(stateService),
		api.WithMediaUsecase(mediaService),
		api.WithStatsUsecase(statsService),
		api.WithBroadcaster(broadcaster, st),
		api.WithSSESecret(cfg.sseSecret),
	}

	if cfg.authEnabled {
		serverOpts = append(serverOpts, api.WithBasicAuth(cfg.username, cfg.password))
	}

	server := api.NewServer("127.0.0.1:0", healthService, serverOpts...)
	server.SetReady(true)

	ts := httptest.NewServer(server.Handler())

	cleanup := func() {
		ts.Close()
		broadcaster.Stop()
		st.Close()
		os.RemoveAll(tmpDir)
	}

	return &TestApp{
		Server:      ts,
		Store:       st,
		Broadcaster: broadcaster,
		Manager:     manager,
		cleanup:     cleanup,
	}
}

// Close releases all resources.
func (a *TestApp) Close() {
	if a.cleanup != nil {
		a.cleanup()
	}
}

// URL returns the base URL of the test server.
func (a *TestApp) URL() string {
	return a.Server.URL
}

var testObsCounter int

// InsertPlayerJoined commits a synthetic player.joined Observation and
// applies it to the Projector Manager, returning its assigned sequence.
func (a *TestApp) InsertPlayerJoined(t *testing.T, playerName string) int64 {
	t.Helper()
	return a.insertOne(t, vrclog.PlayerJoined{Player: vrclog.Player{ID: "usr_" + playerName, DisplayName: playerName}})
}

func (a *TestApp) insertOne(t *testing.T, ev vrclog.Event) int64 {
	t.Helper()
	testObsCounter++

	now := time.Now().UTC()
	sourceID := vrclog.SourceID("test-source")
	record := vrclog.Record{
		ID:         vrclog.RecordID(fmt.Sprintf("rec-%d", testObsCounter)),
		Time:       now,
		SourceID:   sourceID,
		Path:       "/tmp/test.txt",
		Offset:     int64(testObsCounter * 10),
		NextOffset: int64((testObsCounter + 1) * 10),
		Line:       uint64(testObsCounter),
	}
	obs := vrclog.Observation{
		ID:        vrclog.ObservationID(fmt.Sprintf("obs-%d", testObsCounter)),
		Time:      now,
		AdapterID: "vrchat.core",
		RuleID:    "test_rule",
		Record: vrclog.RecordRef{
			ID: record.ID, SourceID: record.SourceID, Offset: record.Offset, Line: record.Line,
		},
		Event: ev,
	}

	result, err := a.Store.CommitRecord(context.Background(), store.RecordCommit{
		Record:     record,
		Result:     vrclog.Result{Observations: []vrclog.Observation{obs}},
		IngestedAt: now,
	})
	if err != nil {
		t.Fatalf("CommitRecord failed: %v", err)
	}
	if len(result.InsertedObservations) != 1 {
		t.Fatalf("expected 1 inserted observation, got %d", len(result.InsertedObservations))
	}

	stored := result.InsertedObservations[0]
	if _, err := a.Manager.Apply(stored); err != nil {
		t.Fatalf("Manager.Apply failed: %v", err)
	}
	a.Broadcaster.Broadcast(stored)

	return stored.Sequence
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
