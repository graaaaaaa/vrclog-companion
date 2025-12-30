// Package derive provides in-memory state tracking derived from events.
// It tracks the current world and online players for notification purposes.
package derive

import (
	"sync"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/event"
)

// DerivedEventType indicates what changed after processing an event.
type DerivedEventType int

const (
	// DerivedWorldChanged indicates a world_join changed the current world.
	DerivedWorldChanged DerivedEventType = iota + 1
	// DerivedPlayerJoined indicates a new player joined the instance.
	DerivedPlayerJoined
	// DerivedPlayerLeft indicates a player left the instance.
	DerivedPlayerLeft
)

// DerivedEvent represents a state change for notification purposes.
type DerivedEvent struct {
	Type      DerivedEventType
	Event     *event.Event // Original event that triggered this
	PrevWorld *WorldInfo   // Previous world (only for WorldChanged)
}

// WorldInfo represents current world state.
type WorldInfo struct {
	WorldID    string
	WorldName  string
	InstanceID string
	JoinedAt   time.Time
}

// PlayerInfo represents a player currently in the instance.
type PlayerInfo struct {
	PlayerName string
	PlayerID   string
	JoinedAt   time.Time
}

// State tracks the current derived state from events.
// It is safe for concurrent use.
type State struct {
	mu           sync.RWMutex
	currentWorld *WorldInfo
	players      map[string]*PlayerInfo // keyed by PlayerID (or PlayerName if ID is empty)
}

// New creates a new State.
func New() *State {
	return &State{
		players: make(map[string]*PlayerInfo),
	}
}

// Update processes an event and returns a derived event indicating changes.
// Returns nil if no notification-worthy change occurred.
// Safe for concurrent use.
func (s *State) Update(e *event.Event) *DerivedEvent {
	if e == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	switch e.Type {
	case event.TypeWorldJoin:
		return s.handleWorldJoin(e)
	case event.TypePlayerJoin:
		return s.handlePlayerJoin(e)
	case event.TypePlayerLeft:
		return s.handlePlayerLeft(e)
	default:
		return nil
	}
}

func (s *State) handleWorldJoin(e *event.Event) *DerivedEvent {
	prev := s.currentWorld

	// Update current world
	s.currentWorld = &WorldInfo{
		WorldID:    deref(e.WorldID),
		WorldName:  deref(e.WorldName),
		InstanceID: deref(e.InstanceID),
		JoinedAt:   e.Ts,
	}

	// Clear player list on world change
	s.players = make(map[string]*PlayerInfo)

	return &DerivedEvent{
		Type:      DerivedWorldChanged,
		Event:     e,
		PrevWorld: prev,
	}
}

// playerKey returns the key for player lookup.
// Prefers PlayerID if available, falls back to PlayerName.
func (s *State) playerKey(e *event.Event) string {
	if id := deref(e.PlayerID); id != "" {
		return id
	}
	return deref(e.PlayerName)
}

func (s *State) handlePlayerJoin(e *event.Event) *DerivedEvent {
	key := s.playerKey(e)
	if key == "" {
		return nil
	}

	// Check if already present (duplicate)
	if _, exists := s.players[key]; exists {
		return nil
	}

	s.players[key] = &PlayerInfo{
		PlayerName: deref(e.PlayerName),
		PlayerID:   deref(e.PlayerID),
		JoinedAt:   e.Ts,
	}

	return &DerivedEvent{
		Type:  DerivedPlayerJoined,
		Event: e,
	}
}

func (s *State) handlePlayerLeft(e *event.Event) *DerivedEvent {
	key := s.playerKey(e)
	if key == "" {
		return nil
	}

	// Check if present
	if _, exists := s.players[key]; !exists {
		return nil
	}

	delete(s.players, key)

	return &DerivedEvent{
		Type:  DerivedPlayerLeft,
		Event: e,
	}
}

// CurrentWorld returns a copy of the current world info (nil if not in world).
// Safe for concurrent use.
func (s *State) CurrentWorld() *WorldInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.currentWorld == nil {
		return nil
	}
	cpy := *s.currentWorld
	return &cpy
}

// CurrentPlayers returns a copy of the current player list.
// Safe for concurrent use.
func (s *State) CurrentPlayers() []PlayerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]PlayerInfo, 0, len(s.players))
	for _, p := range s.players {
		result = append(result, *p)
	}
	return result
}

// PlayerCount returns the current player count.
// Safe for concurrent use.
func (s *State) PlayerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.players)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
