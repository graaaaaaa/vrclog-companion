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

	"github.com/vrclog/vrclog-companion/internal/adapter"
	"github.com/vrclog/vrclog-companion/internal/api"
	"github.com/vrclog/vrclog-companion/internal/app"
	"github.com/vrclog/vrclog-companion/internal/appinfo"
	"github.com/vrclog/vrclog-companion/internal/config"
	"github.com/vrclog/vrclog-companion/internal/ingest"
	"github.com/vrclog/vrclog-companion/internal/notify"
	"github.com/vrclog/vrclog-companion/internal/observation"
	"github.com/vrclog/vrclog-companion/internal/projector"
	"github.com/vrclog/vrclog-companion/internal/singleinstance"
	"github.com/vrclog/vrclog-companion/internal/sse"
	"github.com/vrclog/vrclog-companion/internal/store"
	"github.com/vrclog/vrclog-companion/internal/version"
	"github.com/vrclog/vrclog-companion/webembed"
)

func main() {
	// 1. Single instance check, config/secrets, security setup.
	release, ok, err := singleinstance.AcquireLock()
	if err != nil {
		log.Fatalf("Failed to acquire lock: %v", err)
	}
	if !ok {
		log.Println("Another instance is already running")
		os.Exit(1)
	}
	defer release()

	cfg, _ := config.LoadConfig()
	cfg = config.ApplyEnvOverrides(cfg)
	secrets, secretsStatus, err := config.LoadSecrets()
	if err != nil {
		log.Printf("Warning: %v", err)
	}

	updated, generatedPw, err := config.EnsureLanAuth(&secrets, cfg.LanEnabled)
	if err != nil {
		log.Fatalf("Failed to ensure LAN auth: %v", err)
	}
	sseUpdated, err := config.EnsureSSESecret(&secrets)
	if err != nil {
		log.Fatalf("Failed to ensure SSE secret: %v", err)
	}
	updated = updated || sseUpdated

	if updated && secretsStatus != config.SecretsFallback {
		if err := config.SaveSecrets(secrets); err != nil {
			log.Fatalf("Failed to save secrets: %v", err)
		}
		if generatedPw != "" {
			pwPath, err := config.WritePasswordFile(secrets.BasicAuthUsername, generatedPw)
			if err != nil {
				log.Printf("Warning: failed to write password file: %v", err)
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

	port := flag.Int("port", cfg.Port, "HTTP server port")
	flag.Parse()

	// 2. Store open + schema validation.
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

	if vacuumed, err := db.VacuumIfNeeded(context.Background()); err != nil {
		log.Printf("Warning: VACUUM check failed: %v", err)
	} else if vacuumed {
		log.Println("Database maintenance completed")
	}

	// 3. Adapter/Engine construction.
	engine, loadedAdapters, err := adapter.BuildEngine()
	if err != nil {
		log.Fatalf("Failed to build adapter engine: %v", err)
	}
	log.Printf("Loaded %d adapters", len(loadedAdapters))

	// 4. Projector Manager.
	manager := projector.NewManager()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	broadcaster := sse.NewBroadcaster()

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

	onInsert := func(_ context.Context, obs observation.StoredObservation) {
		changes, err := manager.Apply(obs)
		if err != nil {
			log.Printf("Warning: projector apply failed for observation %s: %v", obs.ID, err)
			return
		}
		broadcaster.Broadcast(obs)
		if notifier != nil {
			for _, c := range changes {
				notifier.Enqueue(c)
			}
		}
	}

	var sourceOpts ingest.VRChatSourceConfig
	if cfg.LogPath != "" {
		sourceOpts.LogDir = cfg.LogPath
	}
	sourceFactory := ingest.NewVRChatSourceFactory(sourceOpts)
	runner := ingest.NewRunner(sourceFactory, engine, db, ingest.WithOnInsert(onInsert))

	// Build dependencies for the API server. Store satisfies
	// app.ObservationStore, app.HealthChecker, app.StatsStore, and
	// api.SSEObservationStore directly.
	health := app.HealthService{
		DB:             db,
		Ingest:         runner,
		LoadedAdapters: len(loadedAdapters),
	}
	observationsService := &app.ObservationsService{Store: db}
	stateService := app.StateService{Manager: manager}
	mediaService := app.MediaService{Manager: manager}
	adaptersService := app.AdaptersService{Loaded: loadedAdapters}
	statsService := app.NewStatsService(db, manager)

	configPath, _ := config.ConfigPath()
	secretsPath, _ := config.SecretsPath()
	configService := app.ConfigService{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
	}

	serverOpts := []api.ServerOption{
		api.WithObservationsUsecase(observationsService),
		api.WithStateUsecase(stateService),
		api.WithMediaUsecase(mediaService),
		api.WithAdaptersUsecase(adaptersService),
		api.WithStatsUsecase(statsService),
		api.WithConfigUsecase(configService),
		api.WithBroadcaster(broadcaster, db),
		api.WithSSESecret([]byte(secrets.SSEHMACSecret.Value())),
	}

	if webFS, err := webembed.GetFS(); err == nil && webFS != nil {
		serverOpts = append(serverOpts, api.WithWebFS(webFS))
		log.Println("Web UI enabled")
	}

	host := "127.0.0.1"
	if cfg.LanEnabled {
		host = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", host, *port)

	var rateLimiter *api.RateLimiter
	var authFailureLimiter *api.AuthFailureLimiter
	if cfg.LanEnabled {
		serverOpts = append(serverOpts, api.WithBasicAuth(secrets.BasicAuthUsername, secrets.BasicAuthPassword.Value()))
		log.Println("Basic Auth enabled for LAN mode")

		rateLimiter = api.NewRateLimiter(api.DefaultRateLimiterConfig())
		serverOpts = append(serverOpts, api.WithRateLimiter(rateLimiter))
		log.Println("Rate limiting enabled for LAN mode")

		authFailureLimiter = api.NewAuthFailureLimiter(api.DefaultAuthFailureLimiterConfig())
		serverOpts = append(serverOpts, api.WithAuthFailureLimiter(authFailureLimiter))
		log.Println("Auth failure limiting enabled for LAN mode")

		csrfAllowedHosts := []string{addr}
		serverOpts = append(serverOpts, api.WithCSRFAllowedHosts(csrfAllowedHosts))
		log.Println("CSRF protection enabled for LAN mode")
	}

	server := api.NewServer(addr, health, serverOpts...)

	// 5. Start the HTTP server before the Projector rebuild completes.
	// /api/v1/health is always served; every other route 503s until
	// SetReady(true) below.
	server.SetReady(false)

	errCh := make(chan error, 1)
	go func() {
		log.Printf("Starting VRClog Companion v%s on %s", version.String(), addr)
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// 6. Startup Projector rebuild: replay all persisted Observations in
	// sequence order. No SSE, no Discord notifications during this pass.
	if err := manager.Rebuild(ctx, db.AllObservations(ctx)); err != nil {
		log.Fatalf("Projector rebuild failed: %v", err)
	}
	log.Println("Projector state rebuilt from database")

	// 7. Full HTTP/Web UI now serving.
	server.SetReady(true)

	// 8/9. Ingest supervisor: internally resolves the latest persisted
	// cursor and starts consuming Records.
	go func() {
		if err := runner.Run(ctx); err != nil {
			log.Printf("Ingest runner error: %v", err)
		}
	}()

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	select {
	case <-done:
		log.Println("Shutting down...")
	case err := <-errCh:
		log.Printf("Server error: %v", err)
		os.Exit(1)
	}

	// Shutdown order: ingest -> pending DB transaction (implicit: Runner's
	// in-flight CommitRecord finishes before Run returns) -> notifier ->
	// SSE -> HTTP -> DB.
	cancel()

	if notifier != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := notifier.Stop(stopCtx); err != nil {
			log.Printf("Notifier stop error: %v", err)
		}
		stopCancel()
	}

	broadcaster.Stop()

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
