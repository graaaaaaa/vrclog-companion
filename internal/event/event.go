// Package event provides the shared Event model for VRClog Companion.
// This package is used by api, derive, notify, and store packages.
package event

import (
	"encoding/json"
	"time"
)

// Event type constants.
const (
	TypePlayerJoin = "player_join"
	TypePlayerLeft = "player_left"
	TypeWorldJoin  = "world_join"
)

// Event represents a VRChat log event.
// This is the domain model shared across packages, independent of storage implementation.
type Event struct {
	ID            int64           `json:"id"`
	Ts            time.Time       `json:"ts"`
	Type          string          `json:"type"`
	PlayerName    *string         `json:"player_name,omitempty"`
	PlayerID      *string         `json:"player_id,omitempty"`
	WorldID       *string         `json:"world_id,omitempty"`
	WorldName     *string         `json:"world_name,omitempty"`
	InstanceID    *string         `json:"instance_id,omitempty"`
	MetaJSON      json.RawMessage `json:"meta,omitempty"`
	DedupeKey     string          `json:"-"`
	IngestedAt    time.Time       `json:"ingested_at"`
	SchemaVersion int             `json:"-"`
}

// StringPtr returns a pointer to the given string.
// Useful for setting optional fields.
func StringPtr(s string) *string {
	return &s
}
