package notify

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/vrclog/vrclog-companion/internal/projector"
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
	// AllowedMentions is always sent with an empty Parse list, disabling
	// every @mention/role/everyone ping regardless of what text ends up in
	// an embed. This is the primary defense; sanitizeDiscordText below is
	// defense-in-depth for clients that render mentions from plain text.
	AllowedMentions DiscordAllowedMentions `json:"allowed_mentions"`
}

// DiscordAllowedMentions disables all mention parsing.
type DiscordAllowedMentions struct {
	Parse []string `json:"parse"`
}

// DiscordEmbed represents a Discord embed.
type DiscordEmbed struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Color       int    `json:"color,omitempty"`
	Timestamp   string `json:"timestamp,omitempty"`
}

// BuildPayloads creates Discord payloads from batched Changes. Only
// WorldChanged/PlayerJoined/PlayerLeft produce notifications — media URLs
// are never sent to Discord (spec: no media notifications).
func BuildPayloads(changes []projector.Change) []DiscordPayload {
	if len(changes) == 0 {
		return nil
	}

	var joins []projector.PlayerJoined
	var leaves []projector.PlayerLeft
	var worldChanges []projector.WorldChanged

	for _, c := range changes {
		switch v := c.(type) {
		case projector.PlayerJoined:
			joins = append(joins, v)
		case projector.PlayerLeft:
			leaves = append(leaves, v)
		case projector.WorldChanged:
			worldChanges = append(worldChanges, v)
		}
	}

	var embeds []DiscordEmbed
	for _, wc := range worldChanges {
		embeds = append(embeds, buildWorldEmbed(wc))
	}
	if len(joins) > 0 {
		embeds = append(embeds, buildJoinsEmbed(joins))
	}
	if len(leaves) > 0 {
		embeds = append(embeds, buildLeavesEmbed(leaves))
	}

	return splitIntoPayloads(embeds)
}

func buildWorldEmbed(wc projector.WorldChanged) DiscordEmbed {
	name := sanitizeDiscordText(wc.Current.Name)
	if name == "" {
		name = "Unknown World"
	}

	desc := fmt.Sprintf("Joined **%s**", name)
	if wc.Current.InstanceID != "" {
		desc += fmt.Sprintf("\nInstance: `%s`", sanitizeDiscordText(wc.Current.InstanceID))
	}

	return DiscordEmbed{
		Title:       "World Changed",
		Description: desc,
		Color:       ColorBlue,
		Timestamp:   wc.At.Format(time.RFC3339),
	}
}

func buildJoinsEmbed(joins []projector.PlayerJoined) DiscordEmbed {
	names := make([]string, len(joins))
	for i, j := range joins {
		names[i] = sanitizeDiscordText(j.Player.DisplayName)
	}

	var desc string
	if len(joins) == 1 {
		desc = fmt.Sprintf("**%s** joined", names[0])
	} else {
		desc = fmt.Sprintf("**%d players** joined: %s", len(joins), strings.Join(names, ", "))
	}

	return DiscordEmbed{
		Title:       "Player Joined",
		Description: desc,
		Color:       ColorGreen,
		Timestamp:   joins[len(joins)-1].At.Format(time.RFC3339),
	}
}

func buildLeavesEmbed(leaves []projector.PlayerLeft) DiscordEmbed {
	names := make([]string, len(leaves))
	for i, l := range leaves {
		names[i] = sanitizeDiscordText(l.Player.DisplayName)
	}

	var desc string
	if len(leaves) == 1 {
		desc = fmt.Sprintf("**%s** left", names[0])
	} else {
		desc = fmt.Sprintf("**%d players** left: %s", len(leaves), strings.Join(names, ", "))
	}

	return DiscordEmbed{
		Title:       "Player Left",
		Description: desc,
		Color:       ColorRed,
		Timestamp:   leaves[len(leaves)-1].At.Format(time.RFC3339),
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
		payloads = append(payloads, DiscordPayload{
			Embeds:          embeds[i:end],
			AllowedMentions: DiscordAllowedMentions{Parse: []string{}},
		})
	}
	return payloads
}

var discordMarkdownEscaper = strings.NewReplacer(
	"\\", "\\\\",
	"*", "\\*",
	"_", "\\_",
	"~", "\\~",
	"`", "\\`",
	"|", "\\|",
	">", "\\>",
	"[", "\\[",
	"]", "\\]",
)

// zeroWidthSpace breaks a character sequence Discord's client-side parser
// looks for (a mention trigger, a URL scheme separator) while leaving the
// text visually unchanged for a human reader.
const zeroWidthSpace = "\u200b"

var mentionNeutralizer = strings.NewReplacer(
	"@everyone", "@"+zeroWidthSpace+"everyone",
	"@here", "@"+zeroWidthSpace+"here",
	"<@", "<"+zeroWidthSpace+"@",
)

var urlSchemePattern = regexp.MustCompile(`(?i)(https?):/{2}`)

// sanitizeDiscordText prepares untrusted text (VRChat player/world names)
// for inclusion in a Discord embed: Markdown control characters are
// escaped, known mention trigger sequences (@everyone, @here, <@id>) are
// broken with a zero-width space, and http(s):// URL text is broken the
// same way so Discord cannot auto-link it. The AllowedMentions field on
// the payload is the primary mention defense; this is defense-in-depth.
func sanitizeDiscordText(s string) string {
	s = discordMarkdownEscaper.Replace(s)
	s = mentionNeutralizer.Replace(s)
	s = urlSchemePattern.ReplaceAllString(s, "$1:"+zeroWidthSpace+"//")
	return s
}
