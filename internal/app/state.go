package app

import (
	"context"

	"github.com/graaaaa/vrclog-companion/internal/derive"
)

// StateUsecase defines the current state use case.
type StateUsecase interface {
	// GetCurrentState returns the current world and players.
	GetCurrentState(ctx context.Context) StateResult
}

// StateResult represents the current state response.
type StateResult struct {
	World   *derive.WorldInfo   `json:"world"`
	Players []derive.PlayerInfo `json:"players"`
}

// StateService implements StateUsecase by wrapping derive.State.
type StateService struct {
	State *derive.State
}

// GetCurrentState returns the current world and player list.
func (s StateService) GetCurrentState(ctx context.Context) StateResult {
	return StateResult{
		World:   s.State.CurrentWorld(),
		Players: s.State.CurrentPlayers(),
	}
}
