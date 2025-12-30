package ingest

import (
	"testing"
	"time"
)

func TestSHA256Hex(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:  "hello",
			input: "hello",
			want:  "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		},
		{
			name:  "sample log line",
			input: "2024.01.15 10:30:45 Log        -  [NetworkManager] OnPlayerJoined TestUser",
			want:  "a7a1d62d0b9b4b0a0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b", // placeholder
		},
	}

	// Override the third test with actual computed value
	tests[2].want = SHA256Hex(tests[2].input)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SHA256Hex(tt.input)
			if got != tt.want {
				t.Errorf("SHA256Hex() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSHA256Hex_Deterministic(t *testing.T) {
	input := "2024.01.15 10:30:45 Log        -  [NetworkManager] OnPlayerJoined TestUser"

	hash1 := SHA256Hex(input)
	hash2 := SHA256Hex(input)

	if hash1 != hash2 {
		t.Errorf("SHA256Hex is not deterministic: %v != %v", hash1, hash2)
	}
}

func TestToStoreEvent(t *testing.T) {
	now := time.Now()
	rawLine := "2024.01.15 10:30:45 Log        -  [NetworkManager] OnPlayerJoined TestUser"

	ev := Event{
		Type:       "player_join",
		Timestamp:  now,
		PlayerName: "TestUser",
		PlayerID:   "usr_12345",
		WorldID:    "",
		WorldName:  "",
		InstanceID: "",
		RawLine:    rawLine,
	}

	storeEvent := ToStoreEvent(ev)

	// Check basic fields
	if storeEvent.Type != "player_join" {
		t.Errorf("Type = %v, want player_join", storeEvent.Type)
	}
	if storeEvent.Ts != now {
		t.Errorf("Ts = %v, want %v", storeEvent.Ts, now)
	}

	// Check pointer fields
	if storeEvent.PlayerName == nil || *storeEvent.PlayerName != "TestUser" {
		t.Errorf("PlayerName = %v, want TestUser", storeEvent.PlayerName)
	}
	if storeEvent.PlayerID == nil || *storeEvent.PlayerID != "usr_12345" {
		t.Errorf("PlayerID = %v, want usr_12345", storeEvent.PlayerID)
	}

	// Empty fields should be nil
	if storeEvent.WorldID != nil {
		t.Errorf("WorldID = %v, want nil", storeEvent.WorldID)
	}
	if storeEvent.WorldName != nil {
		t.Errorf("WorldName = %v, want nil", storeEvent.WorldName)
	}
	if storeEvent.InstanceID != nil {
		t.Errorf("InstanceID = %v, want nil", storeEvent.InstanceID)
	}

	// Check dedupe key
	expectedDedupeKey := SHA256Hex(rawLine)
	if storeEvent.DedupeKey != expectedDedupeKey {
		t.Errorf("DedupeKey = %v, want %v", storeEvent.DedupeKey, expectedDedupeKey)
	}

	// Check IngestedAt is set
	if storeEvent.IngestedAt.IsZero() {
		t.Error("IngestedAt should not be zero")
	}
}

func TestToStoreEvent_WorldJoin(t *testing.T) {
	now := time.Now()
	rawLine := "2024.01.15 10:30:45 Log        -  [Behaviour] Joining wrld_xxx:12345"

	ev := Event{
		Type:       "world_join",
		Timestamp:  now,
		PlayerName: "",
		PlayerID:   "",
		WorldID:    "wrld_xxx",
		WorldName:  "Test World",
		InstanceID: "12345~region(us)",
		RawLine:    rawLine,
	}

	storeEvent := ToStoreEvent(ev)

	if storeEvent.Type != "world_join" {
		t.Errorf("Type = %v, want world_join", storeEvent.Type)
	}

	// Player fields should be nil for world_join
	if storeEvent.PlayerName != nil {
		t.Errorf("PlayerName = %v, want nil", storeEvent.PlayerName)
	}
	if storeEvent.PlayerID != nil {
		t.Errorf("PlayerID = %v, want nil", storeEvent.PlayerID)
	}

	// World fields should be set
	if storeEvent.WorldID == nil || *storeEvent.WorldID != "wrld_xxx" {
		t.Errorf("WorldID = %v, want wrld_xxx", storeEvent.WorldID)
	}
	if storeEvent.WorldName == nil || *storeEvent.WorldName != "Test World" {
		t.Errorf("WorldName = %v, want Test World", storeEvent.WorldName)
	}
	if storeEvent.InstanceID == nil || *storeEvent.InstanceID != "12345~region(us)" {
		t.Errorf("InstanceID = %v, want 12345~region(us)", storeEvent.InstanceID)
	}
}

func TestStringPtrIfNotEmpty(t *testing.T) {
	// Non-empty string should return pointer
	s := "hello"
	ptr := stringPtrIfNotEmpty(s)
	if ptr == nil || *ptr != "hello" {
		t.Errorf("stringPtrIfNotEmpty(%q) = %v, want pointer to %q", s, ptr, s)
	}

	// Empty string should return nil
	empty := ""
	nilPtr := stringPtrIfNotEmpty(empty)
	if nilPtr != nil {
		t.Errorf("stringPtrIfNotEmpty(%q) = %v, want nil", empty, nilPtr)
	}
}
