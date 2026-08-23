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
// attempt. It applies uniformly to exact-target, exact-URL, and
// single-recent-candidate matching alike — an exact match arbitrarily far
// in the past must not merge into a new, unrelated playback attempt. The
// comparison is inclusive (<=) so the boundary itself is a valid match,
// and startup rebuild reproduces the exact same grouping as the live path.
const mediaCorrelationWindow = 10 * time.Second

// mediaSourceDuplicateWindow bounds the narrower merge rule for
// role=source events: only an exact-URL duplicate observed again within
// this window is considered the same playback attempt (a duplicate
// emission burst, e.g. from two adapters observing the same source line).
// Any other role=source event — even one matching the same target or a
// slightly later duplicate URL outside this window — starts a new
// Attempt, since role=source is where a genuinely new playback most often
// begins.
const mediaSourceDuplicateWindow = 2 * time.Second

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

func (m *mediaProjector) applyResourceURL(ev vrclog.ResourceURLObserved, obs observation.StoredObservation, emit bool) []Change {
	target := targetFromEvent(ev.Target)
	res := MediaResourceObservation{
		URL: ev.Resource.URL, Kind: string(ev.Resource.Kind), Role: string(ev.Resource.Role),
		AdapterID: string(obs.AdapterID), RuleID: string(obs.RuleID), ObservationID: string(obs.ID), ObservedAt: obs.OccurredAt,
	}

	var attempt *MediaAttempt
	if ev.Resource.Role == vrclog.ResourceRoleSource {
		// role=source merges only into a genuine duplicate-emission burst
		// (spec §7.3); target/URL matches outside that narrow rule always
		// start a new Attempt, since role=source is where a new playback
		// attempt most often begins.
		attempt = m.findSourceDuplicate(ev.Resource.URL, target, obs.OccurredAt)
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

	if !emit {
		return nil
	}
	return []Change{MediaAttemptUpdated{Attempt: cloneMediaAttempt(attempt), At: obs.OccurredAt}}
}

func (m *mediaProjector) applyResourceResolved(ev vrclog.ResourceResolved, obs observation.StoredObservation, emit bool) []Change {
	target := targetFromEvent(ev.Target)

	// Correlation cascade per spec §7.5: Input URL -> exact target ->
	// Output URL -> single recent candidate -> new Attempt.
	attempt := m.findByExactURL(ev.Input.URL, target, obs.OccurredAt, mediaCorrelationWindow)
	if attempt == nil {
		attempt = m.findByExactTarget(target, obs.OccurredAt, mediaCorrelationWindow)
	}
	if attempt == nil {
		attempt = m.findByExactURL(ev.Output.URL, target, obs.OccurredAt, mediaCorrelationWindow)
	}
	if attempt == nil {
		attempt = m.findSingleRecentCandidate(target, obs.OccurredAt, mediaCorrelationWindow)
	}

	isNew := attempt == nil
	if isNew {
		attempt = m.newAttempt(obs, target)
	}

	// Both Input and Output are recorded as Resources, each keeping its
	// own canonical Kind/Role/URL (spec §7.5) — this is what lets a
	// standalone ResourceResolved (no prior ResourceURLObserved) still
	// surface Input as a BestOpenableURL candidate, since Input typically
	// carries an openable role (e.g. resolver_input) while Output's role
	// (typically resolved) is excluded from the priority table.
	inputRes := MediaResourceObservation{
		URL: ev.Input.URL, Kind: string(ev.Input.Kind), Role: string(ev.Input.Role),
		AdapterID: string(obs.AdapterID), RuleID: string(obs.RuleID), ObservationID: string(obs.ID), ObservedAt: obs.OccurredAt,
	}
	m.attachResource(attempt, inputRes, target)

	outputRes := MediaResourceObservation{
		URL: ev.Output.URL, Kind: string(ev.Output.Kind), Role: string(ev.Output.Role),
		AdapterID: string(obs.AdapterID), RuleID: string(obs.RuleID), ObservationID: string(obs.ID), ObservedAt: obs.OccurredAt,
	}
	m.attachResource(attempt, outputRes, target)

	m.touch(attempt, obs.OccurredAt)
	m.promote(attempt, isNew)

	if !emit {
		return nil
	}
	return []Change{MediaAttemptUpdated{Attempt: cloneMediaAttempt(attempt), At: obs.OccurredAt}}
}

func (m *mediaProjector) applyMediaError(ev vrclog.MediaErrorObserved, obs observation.StoredObservation, emit bool) []Change {
	target := targetFromEvent(ev.Target)

	var errorURL string
	if ev.Resource != nil {
		errorURL = ev.Resource.URL
	}

	// Correlation cascade per spec §7.6: exact URL (if a Resource is
	// present) -> exact target -> single recent candidate -> new Attempt.
	var attempt *MediaAttempt
	if errorURL != "" {
		attempt = m.findByExactURL(errorURL, target, obs.OccurredAt, mediaCorrelationWindow)
	}
	if attempt == nil {
		attempt = m.findByExactTarget(target, obs.OccurredAt, mediaCorrelationWindow)
	}
	if attempt == nil {
		attempt = m.findSingleRecentCandidate(target, obs.OccurredAt, mediaCorrelationWindow)
	}

	isNew := attempt == nil
	if isNew {
		attempt = m.newAttempt(obs, target)
	}

	// Attach the error's own Resource (if present) so its URL can still
	// become a BestOpenableURL candidate — an error-only Observation that
	// carries an openable URL (e.g. role=source) must not lose it just
	// because it was only used for correlation lookup above. No current
	// adapter (core/yamaplayer/iwasync3) populates MediaErrorObserved.Resource,
	// but the canonical field exists and future adapters may use it.
	if ev.Resource != nil {
		res := MediaResourceObservation{
			URL: ev.Resource.URL, Kind: string(ev.Resource.Kind), Role: string(ev.Resource.Role),
			AdapterID: string(obs.AdapterID), RuleID: string(obs.RuleID), ObservationID: string(obs.ID), ObservedAt: obs.OccurredAt,
		}
		m.attachResource(attempt, res, target)
	}

	mediaErr := MediaError{
		Stage: string(ev.Stage), Code: ev.Code, Message: ev.Message,
		AdapterID: string(obs.AdapterID), ObservationID: string(obs.ID), ObservedAt: obs.OccurredAt,
	}
	m.attachError(attempt, mediaErr)
	m.touch(attempt, obs.OccurredAt)
	m.promote(attempt, isNew)

	if !emit {
		return nil
	}
	return []Change{MediaAttemptUpdated{Attempt: cloneMediaAttempt(attempt), At: obs.OccurredAt}}
}

// findCandidate implements the shared exact-target -> exact-URL ->
// single-unambiguous-recent-candidate cascade used by resolver/playback
// resource events (spec §7.4), all bounded by mediaCorrelationWindow.
func (m *mediaProjector) findCandidate(target *MediaTargetDTO, urls []string, occurredAt time.Time) *MediaAttempt {
	if a := m.findByExactTarget(target, occurredAt, mediaCorrelationWindow); a != nil {
		return a
	}
	for _, u := range urls {
		if a := m.findByExactURL(u, target, occurredAt, mediaCorrelationWindow); a != nil {
			return a
		}
	}
	return m.findSingleRecentCandidate(target, occurredAt, mediaCorrelationWindow)
}

// findSourceDuplicate implements the narrow role=source merge rule (spec
// §7.3): the exact same URL observed again within mediaSourceDuplicateWindow,
// with a non-conflicting target, in the current world session. Any other
// case — a different URL, a same-target-different-URL event, or a
// same-URL event outside the window — returns nil so the caller starts a
// new Attempt instead.
func (m *mediaProjector) findSourceDuplicate(rawURL string, target *MediaTargetDTO, occurredAt time.Time) *MediaAttempt {
	if rawURL == "" {
		return nil
	}
	for _, a := range m.currentSessionAttempts() {
		if !withinWindow(a.LastObservedAt, occurredAt, mediaSourceDuplicateWindow) {
			continue
		}
		if targetConflicts(a.Target, target) {
			continue
		}
		for _, r := range a.Resources {
			if r.URL == rawURL {
				return a
			}
		}
	}
	return nil
}

// targetConflicts reports whether a and b both specify a non-empty
// Component+Key identity and those identities differ — i.e. they
// unambiguously name two different on-screen targets. A nil or
// empty-Key target on either side is not a conflict (absence of
// information, not evidence of a different target).
func targetConflicts(a, b *MediaTargetDTO) bool {
	if a == nil || a.Key == "" || b == nil || b.Key == "" {
		return false
	}
	return a.Component != b.Component || a.Key != b.Key
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

// findByExactTarget matches an Attempt sharing target's exact
// Component+Key, bounded by window (spec §7.2: no unbounded-time helper —
// every exact match still needs to be recent relative to occurredAt).
func (m *mediaProjector) findByExactTarget(target *MediaTargetDTO, occurredAt time.Time, window time.Duration) *MediaAttempt {
	if target == nil || target.Key == "" {
		return nil
	}
	for _, a := range m.currentSessionAttempts() {
		if a.Target != nil && a.Target.Component == target.Component && a.Target.Key == target.Key &&
			withinWindow(a.LastObservedAt, occurredAt, window) {
			return a
		}
	}
	return nil
}

// findByExactURL matches an Attempt with a Resource sharing rawURL
// exactly, bounded by window and target compatibility — two different
// on-screen targets that happen to share a URL (e.g. the same video URL
// played on two different AVPro players) must never merge just because
// the URL matches, mirroring the same targetConflicts guard used by
// findSourceDuplicate and findSingleRecentCandidate.
func (m *mediaProjector) findByExactURL(rawURL string, target *MediaTargetDTO, occurredAt time.Time, window time.Duration) *MediaAttempt {
	if rawURL == "" {
		return nil
	}
	for _, a := range m.currentSessionAttempts() {
		if !withinWindow(a.LastObservedAt, occurredAt, window) {
			continue
		}
		if targetConflicts(a.Target, target) {
			continue
		}
		for _, r := range a.Resources {
			if r.URL == rawURL {
				return a
			}
		}
	}
	return nil
}

// findSingleRecentCandidate implements priority 4: exactly one candidate
// within window, in the current world session, with no conflicting target.
func (m *mediaProjector) findSingleRecentCandidate(target *MediaTargetDTO, occurredAt time.Time, window time.Duration) *MediaAttempt {
	var candidates []*MediaAttempt
	for _, a := range m.currentSessionAttempts() {
		if !withinWindow(a.LastObservedAt, occurredAt, window) {
			continue
		}
		if targetConflicts(a.Target, target) {
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

// cloneMediaAttempt returns a deep copy of a — including Target and every
// slice field — so the result shares no mutable memory with the internal
// MediaAttempt a points to. This is the single clone contract used
// everywhere a MediaAttempt crosses the Manager boundary: Change emission,
// recentSnapshot, and any future API DTO conversion.
func cloneMediaAttempt(a *MediaAttempt) MediaAttempt {
	cp := *a
	if a.Target != nil {
		t := *a.Target
		cp.Target = &t
	}
	cp.Resources = append([]MediaResourceObservation(nil), a.Resources...)
	cp.Errors = append([]MediaError(nil), a.Errors...)
	cp.ObservationIDs = append([]string(nil), a.ObservationIDs...)
	cp.AdapterIDs = append([]string(nil), a.AdapterIDs...)
	return cp
}

// recentSnapshot returns defensive deep copies of the recent Attempts,
// newest-first, for safe hand-off to API handlers.
func (m *mediaProjector) recentSnapshot() []MediaAttempt {
	out := make([]MediaAttempt, len(m.recent))
	for i, a := range m.recent {
		out[i] = cloneMediaAttempt(a)
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
