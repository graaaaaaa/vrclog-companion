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
	"syscall"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/api"
	"github.com/graaaaa/vrclog-companion/internal/app"
	"github.com/graaaaa/vrclog-companion/internal/config"
	"github.com/graaaaa/vrclog-companion/internal/singleinstance"
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

	// 4. Determine bind address
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
		log.Println("Shutting down server...")
	case err := <-errCh:
		log.Printf("Server error: %v", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
