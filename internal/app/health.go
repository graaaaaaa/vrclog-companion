// Package app provides application use cases: thin interfaces the api
// package depends on, implemented by services that wire concrete store/
// projector/ingest state together.
package app

import (
	"context"
	"time"

	"github.com/vrclog/vrclog-companion/internal/ingest"
)

// HealthUsecase defines the health check use case.
type HealthUsecase interface {
	Handle(ctx context.Context) (HealthResult, error)
}

// HealthChecker defines the interface for checking component health.
type HealthChecker interface {
	Ping(ctx context.Context) error
}

// IngestStatusProvider reports current ingest health.
type IngestStatusProvider interface {
	Status() ingest.Status
}

// HealthResult represents the health check response. It deliberately
// carries no paths, URLs, or secrets — /api/v1/health is unauthenticated.
type HealthResult struct {
	Status          string `json:"status"`
	Database        string `json:"database"`
	Ingest          string `json:"ingest"`
	LastIngestError string `json:"last_ingest_error"`
	LastRecordAt    string `json:"last_record_at"`
	LoadedAdapters  int    `json:"loaded_adapters"`
}

// Health status constants.
const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
)

// HealthService implements HealthUsecase.
type HealthService struct {
	DB             HealthChecker
	Ingest         IngestStatusProvider
	LoadedAdapters int
}

// Handle returns the current health status.
func (s HealthService) Handle(ctx context.Context) (HealthResult, error) {
	result := HealthResult{
		Status:         StatusOK,
		Database:       StatusOK,
		LoadedAdapters: s.LoadedAdapters,
	}

	if s.DB != nil {
		if err := s.DB.Ping(ctx); err != nil {
			result.Database = "error"
			result.Status = StatusDegraded
		}
	}

	if s.Ingest != nil {
		st := s.Ingest.Status()
		result.Ingest = string(st.State)
		result.LastIngestError = st.LastError
		if !st.LastRecordAt.IsZero() {
			result.LastRecordAt = st.LastRecordAt.UTC().Format(time.RFC3339)
		}
		if st.State != ingest.StateRunning {
			result.Status = StatusDegraded
		}
	}

	return result, nil
}
