package app

import (
	"context"

	"github.com/vrclog/vrclog-companion/internal/adapter"
)

// AdaptersUsecase defines the loaded-adapters listing use case.
type AdaptersUsecase interface {
	List(ctx context.Context) []adapter.LoadedAdapter
}

// AdaptersService implements AdaptersUsecase from a fixed, compile-time
// composed adapter list.
type AdaptersService struct {
	Loaded []adapter.LoadedAdapter
}

// List returns the loaded adapters in composition order.
func (s AdaptersService) List(ctx context.Context) []adapter.LoadedAdapter {
	return s.Loaded
}
