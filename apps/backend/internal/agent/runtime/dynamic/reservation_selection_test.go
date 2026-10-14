package dynamic

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

type reservationSelectionCase struct {
	name     string
	scores   map[string]PaceScore
	reserve  int
	disabled bool
	blocked  bool
	want     string
}

// @covers AC-AGENTS-TIER-SELECTION-003.2, AC-AGENTS-TIER-SELECTION-003.5
func TestReservationSelectionKeepsUnknownCandidatesAndExistingExclusions(t *testing.T) {
	tests := []reservationSelectionCase{
		{name: "entire first tier unknown", reserve: 2, want: "opus"},
		{name: "known peer wins over unknown", reserve: 2, want: "sol", scores: map[string]PaceScore{
			"sol": {Known: true, Pace: 0.8, UsageFraction: 0.4},
		}},
		{name: "known reserved row leaves unknown peer", reserve: 2, want: "sol", scores: map[string]PaceScore{
			"opus": {Known: true, Pace: 0.1, UsageFraction: 0.98},
		}},
		{name: "known threshold still excludes both", reserve: 2, want: "kimi", scores: map[string]PaceScore{
			"opus": {Known: true, UsageFraction: 0.98}, "sol": {Known: true, UsageFraction: 1},
		}},
		{name: "full reserve excludes unknown", reserve: 100, want: "kimi"},
		{name: "disabled unknown row stays excluded", reserve: 2, disabled: true, want: "sol"},
		{name: "actual circuits exclude unknown tier", reserve: 2, blocked: true, want: "kimi"},
		{name: "zero reserve adds no gate", reserve: 0, want: "opus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkReservationSelection(t, tt) })
	}
}

func checkReservationSelection(t *testing.T, tt reservationSelectionCase) {
	t.Helper()
	now := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	profile := Profile{ID: "reservation", Version: 1, Candidates: []Candidate{
		tierCandidate("opus", TierModePace, FailureSameTierNext, false, CostSubscription),
		tierCandidate("sol", TierModePace, FailureSameTierNext, true, CostSubscription),
		tierCandidate("kimi", TierModeOrder, FailureSameTierNext, false, CostFree),
	}}
	circuits := NewCircuitRegistry(WithCircuitClock(func() time.Time { return now }))
	ineligible := make(map[string]string)
	for i := 0; i < 2; i++ {
		profile.Candidates[i].Selection.Model.ReservedUserSharePct = tt.reserve
		if tt.blocked {
			circuits.Open(profile.Candidates[i].BindingKey, now.Add(time.Hour), routingerr.CodeQuotaLimited)
			ineligible[profile.Candidates[i].ID] = IneligibleCircuit
		}
	}
	if tt.disabled {
		profile.Candidates[0].Enabled = false
		ineligible["opus"] = IneligibleDisabled
	}
	pick := func(int) int { return 0 }
	engine := NewEngine(WithClock(func() time.Time { return now }), WithRandomPick(pick),
		WithUsageSnapshotProvider(fixedUsage(tt.scores)), WithCircuitRegistry(circuits))
	decision, err := engine.SelectContext(context.Background(), "session", profile, 0, "")
	if err != nil || decision.ExecutionProfileID != tt.want {
		t.Fatalf("SelectContext = %q, %v; want %q", decision.ExecutionProfileID, err, tt.want)
	}
	preview := PreviewSelectionWith(profile, RankOptions{Scores: tt.scores, Pick: pick},
		ineligible, "", SelectionChain{}, now)
	if preview.CandidateID != decision.ExecutionProfileID {
		t.Fatalf("preview = %q, live = %q; want agreement", preview.CandidateID, decision.ExecutionProfileID)
	}
	if tt.want != "kimi" && preview.TierIndex != 1 {
		t.Fatalf("selected tier = %d, want the first tier", preview.TierIndex)
	}
}
