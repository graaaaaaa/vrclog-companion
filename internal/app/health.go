// Package app provides application use cases.
package app

import "context"

// HealthUsecase defines the health check use case.
type HealthUsecase interface {
	Handle(ctx context.Context) (HealthResult, error)
}

// HealthChecker defines the interface for checking component health.
type HealthChecker interface {
	// Ping checks if the component is healthy.
	// Returns nil if healthy, error otherwise.
	Ping(ctx context.Context) error
}

// HealthResult represents the health check response.
type HealthResult struct {
	Status     string                     `json:"status"`
	Version    string                     `json:"version"`
	Components map[string]ComponentHealth `json:"components,omitempty"`
}

// ComponentHealth represents the health status of a single component.
type ComponentHealth struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// Health status constants.
const (
	StatusHealthy   = "healthy"
	StatusDegraded  = "degraded"
	StatusUnhealthy = "unhealthy"
)

// HealthService implements HealthUsecase.
type HealthService struct {
	Version           string
	DB                HealthChecker
	DiscordConfigured bool
}

// Handle returns the current health status.
// Checks all registered components and returns overall status.
func (s HealthService) Handle(ctx context.Context) (HealthResult, error) {
	result := HealthResult{
		Status:     StatusHealthy,
		Version:    s.Version,
		Components: make(map[string]ComponentHealth),
	}

	// Check database if configured
	if s.DB != nil {
		if err := s.DB.Ping(ctx); err != nil {
			result.Components["database"] = ComponentHealth{
				Status:  StatusUnhealthy,
				Message: "database connection failed",
			}
			result.Status = StatusDegraded
		} else {
			result.Components["database"] = ComponentHealth{
				Status: StatusHealthy,
			}
		}
	}

	// Report Discord webhook configuration status
	if s.DiscordConfigured {
		result.Components["discord_webhook"] = ComponentHealth{
			Status: StatusHealthy,
		}
	} else {
		result.Components["discord_webhook"] = ComponentHealth{
			Status:  "unconfigured",
			Message: "Discord webhook not configured",
		}
	}

	return result, nil
}
