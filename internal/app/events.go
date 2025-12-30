package app

import (
	"context"

	"github.com/graaaaa/vrclog-companion/internal/store"
)

// EventsUsecase defines the events query use case.
type EventsUsecase interface {
	Query(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error)
}

// EventStore defines store operations needed by EventsService.
type EventStore interface {
	QueryEvents(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error)
}

// EventsService implements EventsUsecase.
type EventsService struct {
	Store EventStore
}

// Query queries events with the given filter.
func (s *EventsService) Query(ctx context.Context, filter store.QueryFilter) (store.QueryResult, error) {
	return s.Store.QueryEvents(ctx, filter)
}
