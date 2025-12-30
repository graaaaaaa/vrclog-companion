package derive

import (
	"sync"
	"testing"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/event"
)

func ptr(s string) *string { return &s }

func TestState_WorldJoin_ClearsPlayers(t *testing.T) {
	s := New()

	// Add a player first
	s.Update(&event.Event{
		Type:       event.TypePlayerJoin,
		PlayerName: ptr("Alice"),
		Ts:         time.Now(),
	})

	if s.PlayerCount() != 1 {
		t.Fatalf("expected 1 player, got %d", s.PlayerCount())
	}

	// Join a new world
	derived := s.Update(&event.Event{
		Type:      event.TypeWorldJoin,
		WorldID:   ptr("wrld_123"),
		WorldName: ptr("Test World"),
		Ts:        time.Now(),
	})

	if derived == nil {
		t.Fatal("expected derived event, got nil")
	}
	if derived.Type != DerivedWorldChanged {
		t.Errorf("expected DerivedWorldChanged, got %d", derived.Type)
	}

	// Players should be cleared
	if s.PlayerCount() != 0 {
		t.Errorf("expected 0 players after world change, got %d", s.PlayerCount())
	}

	// World should be updated
	world := s.CurrentWorld()
	if world == nil {
		t.Fatal("expected world, got nil")
	}
	if world.WorldID != "wrld_123" {
		t.Errorf("expected world ID wrld_123, got %s", world.WorldID)
	}
}

func TestState_WorldJoin_PreservesPrevWorld(t *testing.T) {
	s := New()

	// First world
	s.Update(&event.Event{
		Type:      event.TypeWorldJoin,
		WorldID:   ptr("wrld_first"),
		WorldName: ptr("First World"),
		Ts:        time.Now(),
	})

	// Second world
	derived := s.Update(&event.Event{
		Type:      event.TypeWorldJoin,
		WorldID:   ptr("wrld_second"),
		WorldName: ptr("Second World"),
		Ts:        time.Now(),
	})

	if derived.PrevWorld == nil {
		t.Fatal("expected prev world, got nil")
	}
	if derived.PrevWorld.WorldID != "wrld_first" {
		t.Errorf("expected prev world ID wrld_first, got %s", derived.PrevWorld.WorldID)
	}
}

func TestState_PlayerJoin_Dedup(t *testing.T) {
	s := New()

	// First join
	derived1 := s.Update(&event.Event{
		Type:       event.TypePlayerJoin,
		PlayerName: ptr("Alice"),
		PlayerID:   ptr("usr_alice"),
		Ts:         time.Now(),
	})

	if derived1 == nil {
		t.Fatal("expected derived event for first join")
	}
	if derived1.Type != DerivedPlayerJoined {
		t.Errorf("expected DerivedPlayerJoined, got %d", derived1.Type)
	}

	// Duplicate join (same player name)
	derived2 := s.Update(&event.Event{
		Type:       event.TypePlayerJoin,
		PlayerName: ptr("Alice"),
		PlayerID:   ptr("usr_alice"),
		Ts:         time.Now(),
	})

	if derived2 != nil {
		t.Error("expected nil for duplicate join")
	}

	// Player count should still be 1
	if s.PlayerCount() != 1 {
		t.Errorf("expected 1 player, got %d", s.PlayerCount())
	}
}

func TestState_PlayerLeft_NotPresent(t *testing.T) {
	s := New()

	// Leave without join
	derived := s.Update(&event.Event{
		Type:       event.TypePlayerLeft,
		PlayerName: ptr("Ghost"),
		Ts:         time.Now(),
	})

	if derived != nil {
		t.Error("expected nil for player not present")
	}
}

func TestState_PlayerLeft_Present(t *testing.T) {
	s := New()

	// Join first
	s.Update(&event.Event{
		Type:       event.TypePlayerJoin,
		PlayerName: ptr("Alice"),
		Ts:         time.Now(),
	})

	if s.PlayerCount() != 1 {
		t.Fatalf("expected 1 player, got %d", s.PlayerCount())
	}

	// Leave
	derived := s.Update(&event.Event{
		Type:       event.TypePlayerLeft,
		PlayerName: ptr("Alice"),
		Ts:         time.Now(),
	})

	if derived == nil {
		t.Fatal("expected derived event for leave")
	}
	if derived.Type != DerivedPlayerLeft {
		t.Errorf("expected DerivedPlayerLeft, got %d", derived.Type)
	}

	if s.PlayerCount() != 0 {
		t.Errorf("expected 0 players after leave, got %d", s.PlayerCount())
	}
}

func TestState_EmptyPlayerName(t *testing.T) {
	s := New()

	// Join with empty name
	derived := s.Update(&event.Event{
		Type:       event.TypePlayerJoin,
		PlayerName: ptr(""),
		Ts:         time.Now(),
	})

	if derived != nil {
		t.Error("expected nil for empty player name")
	}

	// Join with nil name
	derived = s.Update(&event.Event{
		Type: event.TypePlayerJoin,
		Ts:   time.Now(),
	})

	if derived != nil {
		t.Error("expected nil for nil player name")
	}
}

func TestState_NilEvent(t *testing.T) {
	s := New()

	derived := s.Update(nil)
	if derived != nil {
		t.Error("expected nil for nil event")
	}
}

func TestState_UnknownEventType(t *testing.T) {
	s := New()

	derived := s.Update(&event.Event{
		Type: "unknown_type",
		Ts:   time.Now(),
	})

	if derived != nil {
		t.Error("expected nil for unknown event type")
	}
}

func TestState_CurrentPlayers_Copy(t *testing.T) {
	s := New()

	s.Update(&event.Event{
		Type:       event.TypePlayerJoin,
		PlayerName: ptr("Alice"),
		Ts:         time.Now(),
	})
	s.Update(&event.Event{
		Type:       event.TypePlayerJoin,
		PlayerName: ptr("Bob"),
		Ts:         time.Now(),
	})

	players := s.CurrentPlayers()
	if len(players) != 2 {
		t.Errorf("expected 2 players, got %d", len(players))
	}

	// Verify it's a copy (modifying returned slice doesn't affect state)
	players = append(players, PlayerInfo{PlayerName: "Charlie"})
	if s.PlayerCount() != 2 {
		t.Error("modifying returned slice affected state")
	}
}

func TestState_CurrentWorld_Copy(t *testing.T) {
	s := New()

	s.Update(&event.Event{
		Type:      event.TypeWorldJoin,
		WorldID:   ptr("wrld_123"),
		WorldName: ptr("Test World"),
		Ts:        time.Now(),
	})

	world := s.CurrentWorld()
	if world == nil {
		t.Fatal("expected world, got nil")
	}

	// Modify returned copy
	world.WorldName = "Modified"

	// Original should be unchanged
	world2 := s.CurrentWorld()
	if world2.WorldName != "Test World" {
		t.Error("modifying returned world affected state")
	}
}

func TestState_ThreadSafety(t *testing.T) {
	s := New()

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent world joins
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			s.Update(&event.Event{
				Type:      event.TypeWorldJoin,
				WorldID:   ptr("wrld_test"),
				WorldName: ptr("Test World"),
				Ts:        time.Now(),
			})
		}
	}()

	// Concurrent player joins
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			s.Update(&event.Event{
				Type:       event.TypePlayerJoin,
				PlayerName: ptr("Player"),
				Ts:         time.Now(),
			})
		}
	}()

	// Concurrent reads
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = s.CurrentWorld()
			_ = s.CurrentPlayers()
			_ = s.PlayerCount()
		}
	}()

	wg.Wait()
	// If we get here without panic, thread safety is working
}
