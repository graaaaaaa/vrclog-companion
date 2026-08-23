package app

import (
	"context"
	"testing"

	"github.com/vrclog/vrclog-companion/internal/ingest"
)

// fakeIngestStatusProvider reports a fixed Status for HealthService tests.
type fakeIngestStatusProvider struct {
	status ingest.Status
}

func (f fakeIngestStatusProvider) Status() ingest.Status { return f.status }

// TestHealthService_StateFailedReportsDegraded verifies that
// ingest.StateFailed — the terminal state after a fatal integrity
// violation or projection failure — is surfaced as HealthResult.Ingest =
// "failed" and HealthResult.Status = StatusDegraded, exactly like any
// other non-running ingest state. There is no separate "failed" health
// status; StateFailed is degraded, not a distinct health tier.
func TestHealthService_StateFailedReportsDegraded(t *testing.T) {
	svc := HealthService{
		Ingest: fakeIngestStatusProvider{status: ingest.Status{
			State:     ingest.StateFailed,
			LastError: "integrity violation",
		}},
		LoadedAdapters: 1,
	}

	result, err := svc.Handle(context.Background())
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if result.Ingest != string(ingest.StateFailed) {
		t.Errorf("Ingest = %q, want %q", result.Ingest, ingest.StateFailed)
	}
	if result.Status != StatusDegraded {
		t.Errorf("Status = %q, want %q", result.Status, StatusDegraded)
	}
	if result.LastIngestError != "integrity violation" {
		t.Errorf("LastIngestError = %q, want %q", result.LastIngestError, "integrity violation")
	}
}

// TestHealthService_StateRunningReportsOK is the counterpart baseline: a
// running ingest state must report StatusOK, confirming StateFailed's
// degraded mapping is not simply always-degraded regardless of state.
func TestHealthService_StateRunningReportsOK(t *testing.T) {
	svc := HealthService{
		Ingest:         fakeIngestStatusProvider{status: ingest.Status{State: ingest.StateRunning}},
		LoadedAdapters: 1,
	}

	result, err := svc.Handle(context.Background())
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if result.Status != StatusOK {
		t.Errorf("Status = %q, want %q", result.Status, StatusOK)
	}
}
