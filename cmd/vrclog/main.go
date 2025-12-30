// Package main provides the entry point for VRClog Companion.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/api"
	"github.com/graaaaa/vrclog-companion/internal/app"
	"github.com/graaaaa/vrclog-companion/internal/appinfo"
	"github.com/graaaaa/vrclog-companion/internal/config"
	"github.com/graaaaa/vrclog-companion/internal/ingest"
	"github.com/graaaaa/vrclog-companion/internal/singleinstance"
	"github.com/graaaaa/vrclog-companion/internal/store"
	"github.com/graaaaa/vrclog-companion/internal/version"
)

func main() {
	// 1. Single instance check (Windows: mutex, other: no-op)
	release, ok, err := singleinstance.AcquireLock()
	if err != nil {
		log.Fatalf("Failed to acquire lock: %v", err)
	}
	if !ok {
		log.Println("Another instance is already running")
		os.Exit(1)
	}
	defer release()

	// 2. Load configuration (corrupt config falls back to defaults with warning)
	cfg, _ := config.LoadConfig()
	// Note: secrets loaded but not used yet (for future Discord/Auth features)
	_, _ = config.LoadSecrets()

	// 3. Parse flags (port can override config)
	port := flag.Int("port", cfg.Port, "HTTP server port")
	flag.Parse()

	// 4. Open SQLite store
	dataDir, err := config.EnsureDataDir()
	if err != nil {
		log.Fatalf("Failed to ensure data directory: %v", err)
	}
	dbPath := filepath.Join(dataDir, appinfo.DatabaseFileName)
	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// 5. Create cancellable context for ingester
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 6. Calculate replay since time (5 minutes before last event, no cap)
	lastEventTime, err := db.GetLastEventTime(ctx)
	if err != nil {
		log.Printf("Warning: failed to get last event time: %v", err)
	}
	replaySince := ingest.CalculateReplaySince(lastEventTime, ingest.DefaultReplayRollback)
	if lastEventTime.IsZero() {
		log.Printf("No previous events, starting from now")
	} else {
		log.Printf("Replaying events since: %s", replaySince.Format(time.RFC3339))
	}

	// 7. Create event source (use config.LogPath if set)
	var sourceOpts []ingest.SourceOption
	if cfg.LogPath != "" {
		sourceOpts = append(sourceOpts, ingest.WithLogDir(cfg.LogPath))
	}
	source := ingest.NewVRClogSource(replaySince, sourceOpts...)
	ingester := ingest.New(source, db)

	// 8. Start ingestion in background goroutine
	go func() {
		if err := ingester.Run(ctx); err != nil {
			log.Printf("Ingester error: %v", err)
		}
	}()

	// 9. Determine bind address
	host := "127.0.0.1"
	if cfg.LanEnabled {
		host = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", host, *port)

	// Build dependencies
	health := app.HealthService{Version: version.String()}
	server := api.NewServer(addr, health)

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	// Error channel for server errors
	errCh := make(chan error, 1)

	go func() {
		log.Printf("Starting VRClog Companion v%s on %s", version.String(), addr)
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for shutdown signal or server error
	select {
	case <-done:
		log.Println("Shutting down...")
	case err := <-errCh:
		log.Printf("Server error: %v", err)
		os.Exit(1)
	}

	// Cancel ingester context first
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
