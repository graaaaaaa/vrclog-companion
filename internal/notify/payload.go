package notify

import (
	"fmt"
	"strings"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/derive"
)

// Discord embed color constants.
const (
	ColorGreen = 0x00FF00 // Player joined
	ColorRed   = 0xFF0000 // Player left
	ColorBlue  = 0x5865F2 // World changed (Discord blurple)
)

// MaxEmbedsPerRequest is the Discord API limit for embeds per message.
const MaxEmbedsPerRequest = 10

// DiscordPayload represents a Discord webhook request body.
type DiscordPayload struct {
	Content string         `json:"content,omitempty"`
	Embeds  []DiscordEmbed `json:"embeds,omitempty"`
}

// DiscordEmbed represents a Discord embed.
type DiscordEmbed struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Color       int    `json:"color,omitempty"`
	Timestamp   string `json:"timestamp,omitempty"`
}

// BuildPayloads creates Discord payloads from batched derived events.
// May return multiple payloads if events exceed MaxEmbedsPerRequest.
func BuildPayloads(events []*derive.DerivedEvent) []DiscordPayload {
	if len(events) == 0 {
		return nil
	}

	// Group by type for cleaner messages
	var joins, leaves []*derive.DerivedEvent
	var worldChanges []*derive.DerivedEvent

	for _, e := range events {
		switch e.Type {
		case derive.DerivedPlayerJoined:
			joins = append(joins, e)
		case derive.DerivedPlayerLeft:
			leaves = append(leaves, e)
		case derive.DerivedWorldChanged:
			worldChanges = append(worldChanges, e)
		}
	}

	var embeds []DiscordEmbed

	// World change embeds (usually one, but handle multiples)
	for _, wc := range worldChanges {
		embeds = append(embeds, buildWorldEmbed(wc))
	}

	// Batch joins into single embed
	if len(joins) > 0 {
		embeds = append(embeds, buildJoinsEmbed(joins))
	}

	// Batch leaves into single embed
	if len(leaves) > 0 {
		embeds = append(embeds, buildLeavesEmbed(leaves))
	}

	// Split into multiple payloads if needed
	return splitIntoPayloads(embeds)
}

func buildWorldEmbed(e *derive.DerivedEvent) DiscordEmbed {
	worldName := deref(e.Event.WorldName)
	if worldName == "" {
		worldName = "Unknown World"
	}

	desc := fmt.Sprintf("Joined **%s**", worldName)

	// Add instance info if available
	if instanceID := deref(e.Event.InstanceID); instanceID != "" {
		desc += fmt.Sprintf("\nInstance: `%s`", instanceID)
	}

	return DiscordEmbed{
		Title:       "World Changed",
		Description: desc,
		Color:       ColorBlue,
		Timestamp:   e.Event.Ts.Format(time.RFC3339),
	}
}

func buildJoinsEmbed(events []*derive.DerivedEvent) DiscordEmbed {
	names := make([]string, len(events))
	for i, e := range events {
		names[i] = deref(e.Event.PlayerName)
	}

	var desc string
	if len(events) == 1 {
		desc = fmt.Sprintf("**%s** joined", names[0])
	} else {
		desc = fmt.Sprintf("**%d players** joined: %s", len(events), strings.Join(names, ", "))
	}

	return DiscordEmbed{
		Title:       "Player Joined",
		Description: desc,
		Color:       ColorGreen,
		Timestamp:   events[len(events)-1].Event.Ts.Format(time.RFC3339),
	}
}

func buildLeavesEmbed(events []*derive.DerivedEvent) DiscordEmbed {
	names := make([]string, len(events))
	for i, e := range events {
		names[i] = deref(e.Event.PlayerName)
	}

	var desc string
	if len(events) == 1 {
		desc = fmt.Sprintf("**%s** left", names[0])
	} else {
		desc = fmt.Sprintf("**%d players** left: %s", len(events), strings.Join(names, ", "))
	}

	return DiscordEmbed{
		Title:       "Player Left",
		Description: desc,
		Color:       ColorRed,
		Timestamp:   events[len(events)-1].Event.Ts.Format(time.RFC3339),
	}
}

func splitIntoPayloads(embeds []DiscordEmbed) []DiscordPayload {
	if len(embeds) == 0 {
		return nil
	}

	var payloads []DiscordPayload
	for i := 0; i < len(embeds); i += MaxEmbedsPerRequest {
		end := i + MaxEmbedsPerRequest
		if end > len(embeds) {
			end = len(embeds)
		}
		payloads = append(payloads, DiscordPayload{Embeds: embeds[i:end]})
	}
	return payloads
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
