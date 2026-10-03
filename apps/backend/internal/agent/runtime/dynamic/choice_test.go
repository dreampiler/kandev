package dynamic

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

func tierOf(mode TierMode, ids ...string) Tier {
	candidates := make([]Candidate, 0, len(ids))
	for index, id := range ids {
		candidates = append(candidates, tierCandidate(id, mode, FailureSameTierNext, index > 0, CostFree))
	}
	return DeriveTiers(candidates)[0]
}

func firstID(t *testing.T, ranked []RankCandidate) string {
	t.Helper()
	winner, ok := FirstEligible(ranked)
	if !ok {
		t.Fatal("no eligible candidate")
	}
	return winner.Candidate.ID
}

func TestRandomTierPicksAmongEligibleCandidates(t *testing.T) {
	tier := tierOf(TierModeRandom, "a", "b", "c")
	options := RankOptions{
		Eligible: map[string]string{"b": IneligibleCircuit},
		Pick:     func(n int) int { return n - 1 },
	}
	if got := firstID(t, RankTier(tier, options)); got != "c" {
		t.Fatalf("winner = %q, want the drawn eligible candidate", got)
	}
	options.Pick = func(int) int { return 0 }
	if got := firstID(t, RankTier(tier, options)); got != "a" {
		t.Fatalf("winner = %q, want the first draw", got)
	}
}

func TestRoundRobinTierContinuesAfterTheLastChoiceAndSkipsBlocked(t *testing.T) {
	tier := tierOf(TierModeRoundRobin, "a", "b", "c")
	cases := []struct {
		last    string
		blocked string
		want    string
	}{
		{"", "", "a"},
		{"a", "", "b"},
		{"b", "", "c"},
		{"c", "", "a"},
		{"a", "b", "c"},
		{"c", "a", "b"},
		{"removed", "", "a"},
	}
	for _, tc := range cases {
		eligible := map[string]string{}
		if tc.blocked != "" {
			eligible[tc.blocked] = IneligibleCircuit
		}
		options := RankOptions{Eligible: eligible, LastPicked: map[string]string{tier.HeadID: tc.last}}
		if got := firstID(t, RankTier(tier, options)); got != tc.want {
			t.Fatalf("last=%q blocked=%q: winner = %q, want %q", tc.last, tc.blocked, got, tc.want)
		}
	}
}

func TestPaceRanksCapacityThenUnknownByRecordedUsageThenExhausted(t *testing.T) {
	tier := tierOf(TierModePace, "exhausted", "busy-unknown", "idle-unknown", "known")
	scores := map[string]PaceScore{
		"exhausted":    {Known: true, UsageFraction: 1, Pace: 20},
		"busy-unknown": {Internal: InternalUsage{Known: true, Turns: 40}},
		"idle-unknown": {Internal: InternalUsage{Known: true, Turns: 3}},
		"known":        {Known: true, UsageFraction: 0.9, Pace: 1.4},
	}
	ranked := RankTier(tier, RankOptions{Scores: scores})
	order := make([]string, 0, len(ranked))
	for _, entry := range ranked {
		order = append(order, entry.Candidate.ID)
	}
	want := []string{"known", "idle-unknown", "busy-unknown", "exhausted"}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestPaceWithNoUsageAnywhereDrawsAmongTiedPeers(t *testing.T) {
	tier := tierOf(TierModePace, "a", "b", "c")
	equal := InternalUsage{Known: true, Turns: 5}
	scores := map[string]PaceScore{"a": {Internal: equal}, "b": {Internal: equal}, "c": {Internal: equal}}
	ranked := RankTier(tier, RankOptions{Scores: scores, Pick: func(n int) int { return 1 }})
	if got := firstID(t, ranked); got != "b" {
		t.Fatalf("winner = %q, want the draw among three tied unknown candidates", got)
	}
	// A known reading is never displaced by the draw.
	scores["c"] = PaceScore{Known: true, UsageFraction: 0.1, Pace: 0.2}
	ranked = RankTier(tier, RankOptions{Scores: scores, Pick: func(n int) int { return n - 1 }})
	if got := firstID(t, ranked); got != "c" {
		t.Fatalf("winner = %q, want the known candidate with capacity", got)
	}
}

type stubHistory map[string]time.Time

func (h stubHistory) LastSelections(context.Context, string) (map[string]time.Time, error) {
	return h, nil
}

func roundRobinProfile() Profile {
	return Profile{ID: "rr", Version: 1, Candidates: []Candidate{
		tierCandidate("a", TierModeRoundRobin, FailureSameTierNext, false, CostFree),
		tierCandidate("b", TierModeRoundRobin, FailureSameTierNext, true, CostFree),
		tierCandidate("c", TierModeRoundRobin, FailureSameTierNext, true, CostFree),
	}}
}

// TestRoundRobinContinuesFromDurableHistoryAfterRestart pins that a new engine
// (a restarted process) resumes the rotation from the stored attempt log and
// then keeps rotating from its own choices.
func TestRoundRobinContinuesFromDurableHistoryAfterRestart(t *testing.T) {
	now := time.Unix(5000, 0)
	history := stubHistory{"a": now.Add(-time.Hour), "b": now.Add(-time.Minute)}
	engine := NewEngine(WithClock(func() time.Time { return now }), WithSelectionHistory(history))
	profile := roundRobinProfile()

	first, err := engine.Select("session-1", profile, 0, "")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if first.ExecutionProfileID != "c" {
		t.Fatalf("first = %q, want the candidate after the stored last choice", first.ExecutionProfileID)
	}
	now = now.Add(time.Second)
	second, err := engine.Select("session-2", profile, 0, "")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if second.ExecutionProfileID != "a" {
		t.Fatalf("second = %q, want the rotation to wrap after this process's own choice", second.ExecutionProfileID)
	}
}

type recordingLimits struct {
	calls []string
}

func (r *recordingLimits) ObserveLimit(_ context.Context, profileID string, code routingerr.Code, _ time.Time) {
	r.calls = append(r.calls, profileID+":"+string(code))
}

func TestLimitHitsAreObservedWithoutChangingRouting(t *testing.T) {
	limits := &recordingLimits{}
	engine := NewEngine(WithClock(func() time.Time { return time.Unix(1000, 0) }), WithLimitObserver(limits))
	profile := roundRobinProfile()
	first, err := engine.Select("session", profile, 0, "")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if _, err := engine.ApplyFailure("session", profile, first.Generation, first.ExecutionProfileID, hardSkipFailure()); err != nil {
		t.Fatalf("ApplyFailure: %v", err)
	}
	if len(limits.calls) != 1 || limits.calls[0] != first.ExecutionProfileID+":"+string(routingerr.CodeQuotaLimited) {
		t.Fatalf("observations = %v, want the quota hit of the failing candidate", limits.calls)
	}
}

func TestRandomAndRoundRobinRouteReasons(t *testing.T) {
	cases := map[TierMode]string{TierModeRandom: ReasonTierRandom, TierModeRoundRobin: ReasonTierRoundRobin}
	for mode, want := range cases {
		reason, ok := RouteReason(TierPolicy{Mode: mode, OnFailure: FailureNextTier}, false)
		if !ok || reason != want {
			t.Fatalf("%s reason = %q, want %q", mode, reason, want)
		}
		fallback, _ := RouteReason(TierPolicy{Mode: mode, OnFailure: FailureNextTier}, true)
		if fallback != want+ReasonSuffixNextTier {
			t.Fatalf("%s fallback reason = %q", mode, fallback)
		}
	}
}
