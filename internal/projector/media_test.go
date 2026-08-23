package projector

import (
	"context"
	"testing"
	"time"

	vrclog "github.com/vrclog/vrclog-go"

	"github.com/vrclog/vrclog-companion/internal/observation"
)

func resourceURLObs(id string, resource vrclog.RemoteResource, target *vrclog.MediaTarget, adapterID vrclog.AdapterID, at time.Time) observation.StoredObservation {
	obs, _ := observation.FromVrclogObservation(vrclog.Observation{
		ID: vrclog.ObservationID(id), Time: at, AdapterID: adapterID,
		Event: vrclog.ResourceURLObserved{Resource: resource, Target: target},
	}, at)
	obs.OccurredAt = at
	return obs
}

func resourceResolvedObs(id string, input, output vrclog.RemoteResource, target *vrclog.MediaTarget, adapterID vrclog.AdapterID, at time.Time) observation.StoredObservation {
	obs, _ := observation.FromVrclogObservation(vrclog.Observation{
		ID: vrclog.ObservationID(id), Time: at, AdapterID: adapterID,
		Event: vrclog.ResourceResolved{Input: input, Output: output, Target: target},
	}, at)
	obs.OccurredAt = at
	return obs
}

func mediaErrorObs(id string, stage vrclog.MediaStage, message string, resource *vrclog.RemoteResource, target *vrclog.MediaTarget, adapterID vrclog.AdapterID, at time.Time) observation.StoredObservation {
	obs, _ := observation.FromVrclogObservation(vrclog.Observation{
		ID: vrclog.ObservationID(id), Time: at, AdapterID: adapterID,
		Event: vrclog.MediaErrorObserved{Stage: stage, Message: message, Resource: resource, Target: target},
	}, at)
	obs.OccurredAt = at
	return obs
}

func TestMedia_YamaPlayerScenario(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()
	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))

	const youtubeURL = "https://www.youtube.com/watch?v=abc123"
	const relayURL = "https://relay.internal/proxy?u=abc123"

	applyOne(t, m, resourceURLObs("o1",
		vrclog.RemoteResource{URL: youtubeURL, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		nil, "community.yamaplayer", base.Add(1*time.Second)))

	applyOne(t, m, resourceURLObs("o2",
		vrclog.RemoteResource{URL: relayURL, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleResolverInput},
		nil, "vrchat.core", base.Add(3*time.Second)))

	applyOne(t, m, mediaErrorObs("o3", vrclog.MediaStagePlayback, "AVPro open failed",
		nil, nil, "vrchat.core", base.Add(5*time.Second)))

	applyOne(t, m, mediaErrorObs("o4", vrclog.MediaStagePlayback, "video error",
		nil, nil, "community.yamaplayer", base.Add(6*time.Second)))

	recent := m.RecentMedia(0)
	if len(recent) != 1 {
		t.Fatalf("RecentMedia = %d attempts, want 1: %+v", len(recent), recent)
	}
	attempt := recent[0]

	if attempt.BestOpenableURL != youtubeURL {
		t.Fatalf("BestOpenableURL = %q, want original YouTube URL %q", attempt.BestOpenableURL, youtubeURL)
	}
	if attempt.Status != MediaStatusFailed {
		t.Fatalf("Status = %q, want failed", attempt.Status)
	}
	foundRelay := false
	for _, r := range attempt.Resources {
		if r.URL == relayURL {
			foundRelay = true
		}
	}
	if !foundRelay {
		t.Fatalf("relay URL missing from Resources: %+v", attempt.Resources)
	}
	hasCore, hasYama := false, false
	for _, a := range attempt.AdapterIDs {
		if a == "vrchat.core" {
			hasCore = true
		}
		if a == "community.yamaplayer" {
			hasYama = true
		}
	}
	if !hasCore || !hasYama {
		t.Fatalf("AdapterIDs = %v, want both vrchat.core and community.yamaplayer", attempt.AdapterIDs)
	}
}

func TestMedia_IwaSync3Scenario(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()
	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))

	const sourceURL = "https://example.com/stream.mp4"

	applyOne(t, m, resourceURLObs("o1",
		vrclog.RemoteResource{URL: sourceURL, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		nil, "vrchat.core", base.Add(1*time.Second)))

	applyOne(t, m, mediaErrorObs("o2", vrclog.MediaStagePlayback, "PlayerError",
		nil, nil, "community.iwasync3", base.Add(3*time.Second)))

	recent := m.RecentMedia(0)
	if len(recent) != 1 {
		t.Fatalf("RecentMedia = %d attempts, want 1: %+v", len(recent), recent)
	}
	attempt := recent[0]
	if attempt.BestOpenableURL != sourceURL {
		t.Fatalf("BestOpenableURL = %q, want %q", attempt.BestOpenableURL, sourceURL)
	}
	if attempt.Status != MediaStatusFailed {
		t.Fatalf("Status = %q, want failed", attempt.Status)
	}
}

func TestMedia_DifferentTargetKeysProduceSeparateAttempts(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()
	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))

	target1 := &vrclog.MediaTarget{Component: "AVPro", Key: "player1", Backend: vrclog.MediaBackendAVPro}
	target2 := &vrclog.MediaTarget{Component: "AVPro", Key: "player2", Backend: vrclog.MediaBackendAVPro}

	applyOne(t, m, resourceURLObs("o1",
		vrclog.RemoteResource{URL: "https://a.example.com/1", Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		target1, "vrchat.core", base.Add(1*time.Second)))

	applyOne(t, m, resourceURLObs("o2",
		vrclog.RemoteResource{URL: "https://b.example.com/2", Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		target2, "vrchat.core", base.Add(2*time.Second)))

	recent := m.RecentMedia(0)
	if len(recent) != 2 {
		t.Fatalf("RecentMedia = %d attempts, want 2 (different target keys must not merge): %+v", len(recent), recent)
	}
}

func TestMedia_ResolvedURLNeverBecomesBest(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()
	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))

	const inputURL = "https://relay.internal/attempt"
	const signedOutputURL = "https://cdn.example.com/signed?token=abc123"

	applyOne(t, m, resourceURLObs("o1",
		vrclog.RemoteResource{URL: inputURL, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleResolverInput},
		nil, "vrchat.core", base.Add(1*time.Second)))

	applyOne(t, m, resourceResolvedObs("o2",
		vrclog.RemoteResource{URL: inputURL, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleResolverInput},
		vrclog.RemoteResource{URL: signedOutputURL, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleResolved},
		nil, "vrchat.core", base.Add(2*time.Second)))

	recent := m.RecentMedia(0)
	if len(recent) != 1 {
		t.Fatalf("RecentMedia = %d attempts, want 1: %+v", len(recent), recent)
	}
	attempt := recent[0]
	if attempt.BestOpenableURL == signedOutputURL {
		t.Fatalf("BestOpenableURL must never be the resolved/signed URL, got %q", attempt.BestOpenableURL)
	}
	if attempt.BestOpenableURL != inputURL {
		t.Fatalf("BestOpenableURL = %q, want resolver_input URL %q", attempt.BestOpenableURL, inputURL)
	}

	foundResolved := false
	for _, r := range attempt.Resources {
		if r.URL == signedOutputURL {
			foundResolved = true
		}
	}
	if !foundResolved {
		t.Fatalf("resolved URL missing from details: %+v", attempt.Resources)
	}
}

// TestMedia_ResolvedOnly_InputIsBest pins hardening spec §7.5/§7.8#10: a
// standalone ResourceResolved (no prior ResourceURLObserved) must still
// yield a usable BestOpenableURL, taken from Input (whose role is
// typically openable, e.g. resolver_input) — never from Output (whose role
// is typically the excluded "resolved" signed-CDN URL). Input is recorded
// in Resources alongside Output for full detail, not just used for
// correlation lookup.
func TestMedia_ResolvedOnly_InputIsBest(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()
	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))

	const inputURL = "https://relay.internal/x"
	const outputURL = "https://cdn.example.com/signed?token=xyz"

	target := &vrclog.MediaTarget{Component: "AVPro", Key: "solo", Backend: vrclog.MediaBackendAVPro}
	applyOne(t, m, resourceResolvedObs("o1",
		vrclog.RemoteResource{URL: inputURL, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleResolverInput},
		vrclog.RemoteResource{URL: outputURL, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleResolved},
		target, "vrchat.core", base.Add(1*time.Second)))

	recent := m.RecentMedia(0)
	if len(recent) != 1 {
		t.Fatalf("RecentMedia = %d attempts, want 1", len(recent))
	}
	attempt := recent[0]
	if attempt.BestOpenableURL != inputURL {
		t.Fatalf("BestOpenableURL = %q, want Input URL %q", attempt.BestOpenableURL, inputURL)
	}

	foundInput, foundOutput := false, false
	for _, r := range attempt.Resources {
		if r.URL == inputURL {
			foundInput = true
		}
		if r.URL == outputURL {
			foundOutput = true
		}
	}
	if !foundInput {
		t.Fatalf("Input URL missing from Resources: %+v", attempt.Resources)
	}
	if !foundOutput {
		t.Fatalf("Output URL missing from Resources (details): %+v", attempt.Resources)
	}
}

// TestMedia_SameURL5MinLater_TwoAttempts pins spec §7.8#1: the same source
// URL played again 5 minutes later — far outside both the source
// duplicate-burst window (2s) and the general correlation window (10s) —
// must start a new Attempt, never merge indefinitely by URL alone.
func TestMedia_SameURL5MinLater_TwoAttempts(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()
	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))

	const url = "https://example.com/video.mp4"
	applyOne(t, m, resourceURLObs("o1",
		vrclog.RemoteResource{URL: url, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		nil, "vrchat.core", base.Add(1*time.Second)))
	applyOne(t, m, resourceURLObs("o2",
		vrclog.RemoteResource{URL: url, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		nil, "vrchat.core", base.Add(5*time.Minute)))

	recent := m.RecentMedia(0)
	if len(recent) != 2 {
		t.Fatalf("RecentMedia = %d attempts, want 2 (same URL 5m later must not merge): %+v", len(recent), recent)
	}
}

// TestMedia_SameTargetDiffURL30s_TwoAttempts pins spec §7.8#2: the same
// target key with a different URL 30 seconds later (beyond the source
// duplicate window, and — per §7.3 — target alone is never sufficient for
// role=source) must produce two Attempts.
func TestMedia_SameTargetDiffURL30s_TwoAttempts(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()
	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))

	target := &vrclog.MediaTarget{Component: "AVPro", Key: "shared", Backend: vrclog.MediaBackendAVPro}
	applyOne(t, m, resourceURLObs("o1",
		vrclog.RemoteResource{URL: "https://example.com/first.mp4", Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		target, "vrchat.core", base.Add(1*time.Second)))
	applyOne(t, m, resourceURLObs("o2",
		vrclog.RemoteResource{URL: "https://example.com/second.mp4", Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		target, "vrchat.core", base.Add(31*time.Second)))

	recent := m.RecentMedia(0)
	if len(recent) != 2 {
		t.Fatalf("RecentMedia = %d attempts, want 2 (same target, different URL, must not merge): %+v", len(recent), recent)
	}
}

// TestMedia_DuplicateSourceBurst1s_OneAttempt pins spec §7.8#3: the exact
// same source URL re-observed within 1 second (e.g. two adapters emitting
// for the same log line) is a duplicate-emission burst and merges into one
// Attempt.
func TestMedia_DuplicateSourceBurst1s_OneAttempt(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()
	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))

	const url = "https://example.com/burst.mp4"
	applyOne(t, m, resourceURLObs("o1",
		vrclog.RemoteResource{URL: url, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		nil, "community.yamaplayer", base.Add(1*time.Second)))
	applyOne(t, m, resourceURLObs("o2",
		vrclog.RemoteResource{URL: url, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		nil, "vrchat.core", base.Add(2*time.Second)))

	recent := m.RecentMedia(0)
	if len(recent) != 1 {
		t.Fatalf("RecentMedia = %d attempts, want 1 (duplicate source burst within 1s must merge): %+v", len(recent), recent)
	}
}

// TestMedia_SameURL3sSourceRe_TwoAttempts pins spec §7.8#4: the same URL
// re-observed as a role=source event 3 seconds later — outside the 2s
// mediaSourceDuplicateWindow — must start a new Attempt rather than merge.
func TestMedia_SameURL3sSourceRe_TwoAttempts(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()
	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))

	const url = "https://example.com/reentry.mp4"
	applyOne(t, m, resourceURLObs("o1",
		vrclog.RemoteResource{URL: url, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		nil, "vrchat.core", base.Add(1*time.Second)))
	applyOne(t, m, resourceURLObs("o2",
		vrclog.RemoteResource{URL: url, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		nil, "vrchat.core", base.Add(4*time.Second)))

	recent := m.RecentMedia(0)
	if len(recent) != 2 {
		t.Fatalf("RecentMedia = %d attempts, want 2 (same URL 3s later, outside the 2s burst window, must not merge): %+v", len(recent), recent)
	}
}

// TestMedia_TwoPlayersInterleaved_NoMixup pins spec §7.8#6: two distinct
// on-screen targets playing concurrently, with their source/error events
// interleaved in time, must never cross-contaminate each other's Attempt.
func TestMedia_TwoPlayersInterleaved_NoMixup(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()
	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))

	targetA := &vrclog.MediaTarget{Component: "AVPro", Key: "playerA", Backend: vrclog.MediaBackendAVPro}
	targetB := &vrclog.MediaTarget{Component: "AVPro", Key: "playerB", Backend: vrclog.MediaBackendAVPro}
	const urlA = "https://example.com/a.mp4"
	const urlB = "https://example.com/b.mp4"

	applyOne(t, m, resourceURLObs("a1",
		vrclog.RemoteResource{URL: urlA, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		targetA, "vrchat.core", base.Add(1*time.Second)))
	applyOne(t, m, resourceURLObs("b1",
		vrclog.RemoteResource{URL: urlB, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		targetB, "vrchat.core", base.Add(2*time.Second)))
	applyOne(t, m, mediaErrorObs("a2", vrclog.MediaStagePlayback, "A failed",
		nil, targetA, "vrchat.core", base.Add(3*time.Second)))
	applyOne(t, m, mediaErrorObs("b2", vrclog.MediaStagePlayback, "B failed",
		nil, targetB, "vrchat.core", base.Add(4*time.Second)))

	recent := m.RecentMedia(0)
	if len(recent) != 2 {
		t.Fatalf("RecentMedia = %d attempts, want 2 (interleaved players must not mix): %+v", len(recent), recent)
	}
	for _, a := range recent {
		if a.Target == nil {
			t.Fatalf("attempt %+v missing Target", a)
		}
		switch a.Target.Key {
		case "playerA":
			if a.BestOpenableURL != urlA {
				t.Errorf("playerA BestOpenableURL = %q, want %q", a.BestOpenableURL, urlA)
			}
			if len(a.Errors) != 1 || a.Errors[0].Message != "A failed" {
				t.Errorf("playerA Errors = %+v, want exactly [A failed]", a.Errors)
			}
		case "playerB":
			if a.BestOpenableURL != urlB {
				t.Errorf("playerB BestOpenableURL = %q, want %q", a.BestOpenableURL, urlB)
			}
			if len(a.Errors) != 1 || a.Errors[0].Message != "B failed" {
				t.Errorf("playerB Errors = %+v, want exactly [B failed]", a.Errors)
			}
		default:
			t.Fatalf("unexpected target key %q", a.Target.Key)
		}
	}
}

// TestMedia_ExactTargetURLConflict_NewAttempt pins spec §7.8#7: a
// role=source event sharing an existing Attempt's exact target but with a
// conflicting (different) URL must start a new Attempt — exact target
// alone is never sufficient to merge a source event.
func TestMedia_ExactTargetURLConflict_NewAttempt(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()
	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))

	target := &vrclog.MediaTarget{Component: "AVPro", Key: "solo", Backend: vrclog.MediaBackendAVPro}
	applyOne(t, m, resourceURLObs("o1",
		vrclog.RemoteResource{URL: "https://example.com/one.mp4", Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		target, "vrchat.core", base.Add(1*time.Second)))
	// Same target, different URL, well within any time window.
	applyOne(t, m, resourceURLObs("o2",
		vrclog.RemoteResource{URL: "https://example.com/two.mp4", Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		target, "vrchat.core", base.Add(1500*time.Millisecond)))

	recent := m.RecentMedia(0)
	if len(recent) != 2 {
		t.Fatalf("RecentMedia = %d attempts, want 2 (same target, conflicting URL, source event must not merge): %+v", len(recent), recent)
	}
}

// TestMedia_AmbiguousTwoCandidates_NewAttempt pins spec §7.8#8: when a
// non-source resource event has no exact target/URL match, and exactly
// TWO (not one) recent same-session candidates fall within the
// correlation window, findSingleRecentCandidate must refuse to pick either
// — ambiguity always starts a new Attempt.
func TestMedia_AmbiguousTwoCandidates_NewAttempt(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()
	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))

	// Two untargeted attempts, both recent, both without a target — an
	// ambiguous pool for a later untargeted resolver_input event.
	applyOne(t, m, resourceURLObs("o1",
		vrclog.RemoteResource{URL: "https://example.com/one.mp4", Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		nil, "vrchat.core", base.Add(1*time.Second)))
	applyOne(t, m, resourceURLObs("o2",
		vrclog.RemoteResource{URL: "https://example.com/two.mp4", Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		nil, "vrchat.core", base.Add(2*time.Second)))

	// A resolver_input event with no target and a URL matching neither
	// existing attempt: falls through to findSingleRecentCandidate, which
	// sees 2 candidates within the 10s window and refuses to pick.
	applyOne(t, m, resourceURLObs("o3",
		vrclog.RemoteResource{URL: "https://relay.internal/ambiguous", Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleResolverInput},
		nil, "vrchat.core", base.Add(3*time.Second)))

	recent := m.RecentMedia(0)
	if len(recent) != 3 {
		t.Fatalf("RecentMedia = %d attempts, want 3 (ambiguous candidate must start a new Attempt, not guess): %+v", len(recent), recent)
	}
}

// TestMedia_RebuildMatchesLive pins spec §7.8#11 and §8.3: replaying the
// same Observation sequence via Rebuild must produce byte-for-byte
// identical RecentMedia results to the live Apply path — including an
// edge case where two Observations share the exact same OccurredAt, so
// correlation must not depend on wall-clock or Apply-call ordering
// artifacts, only on the deterministic sequence order Rebuild replays in.
func TestMedia_RebuildMatchesLive(t *testing.T) {
	base := time.Now().UTC()
	sameInstant := base.Add(2 * time.Second)

	target1 := &vrclog.MediaTarget{Component: "AVPro", Key: "p1", Backend: vrclog.MediaBackendAVPro}
	target2 := &vrclog.MediaTarget{Component: "AVPro", Key: "p2", Backend: vrclog.MediaBackendAVPro}

	obsSeq := []observation.StoredObservation{
		joiningObs("j1", "wrld_1", "inst_1", base),
		resourceURLObs("o1",
			vrclog.RemoteResource{URL: "https://example.com/1.mp4", Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
			target1, "vrchat.core", base.Add(1*time.Second)),
		// Two Observations sharing the exact same OccurredAt, targeting
		// two different attempts — correlation must resolve each to its
		// own Target deterministically regardless of tie ordering.
		resourceURLObs("o2",
			vrclog.RemoteResource{URL: "https://example.com/2.mp4", Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
			target2, "vrchat.core", sameInstant),
		mediaErrorObs("o3", vrclog.MediaStagePlayback, "p1 failed",
			nil, target1, "vrchat.core", sameInstant),
	}

	live := NewManager()
	for i := range obsSeq {
		obsSeq[i].Sequence = int64(i + 1)
		if _, err := live.Apply(obsSeq[i]); err != nil {
			t.Fatalf("live Apply(%s): %v", obsSeq[i].ID, err)
		}
	}

	rebuilt := NewManager()
	if err := rebuilt.Rebuild(context.Background(), seqOf(obsSeq...)); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	liveRecent := live.RecentMedia(0)
	rebuiltRecent := rebuilt.RecentMedia(0)
	if len(liveRecent) != len(rebuiltRecent) {
		t.Fatalf("live RecentMedia = %d attempts, rebuilt = %d: live=%+v rebuilt=%+v",
			len(liveRecent), len(rebuiltRecent), liveRecent, rebuiltRecent)
	}
	for i := range liveRecent {
		lv, rb := liveRecent[i], rebuiltRecent[i]
		if lv.BestOpenableURL != rb.BestOpenableURL || lv.Status != rb.Status ||
			(lv.Target == nil) != (rb.Target == nil) ||
			(lv.Target != nil && rb.Target != nil && *lv.Target != *rb.Target) {
			t.Fatalf("attempt %d differs: live=%+v rebuilt=%+v", i, lv, rb)
		}
	}
}

// TestMedia_ExactURLDoesNotMergeAcrossTargets pins the CRITICAL code-review
// fix: findByExactURL previously ignored target conflicts, unlike
// findSourceDuplicate/findSingleRecentCandidate. Two different on-screen
// targets (playerA, playerB) both emitting a resolver_input
// ResourceURLObserved with the SAME URL within the correlation window must
// produce two Attempts, not merge into one just because the URL matches.
func TestMedia_ExactURLDoesNotMergeAcrossTargets(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()
	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))

	targetA := &vrclog.MediaTarget{Component: "AVPro", Key: "playerA", Backend: vrclog.MediaBackendAVPro}
	targetB := &vrclog.MediaTarget{Component: "AVPro", Key: "playerB", Backend: vrclog.MediaBackendAVPro}
	const sharedURL = "https://example.com/shared.mp4"

	applyOne(t, m, resourceURLObs("a1",
		vrclog.RemoteResource{URL: sharedURL, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleResolverInput},
		targetA, "vrchat.core", base.Add(1*time.Second)))
	applyOne(t, m, resourceURLObs("b1",
		vrclog.RemoteResource{URL: sharedURL, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleResolverInput},
		targetB, "vrchat.core", base.Add(3*time.Second)))

	recent := m.RecentMedia(0)
	if len(recent) != 2 {
		t.Fatalf("RecentMedia = %d attempts, want 2 (different targets sharing a URL must not merge): %+v", len(recent), recent)
	}
}

// TestMedia_ErrorResourceAttachedToBestURL pins the WARNING code-review
// fix: applyMediaError previously used ev.Resource's URL only for
// correlation lookup, never attaching it as a Resource — so an error-only
// Observation carrying an openable URL never became a BestOpenableURL
// candidate.
func TestMedia_ErrorResourceAttachedToBestURL(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()
	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))

	const sourceURL = "https://example.com/error-source.mp4"
	resource := &vrclog.RemoteResource{URL: sourceURL, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource}

	applyOne(t, m, mediaErrorObs("e1", vrclog.MediaStagePlayback, "playback failed",
		resource, nil, "vrchat.core", base.Add(1*time.Second)))

	recent := m.RecentMedia(0)
	if len(recent) != 1 {
		t.Fatalf("RecentMedia = %d attempts, want 1", len(recent))
	}
	if recent[0].BestOpenableURL != sourceURL {
		t.Fatalf("BestOpenableURL = %q, want %q (MediaErrorObserved.Resource must be attached, not just used for lookup)", recent[0].BestOpenableURL, sourceURL)
	}
}

func TestMedia_WorldBoundaryObservationsNotMerged(t *testing.T) {
	m := NewManager()
	base := time.Now().UTC()

	applyOne(t, m, joiningObs("j1", "wrld_1", "inst_1", base))
	const url1 = "https://example.com/session1.mp4"
	applyOne(t, m, resourceURLObs("o1",
		vrclog.RemoteResource{URL: url1, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		nil, "vrchat.core", base.Add(1*time.Second)))

	// Definitive world transition.
	applyOne(t, m, joiningObs("j2", "wrld_2", "inst_2", base.Add(2*time.Second)))

	// Same URL observed again in the new world session: because
	// currentSessionAttempts() scopes correlation to inst_2, this must NOT
	// merge into the inst_1 attempt even though the URL string matches.
	applyOne(t, m, resourceURLObs("o2",
		vrclog.RemoteResource{URL: url1, Kind: vrclog.ResourceKindVideo, Role: vrclog.ResourceRoleSource},
		nil, "vrchat.core", base.Add(3*time.Second)))

	recent := m.RecentMedia(0)
	if len(recent) != 2 {
		t.Fatalf("RecentMedia = %d attempts, want 2 (recent history must be preserved across world transition, not merged): %+v", len(recent), recent)
	}
}
