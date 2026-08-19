//go:build e2e

// Package e2e runs the most important end-to-end pipeline test: a real
// VRChat log fixture flows through vrclog.ReadFile -> Engine (core +
// community adapters) -> Store.CommitRecord -> Projector rebuild ->
// GET /api/v1/media/recent, and the original media URL must come out the
// other end unchanged.
package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	vrclog "github.com/vrclog/vrclog-go"

	"github.com/vrclog/vrclog-companion/internal/adapter"
	"github.com/vrclog/vrclog-companion/internal/api"
	"github.com/vrclog/vrclog-companion/internal/app"
	"github.com/vrclog/vrclog-companion/internal/projector"
	"github.com/vrclog/vrclog-companion/internal/sse"
	"github.com/vrclog/vrclog-companion/internal/store"
)

// runFixtureThroughPipeline reads fixturePath with vrclog.ReadFile, runs
// every Record through a freshly composed Engine (core + community
// adapters), commits each Result atomically, and rebuilds a Projector
// Manager from the resulting database — mirroring exactly what the
// production ingest Runner and startup rebuild do.
func runFixtureThroughPipeline(t *testing.T, fixturePath string) (*store.Store, *projector.Manager) {
	t.Helper()

	engine, _, err := adapter.BuildEngine()
	if err != nil {
		t.Fatalf("BuildEngine: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "e2e.sqlite")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	now := time.Now().UTC()

	for record, readErr := range vrclog.ReadFile(ctx, vrclog.ReadFileConfig{Path: fixturePath}) {
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			t.Fatalf("ReadFile: %v", readErr)
		}
		result := engine.Process(record)
		if _, err := st.CommitRecord(ctx, store.RecordCommit{
			Record:     record,
			Result:     result,
			IngestedAt: now,
		}); err != nil {
			t.Fatalf("CommitRecord: %v", err)
		}
	}

	manager := projector.NewManager()
	if err := manager.Rebuild(ctx, st.AllObservations(ctx)); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	return st, manager
}

// newMediaAPIServer wires a real api.Server backed by st/manager, exactly
// as cmd/vrclog-companion/main.go does, and returns an httptest.Server.
func newMediaAPIServer(t *testing.T, st *store.Store, manager *projector.Manager) *httptest.Server {
	t.Helper()

	health := app.HealthService{DB: st}
	mediaService := app.MediaService{Manager: manager}
	broadcaster := sse.NewBroadcaster()
	t.Cleanup(broadcaster.Stop)

	server := api.NewServer("127.0.0.1:0", health,
		api.WithMediaUsecase(mediaService),
		api.WithBroadcaster(broadcaster, st),
	)
	server.SetReady(true)

	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	return ts
}

type mediaAttemptDTO struct {
	ID              string                   `json:"id"`
	Status          string                   `json:"status"`
	BestOpenableURL string                   `json:"best_openable_url"`
	Resources       []map[string]interface{} `json:"resources"`
	Errors          []map[string]interface{} `json:"errors"`
}

func fetchMediaRecent(t *testing.T, baseURL string) []mediaAttemptDTO {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/v1/media/recent")
	if err != nil {
		t.Fatalf("GET /api/v1/media/recent: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/media/recent = %d: %s", resp.StatusCode, body)
	}

	// Assert no local filesystem path or raw log line ever reaches the API.
	bodyStr := string(body)
	if filepathContains(bodyStr, fixtureDir) {
		t.Fatalf("API response leaked a local filesystem path: %s", bodyStr)
	}

	var decoded struct {
		Attempts []mediaAttemptDTO `json:"attempts"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, bodyStr)
	}
	return decoded.Attempts
}

var fixtureDir = func() string {
	dir, _ := filepath.Abs("testdata")
	return dir
}()

func filepathContains(haystack, needle string) bool {
	return needle != "" && len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// TestE2E_YamaPlayer_MediaURLRecovery is the spec's most important E2E
// scenario: YamaPlayer source URL -> vrchat.core resolver relay -> vrchat.core
// resolved -> YamaPlayer video error, all correlating into one Attempt whose
// BestOpenableURL is the original YouTube URL, unmodified.
func TestE2E_YamaPlayer_MediaURLRecovery(t *testing.T) {
	const wantURL = "https://www.youtube.com/watch?v=TESTVIDEO01"
	const relayURL = "https://relay.example.invalid/TESTVIDEO01"

	fixture := filepath.Join("testdata", "yamaplayer_mixed.log")
	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("fixture missing: %v", err)
	}

	st, manager := runFixtureThroughPipeline(t, fixture)
	ts := newMediaAPIServer(t, st, manager)

	attempts := fetchMediaRecent(t, ts.URL)
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want exactly 1 (unambiguous correlation): %+v", len(attempts), attempts)
	}

	a := attempts[0]
	if a.BestOpenableURL != wantURL {
		t.Errorf("BestOpenableURL = %q, want original YouTube URL %q", a.BestOpenableURL, wantURL)
	}
	if a.Status != "failed" {
		t.Errorf("Status = %q, want failed", a.Status)
	}

	foundRelay := false
	for _, r := range a.Resources {
		if r["url"] == relayURL {
			foundRelay = true
		}
	}
	if !foundRelay {
		t.Errorf("relay/resolver URL %q missing from details: %+v", relayURL, a.Resources)
	}
}

// TestE2E_IwaSync3_MediaURLRecovery mirrors the YamaPlayer scenario for the
// iwaSync3 community adapter: the source URL is observed via vrchat.core
// (iwaSync3 itself only emits the PlayerError), and it must remain Best.
func TestE2E_IwaSync3_MediaURLRecovery(t *testing.T) {
	const wantURL = "https://www.youtube.com/watch?v=TESTVIDEO01"

	fixture := filepath.Join("testdata", "iwasync3_mixed.log")
	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("fixture missing: %v", err)
	}

	st, manager := runFixtureThroughPipeline(t, fixture)
	ts := newMediaAPIServer(t, st, manager)

	attempts := fetchMediaRecent(t, ts.URL)
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want exactly 1: %+v", len(attempts), attempts)
	}

	a := attempts[0]
	if a.BestOpenableURL != wantURL {
		t.Errorf("BestOpenableURL = %q, want %q", a.BestOpenableURL, wantURL)
	}
	if a.Status != "failed" {
		t.Errorf("Status = %q, want failed", a.Status)
	}
}
