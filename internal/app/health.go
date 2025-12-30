// Package app provides application use cases.
package app

import "context"

// HealthUsecase defines the health check use case.
type HealthUsecase interface {
	Handle(ctx context.Context) (HealthResult, error)
}

// HealthResult represents the health check response.
type HealthResult struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// HealthService implements HealthUsecase.
type HealthService struct {
	Version string
}

// Handle returns the current health status.
func (s HealthService) Handle(ctx context.Context) (HealthResult, error) {
	return HealthResult{
		Status:  "ok",
		Version: s.Version,
	}, nil
}
