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
	"github.com/graaaaa/vrclog-companion/internal/derive"
	"github.com/graaaaa/vrclog-companion/internal/event"
	"github.com/graaaaa/vrclog-companion/internal/ingest"
	"github.com/graaaaa/vrclog-companion/internal/notify"
	"github.com/graaaaa/vrclog-companion/internal/singleinstance"
	"github.com/graaaaa/vrclog-companion/internal/store"
	"github.com/graaaaa/vrclog-companion/internal/version"
	"github.com/graaaaa/vrclog-companion/webembed"
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
	// Apply environment variable overrides (highest priority)
	cfg = config.ApplyEnvOverrides(cfg)
	secrets, secretsStatus, err := config.LoadSecrets()
	if err != nil {
		log.Printf("Warning: %v", err)
	}

	// 3. Ensure LAN auth credentials if LAN mode is enabled
	updated, generatedPw, err := config.EnsureLanAuth(&secrets, cfg.LanEnabled)
	if err != nil {
		log.Fatalf("Failed to ensure LAN auth: %v", err)
	}

	// Ensure SSE secret exists (always needed for token generation)
	sseUpdated, err := config.EnsureSSESecret(&secrets)
	if err != nil {
		log.Fatalf("Failed to ensure SSE secret: %v", err)
	}
	updated = updated || sseUpdated

	// Only save if loaded successfully or file was missing (prevent overwrite on fallback)
	if updated && secretsStatus != config.SecretsFallback {
		if err := config.SaveSecrets(secrets); err != nil {
			log.Fatalf("Failed to save secrets: %v", err)
		}
		if generatedPw != "" {
			// Write password to file instead of logging
			pwPath, err := config.WritePasswordFile(secrets.BasicAuthUsername, generatedPw)
			if err != nil {
				log.Printf("Warning: failed to write password file: %v", err)
				// Fallback to log output if file write fails
				log.Println("=== GENERATED BASIC AUTH CREDENTIALS ===")
				log.Printf("Username: %s", secrets.BasicAuthUsername)
				log.Printf("Password: %s", generatedPw)
				log.Println("=========================================")
			} else {
				log.Println("=== BASIC AUTH CREDENTIALS GENERATED ===")
				log.Printf("Credentials saved to: %s", pwPath)
				log.Println("Delete this file after saving the credentials!")
				log.Println("=========================================")
			}
		}
	} else if updated && secretsStatus == config.SecretsFallback {
		log.Println("WARNING: Secrets file has errors; new credentials not saved to avoid data loss")
		log.Println("Please fix or delete secrets.json and restart")
	}

	// 4. Parse flags (port can override config)
	port := flag.Int("port", cfg.Port, "HTTP server port")
	flag.Parse()

	// 5. Open SQLite store
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

	// Run VACUUM if needed (every 30 days)
	if vacuumed, err := db.VacuumIfNeeded(context.Background()); err != nil {
		log.Printf("Warning: VACUUM check failed: %v", err)
	} else if vacuumed {
		log.Println("Database maintenance completed")
	}

	// 6. Create cancellable context for ingester
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 7. Calculate replay since time
	lastEventTime, err := db.GetLastEventTime(ctx)
	if err != nil {
		log.Printf("Warning: failed to get last event time: %v", err)
	}

	// Choose rollback based on whether we have previous events
	rollback := ingest.DefaultReplayRollback
	if lastEventTime.IsZero() {
		rollback = ingest.DefaultFirstRunRollback
	}
	replaySince := ingest.CalculateReplaySince(lastEventTime, rollback)

	if lastEventTime.IsZero() {
		log.Printf("No previous events, replaying last %v", rollback)
	} else {
		log.Printf("Replaying events since: %s", replaySince.Format(time.RFC3339))
	}

	// 8. Create derive state, SSE hub, and notifier
	deriveState := derive.New()

	// Create SSE hub and start its run loop
	hub := api.NewHub()
	go hub.Run()

	var notifier *notify.Notifier
	if !secrets.DiscordWebhookURL.IsEmpty() {
		sender := notify.NewDiscordSender(secrets.DiscordWebhookURL)
		notifier = notify.NewNotifier(sender, cfg.DiscordBatchSec, notify.FilterConfig{
			NotifyOnJoin:      cfg.NotifyOnJoin,
			NotifyOnLeave:     cfg.NotifyOnLeave,
			NotifyOnWorldJoin: cfg.NotifyOnWorldJoin,
		})
		go notifier.Run(ctx)
		log.Println("Discord notifications enabled")
	} else {
		log.Println("Discord webhook not configured, notifications disabled")
	}

	// 9. Create event source (use config.LogPath if set)
	var sourceOpts []ingest.SourceOption
	if cfg.LogPath != "" {
		sourceOpts = append(sourceOpts, ingest.WithLogDir(cfg.LogPath))
	}
	source := ingest.NewVRClogSource(replaySince, sourceOpts...)

	// Create ingester with OnInsert callback for derive, notify, and SSE
	ingester := ingest.New(source, db,
		ingest.WithOnInsert(func(ctx context.Context, e *event.Event) {
			derived := deriveState.Update(e)
			if derived != nil && notifier != nil {
				notifier.Enqueue(derived)
			}
			// Broadcast to SSE subscribers
			hub.Publish(e)
		}),
	)

	// 10. Start ingestion in background goroutine
	go func() {
		if err := ingester.Run(ctx); err != nil {
			log.Printf("Ingester error: %v", err)
		}
	}()

	// 11. Determine bind address
	host := "127.0.0.1"
	if cfg.LanEnabled {
		host = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", host, *port)

	// Build dependencies
	health := app.HealthService{
		Version:           version.String(),
		DB:                db,
		DiscordConfigured: !secrets.DiscordWebhookURL.IsEmpty(),
	}
	eventsService := &app.EventsService{Store: db}
	stateService := app.StateService{State: deriveState}
	statsService := app.NewStatsService(db)

	// Get config paths for ConfigService
	configPath, _ := config.ConfigPath()
	secretsPath, _ := config.SecretsPath()
	configService := app.ConfigService{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
	}

	// Build server options
	serverOpts := []api.ServerOption{
		api.WithEventsUsecase(eventsService),
		api.WithStateUsecase(stateService),
		api.WithStatsUsecase(statsService),
		api.WithConfigUsecase(configService),
		api.WithHub(hub),
		api.WithSSESecret([]byte(secrets.SSEHMACSecret.Value())),
	}

	// Add embedded web UI if available
	if webFS, err := webembed.GetFS(); err == nil && webFS != nil {
		serverOpts = append(serverOpts, api.WithWebFS(webFS))
		log.Println("Web UI enabled")
	}

	// Enable Basic Auth, Rate Limiting, Auth Failure Limiting, and CSRF protection for LAN mode
	var rateLimiter *api.RateLimiter
	var authFailureLimiter *api.AuthFailureLimiter
	if cfg.LanEnabled {
		serverOpts = append(serverOpts, api.WithBasicAuth(secrets.BasicAuthUsername, secrets.BasicAuthPassword.Value()))
		log.Println("Basic Auth enabled for LAN mode")

		// Enable rate limiting for LAN mode
		rateLimiter = api.NewRateLimiter(api.DefaultRateLimiterConfig())
		serverOpts = append(serverOpts, api.WithRateLimiter(rateLimiter))
		log.Println("Rate limiting enabled for LAN mode")

		// Enable auth failure limiting for brute-force protection
		authFailureLimiter = api.NewAuthFailureLimiter(api.DefaultAuthFailureLimiterConfig())
		serverOpts = append(serverOpts, api.WithAuthFailureLimiter(authFailureLimiter))
		log.Println("Auth failure limiting enabled for LAN mode")

		// Enable CSRF protection for LAN mode
		// Allow requests from the server's own address
		csrfAllowedHosts := []string{addr}
		serverOpts = append(serverOpts, api.WithCSRFAllowedHosts(csrfAllowedHosts))
		log.Println("CSRF protection enabled for LAN mode")
	}

	server := api.NewServer(addr, health, serverOpts...)

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

	// Cancel ingester context first (this also stops notifier via context)
	cancel()

	// Stop notifier gracefully (best-effort flush)
	if notifier != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := notifier.Stop(stopCtx); err != nil {
			log.Printf("Notifier stop error: %v", err)
		}
		stopCancel()
	}

	// Stop SSE hub (closes all subscriber channels)
	hub.Stop()

	// Stop rate limiter cleanup goroutine
	if rateLimiter != nil {
		rateLimiter.Stop()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
