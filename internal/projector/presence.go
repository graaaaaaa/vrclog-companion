package projector

import (
	"sort"
	"strings"
	"time"

	vrclog "github.com/vrclog/vrclog-go"
)

// presenceProjector tracks players currently in the world. Keyed by
// Player.ID when present, falling back to a trimmed DisplayName so players
// without a stable ID are still deduplicated.
type presenceProjector struct {
	players map[string]*PlayerInfo
}

func newPresenceProjector() *presenceProjector {
	return &presenceProjector{players: make(map[string]*PlayerInfo)}
}

func playerKey(id, displayName string) string {
	if id != "" {
		return "id:" + id
	}
	return "name:" + strings.TrimSpace(displayName)
}

// applyJoined handles player.joined. A duplicate join (same key already
// present) is a no-op: no state change, no Change emitted.
func (p *presenceProjector) applyJoined(ev vrclog.PlayerJoined, occurredAt time.Time, observationID string) []Change {
	key := playerKey(ev.Player.ID, ev.Player.DisplayName)
	if _, exists := p.players[key]; exists {
		return nil
	}

	info := PlayerInfo{
		ID:            ev.Player.ID,
		DisplayName:   ev.Player.DisplayName,
		JoinedAt:      occurredAt,
		ObservationID: observationID,
	}
	p.players[key] = &info
	return []Change{PlayerJoined{Player: info, At: occurredAt}}
}

// applyLeft handles player.left. Leaving a player not currently tracked is
// a no-op — unknown-left is never notified.
func (p *presenceProjector) applyLeft(ev vrclog.PlayerLeft, occurredAt time.Time) []Change {
	key := playerKey(ev.Player.ID, ev.Player.DisplayName)
	info, exists := p.players[key]
	if !exists {
		return nil
	}
	delete(p.players, key)
	return []Change{PlayerLeft{Player: *info, At: occurredAt}}
}

// reset clears all tracked players. Used on a definitive world transition.
// Deliberately returns nothing: a world-transition reset is not a batch of
// individual PlayerLeft events and must not trigger leave notifications.
func (p *presenceProjector) reset() {
	p.players = make(map[string]*PlayerInfo)
}

func (p *presenceProjector) snapshot() []PlayerInfo {
	out := make([]PlayerInfo, 0, len(p.players))
	for _, info := range p.players {
		out = append(out, *info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].JoinedAt.Before(out[j].JoinedAt) })
	return out
}
