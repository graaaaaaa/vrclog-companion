package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/graaaaa/vrclog-companion/internal/config"
)

// ConfigUsecase defines the configuration management use case.
type ConfigUsecase interface {
	// GetConfig returns the current configuration.
	GetConfig(ctx context.Context) ConfigResponse

	// UpdateConfig updates the configuration with the given changes.
	// Returns the result indicating success and whether restart is required.
	UpdateConfig(ctx context.Context, req ConfigUpdateRequest) (ConfigUpdateResponse, error)
}

// ConfigResponse represents the current configuration (excludes secret values).
type ConfigResponse struct {
	Port                     int  `json:"port"`
	LanEnabled               bool `json:"lan_enabled"`
	DiscordBatchSec          int  `json:"discord_batch_sec"`
	NotifyOnJoin             bool `json:"notify_on_join"`
	NotifyOnLeave            bool `json:"notify_on_leave"`
	NotifyOnWorldJoin        bool `json:"notify_on_world_join"`
	DiscordWebhookConfigured bool `json:"discord_webhook_configured"`
}

// ConfigUpdateRequest contains optional fields for updating configuration.
type ConfigUpdateRequest struct {
	Port              *int    `json:"port,omitempty"`
	LanEnabled        *bool   `json:"lan_enabled,omitempty"`
	DiscordBatchSec   *int    `json:"discord_batch_sec,omitempty"`
	NotifyOnJoin      *bool   `json:"notify_on_join,omitempty"`
	NotifyOnLeave     *bool   `json:"notify_on_leave,omitempty"`
	NotifyOnWorldJoin *bool   `json:"notify_on_world_join,omitempty"`
	DiscordWebhookURL *string `json:"discord_webhook_url,omitempty"`
}

// ConfigUpdateResponse indicates the result of a configuration update.
type ConfigUpdateResponse struct {
	Success         bool `json:"success"`
	RestartRequired bool `json:"restart_required"`
	NewPort         int  `json:"new_port,omitempty"`
}

// ConfigService implements ConfigUsecase.
type ConfigService struct {
	ConfigPath  string
	SecretsPath string
}

// GetConfig returns the current configuration.
func (s ConfigService) GetConfig(ctx context.Context) ConfigResponse {
	cfg, _ := config.LoadConfigFrom(s.ConfigPath)
	sec, _, _ := config.LoadSecretsFrom(s.SecretsPath)

	return ConfigResponse{
		Port:                     cfg.Port,
		LanEnabled:               cfg.LanEnabled,
		DiscordBatchSec:          cfg.DiscordBatchSec,
		NotifyOnJoin:             cfg.NotifyOnJoin,
		NotifyOnLeave:            cfg.NotifyOnLeave,
		NotifyOnWorldJoin:        cfg.NotifyOnWorldJoin,
		DiscordWebhookConfigured: !sec.DiscordWebhookURL.IsEmpty(),
	}
}

// UpdateConfig updates the configuration.
func (s ConfigService) UpdateConfig(ctx context.Context, req ConfigUpdateRequest) (ConfigUpdateResponse, error) {
	// Load current config
	cfg, err := config.LoadConfigFrom(s.ConfigPath)
	if err != nil {
		return ConfigUpdateResponse{}, fmt.Errorf("load config: %w", err)
	}

	// Load current secrets
	sec, status, err := config.LoadSecretsFrom(s.SecretsPath)
	if err != nil && status == config.SecretsFallback {
		return ConfigUpdateResponse{}, fmt.Errorf("load secrets: %w", err)
	}

	originalPort := cfg.Port
	configChanged := false
	secretsChanged := false

	// Apply updates to config
	if req.Port != nil {
		if *req.Port < 1 || *req.Port > 65535 {
			return ConfigUpdateResponse{}, fmt.Errorf("port must be between 1 and 65535")
		}
		cfg.Port = *req.Port
		configChanged = true
	}
	if req.LanEnabled != nil {
		cfg.LanEnabled = *req.LanEnabled
		configChanged = true
	}
	if req.DiscordBatchSec != nil {
		if *req.DiscordBatchSec < 0 {
			return ConfigUpdateResponse{}, fmt.Errorf("discord_batch_sec must be non-negative")
		}
		cfg.DiscordBatchSec = *req.DiscordBatchSec
		configChanged = true
	}
	if req.NotifyOnJoin != nil {
		cfg.NotifyOnJoin = *req.NotifyOnJoin
		configChanged = true
	}
	if req.NotifyOnLeave != nil {
		cfg.NotifyOnLeave = *req.NotifyOnLeave
		configChanged = true
	}
	if req.NotifyOnWorldJoin != nil {
		cfg.NotifyOnWorldJoin = *req.NotifyOnWorldJoin
		configChanged = true
	}

	// Apply updates to secrets
	if req.DiscordWebhookURL != nil {
		url := *req.DiscordWebhookURL
		if url != "" && !isValidDiscordWebhookURL(url) {
			return ConfigUpdateResponse{}, fmt.Errorf("invalid Discord webhook URL")
		}
		sec.DiscordWebhookURL = config.Secret(url)
		secretsChanged = true
	}

	// Save config if changed
	if configChanged {
		if err := config.SaveConfigTo(cfg, s.ConfigPath); err != nil {
			return ConfigUpdateResponse{}, fmt.Errorf("save config: %w", err)
		}
	}

	// Save secrets if changed
	if secretsChanged {
		if err := config.SaveSecretsTo(sec, s.SecretsPath); err != nil {
			return ConfigUpdateResponse{}, fmt.Errorf("save secrets: %w", err)
		}
	}

	resp := ConfigUpdateResponse{
		Success:         true,
		RestartRequired: configChanged || secretsChanged, // MVP: always require restart
	}

	if cfg.Port != originalPort {
		resp.NewPort = cfg.Port
	}

	return resp, nil
}

// isValidDiscordWebhookURL validates Discord webhook URL format.
func isValidDiscordWebhookURL(url string) bool {
	return strings.HasPrefix(url, "https://discord.com/api/webhooks/") ||
		strings.HasPrefix(url, "https://discordapp.com/api/webhooks/")
}
