// Package main provides the entry point for VRClog Companion.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/api"
	"github.com/graaaaa/vrclog-companion/internal/app"
	"github.com/graaaaa/vrclog-companion/internal/version"
)

func main() {
	port := flag.String("port", "8080", "HTTP server port")
	flag.Parse()

	addr := "127.0.0.1:" + *port

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
