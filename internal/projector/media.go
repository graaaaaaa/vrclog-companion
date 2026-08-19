package projector

import (
	"time"

	vrclog "github.com/vrclog/vrclog-go"

	"github.com/vrclog/vrclog-companion/internal/observation"
)

// Media status values. The log gives no reliable signal that playback
// actually started, so there is no "playing" status — only whether a
// media resource/error was observed.
const (
	MediaStatusObserved = "observed"
	MediaStatusFailed   = "failed"
)

// mediaCorrelationWindow bounds how far apart (by OccurredAt) two
// Observations may be to still be considered part of the same media
// attempt when no exact target/URL match exists. The comparison is
// inclusive (<=) so the boundary itself is a valid match, and startup
// rebuild reproduces the exact same grouping as the live path.
const mediaCorrelationWindow = 10 * time.Second

// mediaMaxRecent bounds in-memory attempt history. DB Observations are
// never deleted; only this projected view is capped.
const mediaMaxRecent = 50

// MediaTargetDTO identifies which on-screen player/component a media
// resource or error belongs to, when the adapter reports one.
type MediaTargetDTO struct {
	Component string
	Key       string
	Backend   string
}

// MediaResourceObservation is one resource URL folded into a MediaAttempt.
type MediaResourceObservation struct {
	URL           string
	Kind          string
	Role          string
	AdapterID     string
	RuleID        string
	ObservationID string
	ObservedAt    time.Time
}

// MediaError is one playback/resolve error folded into a MediaAttempt.
type MediaError struct {
	Stage         string
	Code          string
	Message       string
	AdapterID     string
	ObservationID string
	ObservedAt    time.Time
}

// MediaAttempt groups the resource/error Observations that describe one
// attempt to play a piece of media, so a user can recover the original URL
// even when in-world playback failed silently.
type MediaAttempt struct {
	// ID is the Observation ID that first created this attempt —
	// deterministic across rebuilds, never a random UUID.
	ID              string
	FirstObservedAt time.Time
	LastObservedAt  time.Time
	Status          string
	// BestOpenableURL is chosen by role priority: source > resolver_input
	// > playback_input > none. role=resolved is intentionally excluded
	// (signed/temporary CDN URLs are unfit for external browser opening).
	BestOpenableURL string
	Resources       []MediaResourceObservation
	Errors          []MediaError
	ObservationIDs  []string
	AdapterIDs      []string
	Target          *MediaTargetDTO
	WorldInstanceID string
}

// LatestOpenableMedia is the most recent MediaAttempt (across all
// world sessions) that has a BestOpenableURL, included even if failed —
// URL recovery is the point of this feature.
type LatestOpenableMedia struct {
	AttemptID  string
	URL        string
	Status     string
	ObservedAt time.Time
}

// resourceRolePriority defines BestOpenableURL precedence. Roles absent
// from this map (resolved, thumbnail, metadata) never become Best.
var resourceRolePriority = map[vrclog.ResourceRole]int{
	vrclog.ResourceRoleSource:        1,
	vrclog.ResourceRoleResolverInput: 2,
	vrclog.ResourceRolePlaybackInput: 3,
}

// mediaProjector correlates resource/error Observations into MediaAttempts.
//
// Correlation is deliberately conservative: ambiguous or absent matches
// always start a new Attempt rather than risk merging two unrelated media
// sessions (spec: "誤mergeより分離を優先する"). Priority 3's "same
// component/backend" sub-tier from spec §15.14 is folded into the single
// unambiguous recent-candidate check below rather than implemented as a
// fully separate pass — the two YamaPlayer/iwaSync3 reference flows this
// exists to serve are both single-unambiguous-candidate scenarios, so the
// simpler rule produces the same grouping without adding another priority
// level to reason about.
type mediaProjector struct {
	recent                 []*MediaAttempt // newest first
	currentWorldInstanceID string
}

func newMediaProjector() *mediaProjector {
	return &mediaProjector{}
}

// resetSession is called on every definitive world transition. Recent
// history is intentionally preserved; only the "current session" scope
// used by correlation moves forward, so future Observations cannot merge
// into an Attempt from a previous world.
func (m *mediaProjector) resetSession(newWorldInstanceID string) {
	m.currentWorldInstanceID = newWorldInstanceID
}

func (m *mediaProjector) applyResourceURL(ev vrclog.ResourceURLObserved, obs observation.StoredObservation) []Change {
	target := targetFromEvent(ev.Target)
	res := MediaResourceObservation{
		URL: ev.Resource.URL, Kind: string(ev.Resource.Kind), Role: string(ev.Resource.Role),
		AdapterID: string(obs.AdapterID), RuleID: string(obs.RuleID), ObservationID: string(obs.ID), ObservedAt: obs.OccurredAt,
	}

	var attempt *MediaAttempt
	if ev.Resource.Role == vrclog.ResourceRoleSource {
		// role=source defaults to starting a new Attempt unless it
		// unambiguously matches an existing one by target or URL.
		attempt = m.findByExactTarget(target)
		if attempt == nil {
			attempt = m.findByExactURL(ev.Resource.URL)
		}
	} else {
		attempt = m.findCandidate(target, []string{ev.Resource.URL}, obs.OccurredAt)
	}

	isNew := attempt == nil
	if isNew {
		attempt = m.newAttempt(obs, target)
	}
	m.attachResource(attempt, res, target)
	m.touch(attempt, obs.OccurredAt)
	m.promote(attempt, isNew)

	return []Change{MediaAttemptUpdated{Attempt: attempt, At: obs.OccurredAt}}
}

func (m *mediaProjector) applyResourceResolved(ev vrclog.ResourceResolved, obs observation.StoredObservation) []Change {
	target := targetFromEvent(ev.Target)

	attempt := m.findByExactTarget(target)
	if attempt == nil {
		attempt = m.findByExactURL(ev.Input.URL)
	}
	if attempt == nil {
		attempt = m.findByExactURL(ev.Output.URL)
	}
	if attempt == nil {
		attempt = m.findSingleRecentCandidate(target, obs.OccurredAt)
	}

	isNew := attempt == nil
	if isNew {
		attempt = m.newAttempt(obs, target)
	}

	// Only the resolved Output is recorded, tagged role=resolved so
	// attachResource's Best-URL priority table excludes it — it stays in
	// details and never overrides a source/resolver/playback URL.
	outputRes := MediaResourceObservation{
		URL: ev.Output.URL, Kind: string(ev.Output.Kind), Role: string(vrclog.ResourceRoleResolved),
		AdapterID: string(obs.AdapterID), RuleID: string(obs.RuleID), ObservationID: string(obs.ID), ObservedAt: obs.OccurredAt,
	}
	m.attachResource(attempt, outputRes, target)
	m.touch(attempt, obs.OccurredAt)
	m.promote(attempt, isNew)

	return []Change{MediaAttemptUpdated{Attempt: attempt, At: obs.OccurredAt}}
}

func (m *mediaProjector) applyMediaError(ev vrclog.MediaErrorObserved, obs observation.StoredObservation) []Change {
	target := targetFromEvent(ev.Target)

	var errorURL string
	if ev.Resource != nil {
		errorURL = ev.Resource.URL
	}

	attempt := m.findByExactTarget(target)
	if attempt == nil && errorURL != "" {
		attempt = m.findByExactURL(errorURL)
	}
	if attempt == nil {
		attempt = m.findSingleRecentCandidate(target, obs.OccurredAt)
	}

	isNew := attempt == nil
	if isNew {
		attempt = m.newAttempt(obs, target)
	}

	mediaErr := MediaError{
		Stage: string(ev.Stage), Code: ev.Code, Message: ev.Message,
		AdapterID: string(obs.AdapterID), ObservationID: string(obs.ID), ObservedAt: obs.OccurredAt,
	}
	m.attachError(attempt, mediaErr)
	m.touch(attempt, obs.OccurredAt)
	m.promote(attempt, isNew)

	return []Change{MediaAttemptUpdated{Attempt: attempt, At: obs.OccurredAt}}
}

// findCandidate implements the shared exact-target -> exact-URL ->
// single-unambiguous-recent-candidate cascade used by resolver/playback
// resource events.
func (m *mediaProjector) findCandidate(target *MediaTargetDTO, urls []string, occurredAt time.Time) *MediaAttempt {
	if a := m.findByExactTarget(target); a != nil {
		return a
	}
	for _, u := range urls {
		if a := m.findByExactURL(u); a != nil {
			return a
		}
	}
	return m.findSingleRecentCandidate(target, occurredAt)
}

// currentSessionAttempts returns recent Attempts scoped to the current
// world session, so correlation never merges across a definitive world
// transition. Before the first world.joining_observed (WorldInstanceID
// unset), all recent Attempts are eligible.
func (m *mediaProjector) currentSessionAttempts() []*MediaAttempt {
	if m.currentWorldInstanceID == "" {
		return m.recent
	}
	out := make([]*MediaAttempt, 0, len(m.recent))
	for _, a := range m.recent {
		if a.WorldInstanceID == m.currentWorldInstanceID {
			out = append(out, a)
		}
	}
	return out
}

func (m *mediaProjector) findByExactTarget(target *MediaTargetDTO) *MediaAttempt {
	if target == nil || target.Key == "" {
		return nil
	}
	for _, a := range m.currentSessionAttempts() {
		if a.Target != nil && a.Target.Component == target.Component && a.Target.Key == target.Key {
			return a
		}
	}
	return nil
}

func (m *mediaProjector) findByExactURL(rawURL string) *MediaAttempt {
	if rawURL == "" {
		return nil
	}
	for _, a := range m.currentSessionAttempts() {
		for _, r := range a.Resources {
			if r.URL == rawURL {
				return a
			}
		}
	}
	return nil
}

// findSingleRecentCandidate implements priority 4: exactly one candidate
// within the correlation window, in the current world session, with no
// conflicting target.
func (m *mediaProjector) findSingleRecentCandidate(target *MediaTargetDTO, occurredAt time.Time) *MediaAttempt {
	var candidates []*MediaAttempt
	for _, a := range m.currentSessionAttempts() {
		if !withinWindow(a.LastObservedAt, occurredAt, mediaCorrelationWindow) {
			continue
		}
		if a.Target != nil && a.Target.Key != "" && target != nil && target.Key != "" &&
			!(a.Target.Component == target.Component && a.Target.Key == target.Key) {
			continue
		}
		candidates = append(candidates, a)
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return nil
}

func (m *mediaProjector) newAttempt(obs observation.StoredObservation, target *MediaTargetDTO) *MediaAttempt {
	return &MediaAttempt{
		ID:              string(obs.ID),
		Status:          MediaStatusObserved,
		Target:          target,
		WorldInstanceID: m.currentWorldInstanceID,
	}
}

func (m *mediaProjector) attachResource(a *MediaAttempt, res MediaResourceObservation, target *MediaTargetDTO) {
	a.Resources = append(a.Resources, res)
	if a.Target == nil && target != nil {
		a.Target = target
	}
	addUnique(&a.AdapterIDs, res.AdapterID)
	addUnique(&a.ObservationIDs, res.ObservationID)
	m.recomputeBestURL(a)
}

func (m *mediaProjector) attachError(a *MediaAttempt, e MediaError) {
	a.Errors = append(a.Errors, e)
	a.Status = MediaStatusFailed
	addUnique(&a.AdapterIDs, e.AdapterID)
	addUnique(&a.ObservationIDs, e.ObservationID)
}

// recomputeBestURL scans Resources in observation order for the
// highest-priority openable URL, keeping the first-observed URL within the
// winning priority tier stable across later attaches at the same tier.
func (m *mediaProjector) recomputeBestURL(a *MediaAttempt) {
	bestPriority := 0
	best := ""
	for _, r := range a.Resources {
		p, ok := resourceRolePriority[vrclog.ResourceRole(r.Role)]
		if !ok || !IsOpenableURL(r.URL) {
			continue
		}
		if best == "" || p < bestPriority {
			best = r.URL
			bestPriority = p
		}
	}
	a.BestOpenableURL = best
}

func (m *mediaProjector) touch(a *MediaAttempt, at time.Time) {
	if a.FirstObservedAt.IsZero() || at.Before(a.FirstObservedAt) {
		a.FirstObservedAt = at
	}
	if at.After(a.LastObservedAt) {
		a.LastObservedAt = at
	}
}

// promote moves a to the front of recent (newest-first) and enforces
// mediaMaxRecent, dropping the oldest projected entries. DB rows are
// untouched.
func (m *mediaProjector) promote(a *MediaAttempt, isNew bool) {
	if !isNew {
		for i, x := range m.recent {
			if x == a {
				m.recent = append(m.recent[:i], m.recent[i+1:]...)
				break
			}
		}
	}
	m.recent = append([]*MediaAttempt{a}, m.recent...)
	if len(m.recent) > mediaMaxRecent {
		m.recent = m.recent[:mediaMaxRecent]
	}
}

// latestOpenable returns the newest Attempt (across all world sessions)
// with a non-empty BestOpenableURL, including failed attempts.
func (m *mediaProjector) latestOpenable() *LatestOpenableMedia {
	for _, a := range m.recent {
		if a.BestOpenableURL != "" {
			return &LatestOpenableMedia{
				AttemptID: a.ID, URL: a.BestOpenableURL, Status: a.Status, ObservedAt: a.LastObservedAt,
			}
		}
	}
	return nil
}

// recentSnapshot returns defensive copies of the recent Attempts,
// newest-first, for safe hand-off to API handlers.
func (m *mediaProjector) recentSnapshot() []*MediaAttempt {
	out := make([]*MediaAttempt, len(m.recent))
	for i, a := range m.recent {
		cp := *a
		cp.Resources = append([]MediaResourceObservation(nil), a.Resources...)
		cp.Errors = append([]MediaError(nil), a.Errors...)
		cp.ObservationIDs = append([]string(nil), a.ObservationIDs...)
		cp.AdapterIDs = append([]string(nil), a.AdapterIDs...)
		out[i] = &cp
	}
	return out
}

func targetFromEvent(t *vrclog.MediaTarget) *MediaTargetDTO {
	if t == nil {
		return nil
	}
	return &MediaTargetDTO{Component: t.Component, Key: t.Key, Backend: string(t.Backend)}
}

func addUnique(list *[]string, v string) {
	if v == "" {
		return
	}
	for _, x := range *list {
		if x == v {
			return
		}
	}
	*list = append(*list, v)
}
