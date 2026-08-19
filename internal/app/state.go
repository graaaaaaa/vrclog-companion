package app

import (
	"context"

	"github.com/vrclog/vrclog-companion/internal/projector"
)

// StateUsecase defines the current projected state use case.
type StateUsecase interface {
	GetCurrentState(ctx context.Context) projector.Snapshot
}

// StateService implements StateUsecase by wrapping a projector.Manager.
type StateService struct {
	Manager *projector.Manager
}

// GetCurrentState returns the current world, players, and latest openable
// media.
func (s StateService) GetCurrentState(ctx context.Context) projector.Snapshot {
	return s.Manager.Snapshot()
}
