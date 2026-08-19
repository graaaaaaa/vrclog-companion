package app

import (
	"context"

	"github.com/vrclog/vrclog-companion/internal/observation"
	"github.com/vrclog/vrclog-companion/internal/store"
)

// ObservationsUsecase defines the generic Observation history query use case.
type ObservationsUsecase interface {
	List(ctx context.Context, q store.ObservationQuery) ([]observation.StoredObservation, *int64, error)
}

// ObservationStore is the store dependency needed by ObservationsService.
type ObservationStore interface {
	ListObservations(ctx context.Context, q store.ObservationQuery) ([]observation.StoredObservation, *int64, error)
}

// ObservationsService implements ObservationsUsecase.
type ObservationsService struct {
	Store ObservationStore
}

// List queries Observations with the given filter.
func (s *ObservationsService) List(ctx context.Context, q store.ObservationQuery) ([]observation.StoredObservation, *int64, error) {
	return s.Store.ListObservations(ctx, q)
}
