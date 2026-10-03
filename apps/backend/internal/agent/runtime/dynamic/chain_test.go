package dynamic

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/agent/runtime/routingpolicy"
)

func tierCandidate(id string, mode TierMode, direction FailureDirection, join bool, cost CostClass) Candidate {
	candidate := Candidate{
		ID: id, Enabled: true, BindingKey: "binding:" + id,
		Policies: routingpolicy.DefaultDocument(),
		Selection: Selection{
			JoinPrevious: join,
			Model:        ModelOptions{Cost: cost, UsageSource: UsageAutomatic},
		},
	}
	if !join {
		candidate.Selection.Tier = &TierPolicy{Mode: mode, OnFailure: direction}
	}
	return candidate
}

func hardSkipFailure() *routingerr.Error {
	return &routingerr.Error{
		Code: routingerr.CodeQuotaLimited, Class: routingerr.ClassHard,
		FallbackAllowed: true,
	}
}

// TestDynamicTierTransitionChainSameTierNext covers the A-B-C case: a
// same-tier failure walks the remaining peers, then advances forward, and never
// returns to a candidate already tried in the chain.
func TestDynamicTierTransitionChainSameTierNext(t *testing.T) {
	engine := NewEngine(WithClock(func() time.Time { return time.Unix(1000, 0) }))
	profile := Profile{
		ID: "tiered", Version: 1,
		Candidates: []Candidate{
			tierCandidate("a", TierModeOrder, FailureSameTierNext, false, CostFree),
			tierCandidate("b", TierModeOrder, FailureSameTierNext, true, CostFree),
			tierCandidate("c", TierModeOrder, FailureSameTierNext, true, CostFree),
			tierCandidate("d", TierModeOrder, FailureSameTierNext, false, CostFree),
		},
	}
	first, err := engine.Select("session", profile, 0, "")
	if err != nil || first.ExecutionProfileID != "a" {
		t.Fatalf("first = %#v, err = %v", first, err)
	}
	second, err := engine.ApplyFailure("session", profile, first.Generation, "a", hardSkipFailure())
	if err != nil {
		t.Fatalf("apply failure a: %v", err)
	}
	if second.ExecutionProfileID != "b" {
		t.Fatalf("second = %q, want b (same tier, next peer)", second.ExecutionProfileID)
	}
	third, err := engine.ApplyFailure("session", profile, second.Generation, "b", hardSkipFailure())
	if err != nil {
		t.Fatalf("apply failure b: %v", err)
	}
	if third.ExecutionProfileID != "c" {
		t.Fatalf("third = %q, want c (same tier, next peer)", third.ExecutionProfileID)
	}
	fourth, err := engine.ApplyFailure("session", profile, third.Generation, "c", hardSkipFailure())
	if err != nil {
		t.Fatalf("apply failure c: %v", err)
	}
	if fourth.ExecutionProfileID != "d" {
		t.Fatalf("fourth = %q, want d (exhausted tier advances forward)", fourth.ExecutionProfileID)
	}
	// The chain is exhausted: it must not wrap back to a or start a new session.
	_, err = engine.ApplyFailure("session", profile, fourth.Generation, "d", hardSkipFailure())
	if !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("exhausted chain error = %v, want %v", err, ErrNoEligibleCandidate)
	}
	state, ok := engine.State("session")
	if !ok || state.Status != "waiting" {
		t.Fatalf("state = %#v, want a persisted waiting state", state)
	}
	chain, err := decodeSelectionChain(state.PolicyStateJSON)
	if err != nil {
		t.Fatalf("decode chain: %v", err)
	}
	if !chain.valid() || len(chain.TriedCandidateIDs) != 4 {
		t.Fatalf("chain = %#v, want four tried candidates retained", chain)
	}
}

// TestDynamicTierTransitionChainNextTierSkipsPeers covers next-tier-on-failure:
// the remaining peers of the failed tier are skipped entirely.
func TestDynamicTierTransitionChainNextTierSkipsPeers(t *testing.T) {
	engine := NewEngine(WithClock(func() time.Time { return time.Unix(1000, 0) }))
	profile := Profile{
		ID: "tiered-next", Version: 1,
		Candidates: []Candidate{
			tierCandidate("a", TierModeOrder, FailureNextTier, false, CostFree),
			tierCandidate("b", TierModeOrder, FailureNextTier, true, CostFree),
			tierCandidate("c", TierModeOrder, FailureNextTier, false, CostFree),
		},
	}
	first, err := engine.Select("session", profile, 0, "")
	if err != nil || first.ExecutionProfileID != "a" {
		t.Fatalf("first = %#v, err = %v", first, err)
	}
	second, err := engine.ApplyFailure("session", profile, first.Generation, "a", hardSkipFailure())
	if err != nil {
		t.Fatalf("apply failure: %v", err)
	}
	if second.ExecutionProfileID != "c" {
		t.Fatalf("second = %q, want c (peers of the failed tier are skipped)", second.ExecutionProfileID)
	}
}

func TestDynamicTierTransitionChainNeverReturnsToAnEarlierTier(t *testing.T) {
	engine := NewEngine(WithClock(func() time.Time { return time.Unix(1000, 0) }))
	profile := Profile{
		ID: "tiered-back", Version: 1,
		Candidates: []Candidate{
			tierCandidate("a", TierModeOrder, FailureSameTierNext, false, CostFree),
			tierCandidate("b", TierModeOrder, FailureSameTierNext, false, CostFree),
		},
	}
	first, _ := engine.Select("session", profile, 0, "")
	second, err := engine.ApplyFailure("session", profile, first.Generation, "a", hardSkipFailure())
	if err != nil || second.ExecutionProfileID != "b" {
		t.Fatalf("second = %#v, err = %v", second, err)
	}
	// b's tier has no further candidate, and the chain must not wrap to tier 1.
	_, err = engine.ApplyFailure("session", profile, second.Generation, "b", hardSkipFailure())
	if !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("error = %v, want exhaustion rather than a wrap to the earlier tier", err)
	}
}

func TestDynamicTierTransitionChainSurvivesRestart(t *testing.T) {
	store := &recordingPersistence{}
	now := time.Unix(1000, 0)
	profile := Profile{
		ID: "tiered-restart", Version: 1,
		Candidates: []Candidate{
			tierCandidate("a", TierModeOrder, FailureSameTierNext, false, CostFree),
			tierCandidate("b", TierModeOrder, FailureSameTierNext, true, CostFree),
			tierCandidate("c", TierModeOrder, FailureSameTierNext, false, CostFree),
		},
	}
	first := NewEngine(WithClock(func() time.Time { return now }), WithPersistence(store))
	decision, err := first.Select("session", profile, 0, "")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	decision, err = first.ApplyFailure("session", profile, decision.Generation, "a", hardSkipFailure())
	if err != nil {
		t.Fatalf("ApplyFailure: %v", err)
	}
	if decision.ExecutionProfileID != "b" {
		t.Fatalf("decision = %q, want b", decision.ExecutionProfileID)
	}

	// A new process reloads the durable row and must not revisit a.
	second := NewEngine(
		WithClock(func() time.Time { return now }),
		WithPersistence(store),
		WithStateLoader(store),
	)
	after, err := second.ApplyFailure("session", profile, decision.Generation, "b", hardSkipFailure())
	if err != nil {
		t.Fatalf("ApplyFailure after restart: %v", err)
	}
	if after.ExecutionProfileID != "c" {
		t.Fatalf("after restart = %q, want c and never a", after.ExecutionProfileID)
	}
}

func TestDynamicTierTransitionChainPaceRankingPicksLeastUsed(t *testing.T) {
	engine := NewEngine(
		WithClock(func() time.Time { return time.Unix(1000, 0) }),
		WithUsageSnapshotProvider(fixedUsage{
			"a": {Known: true, Pace: 0.9, UsageFraction: 0.36},
			"b": {Known: true, Pace: 0.1, UsageFraction: 0.04},
		}),
	)
	profile := Profile{
		ID: "paced", Version: 1,
		Candidates: []Candidate{
			tierCandidate("a", TierModePace, FailureSameTierNext, false, CostFree),
			tierCandidate("b", TierModePace, FailureSameTierNext, true, CostFree),
		},
	}
	decision, err := engine.Select("session", profile, 0, "")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if decision.ExecutionProfileID != "b" {
		t.Fatalf("decision = %q, want the lower pace", decision.ExecutionProfileID)
	}
}

func TestDynamicTierTransitionChainRanksOnlyTheFirstTierWithACandidate(t *testing.T) {
	engine := NewEngine(
		WithClock(func() time.Time { return time.Unix(1000, 0) }),
		WithUsageSnapshotProvider(fixedUsage{
			"a": {Known: true, Pace: 5.0, UsageFraction: 0.5},
			"b": {Known: true, Pace: 5.0, UsageFraction: 0.5},
			"c": {Known: true, Pace: 0.0, UsageFraction: 0.0},
		}),
	)
	profile := Profile{
		ID: "tier-1-busy", Version: 1,
		Candidates: []Candidate{
			tierCandidate("a", TierModePace, FailureSameTierNext, false, CostFree),
			tierCandidate("b", TierModePace, FailureSameTierNext, false, CostFree),
			tierCandidate("c", TierModePace, FailureSameTierNext, false, CostFree),
		},
	}
	decision, err := engine.Select("session", profile, 0, "")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	// Tiers are always traversed in list order, so the idle third tier must not
	// win over an eligible first tier.
	if decision.ExecutionProfileID != "a" {
		t.Fatalf("decision = %q, want a from the first tier", decision.ExecutionProfileID)
	}
}

func TestDynamicTierTransitionChainStopsBeforeRanking(t *testing.T) {
	engine := NewEngine(WithClock(func() time.Time { return time.Unix(1000, 0) }))
	stopping := tierCandidate("a", TierModeOrder, FailureSameTierNext, false, CostFree)
	stopping.Policies.Hard.OnExhausted = "stop"
	profile := Profile{
		ID: "stopping", Version: 1,
		Candidates: []Candidate{
			stopping,
			tierCandidate("b", TierModeOrder, FailureSameTierNext, false, CostFree),
		},
	}
	first, err := engine.Select("session", profile, 0, "")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	decision, err := engine.ApplyFailure("session", profile, first.Generation, "a", hardSkipFailure())
	// A stop never invokes ranking, so the successor stays the same candidate.
	if err == nil && decision.ExecutionProfileID != "a" {
		t.Fatalf("decision = %q, want the stop to veto any automatic change", decision.ExecutionProfileID)
	}
	if err != nil && !errors.Is(err, ErrNoEligibleCandidate) && !errors.Is(err, ErrRecoveryPending) {
		t.Fatalf("error = %v, want the existing stop outcome", err)
	}
}

func TestDynamicTierTransitionChainIgnoredUsageNeverWinsPace(t *testing.T) {
	engine := NewEngine(
		WithClock(func() time.Time { return time.Unix(1000, 0) }),
		// A failed refresh is unknown usage, not a zero: a must not win by
		// looking idle.
		WithUsageSnapshotProvider(failingUsage{}),
	)
	profile := Profile{
		ID: "unknown-usage", Version: 1,
		Candidates: []Candidate{
			tierCandidate("a", TierModePace, FailureSameTierNext, false, CostFree),
			tierCandidate("b", TierModePace, FailureSameTierNext, true, CostFree),
		},
	}
	decision, err := engine.Select("session", profile, 0, "")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if decision.ExecutionProfileID != "a" {
		t.Fatalf("decision = %q, want saved row order for unknown usage", decision.ExecutionProfileID)
	}
}

func TestDynamicTierSelectionChainIsNotResetByPolicyCounters(t *testing.T) {
	now := time.Unix(1000, 0)
	profile := Profile{
		ID: "counters", Version: 1,
		Candidates: []Candidate{
			tierCandidate("a", TierModeOrder, FailureSameTierNext, false, CostFree),
			tierCandidate("b", TierModeOrder, FailureSameTierNext, false, CostFree),
		},
	}
	engine := NewEngine(WithClock(func() time.Time { return now }))
	first, _ := engine.Select("session", profile, 0, "")
	if _, err := engine.ApplyFailure("session", profile, first.Generation, "a", hardSkipFailure()); err != nil {
		t.Fatalf("ApplyFailure: %v", err)
	}
	state, _ := engine.State("session")
	chain, err := decodeSelectionChain(state.PolicyStateJSON)
	if err != nil {
		t.Fatalf("decode chain: %v", err)
	}
	if len(chain.TriedCandidateIDs) != 2 {
		t.Fatalf("chain = %#v, want both candidates recorded", chain)
	}
	if chain.LastTierHeadID != "a" && chain.LastTierHeadID != "b" {
		t.Fatalf("last tier head = %q, want a concrete candidate identity", chain.LastTierHeadID)
	}
}

type fixedUsage map[string]PaceScore

func (f fixedUsage) UsageSnapshot(context.Context, Profile) (map[string]PaceScore, error) {
	return f, nil
}

type failingUsage struct{}

func (failingUsage) UsageSnapshot(context.Context, Profile) (map[string]PaceScore, error) {
	return nil, errors.New("usage refresh failed")
}

// recordingPersistence is the minimal durable seam: a generation CAS plus the
// row read a restarted process would perform.
type recordingPersistence struct {
	mu       sync.Mutex
	row      RouteState
	attempts []RouteAttempt
}

func (p *recordingPersistence) SaveRouteState(_ context.Context, state RouteState) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.row = state
	return nil
}

func (p *recordingPersistence) AppendRouteAttempt(_ context.Context, attempt RouteAttempt) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.attempts = append(p.attempts, attempt)
	return nil
}

func (p *recordingPersistence) ClaimRouteState(_ context.Context, expected int64, state RouteState) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.row.Generation != expected {
		return false, nil
	}
	p.row = state
	return true, nil
}

func (p *recordingPersistence) LoadRouteState(_ context.Context, sessionID string) (*RouteState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.row.SessionID != sessionID || p.row.Generation == 0 {
		return nil, nil
	}
	row := p.row
	return &row, nil
}
