package dynamic

import (
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingpolicy"
)

func TestRouteReasonIsABoundedCodeSet(t *testing.T) {
	tests := []struct {
		name     string
		policy   TierPolicy
		fallback bool
		want     string
		wantOK   bool
	}{
		{
			name:   "fresh ordered selection",
			policy: TierPolicy{Mode: TierModeOrder, OnFailure: FailureSameTierNext},
			want:   ReasonTierOrder, wantOK: true,
		},
		{
			name:   "fresh pace selection",
			policy: TierPolicy{Mode: TierModePace, OnFailure: FailureSameTierNext},
			want:   ReasonTierPace, wantOK: true,
		},
		{
			name:   "fresh cost selection",
			policy: TierPolicy{Mode: TierModeCost, OnFailure: FailureNextTier},
			want:   ReasonTierCost, wantOK: true,
		},
		{
			name:     "fallback within the same tier",
			policy:   TierPolicy{Mode: TierModePace, OnFailure: FailureSameTierNext},
			fallback: true,
			want:     ReasonTierPace + ReasonSuffixSameTier, wantOK: true,
		},
		{
			name:     "fallback to the next tier",
			policy:   TierPolicy{Mode: TierModeCost, OnFailure: FailureNextTier},
			fallback: true,
			want:     ReasonTierCost + ReasonSuffixNextTier, wantOK: true,
		},
		{
			name:   "unconfigured mode leaves the caller's code alone",
			policy: TierPolicy{OnFailure: FailureSameTierNext},
			wantOK: false,
		},
		{
			name:   "unknown mode is not passed through",
			policy: TierPolicy{Mode: "cheapest", OnFailure: FailureSameTierNext},
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, ok := RouteReason(tt.policy, tt.fallback)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && code != tt.want {
				t.Fatalf("code = %q, want %q", code, tt.want)
			}
		})
	}
}

func TestPersistedReasonRecordsTheSelectionRuleAndDirection(t *testing.T) {
	engine := NewEngine(WithClock(func() time.Time { return time.Unix(1000, 0) }))
	profile := Profile{
		ID: "reasoned", Version: 1,
		Candidates: []Candidate{
			tierCandidate("a", TierModePace, FailureNextTier, false, CostFree),
			tierCandidate("b", TierModeOrder, FailureSameTierNext, false, CostFree),
		},
	}
	first, err := engine.Select("session", profile, 0, "")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if first.Reason != ReasonTierPace {
		t.Fatalf("reason = %q, want the fresh pace code", first.Reason)
	}
	// next_tier_on_failure skips the peers of the first tier, so the successor
	// comes from the following tier and records the direction suffix.
	second, err := engine.ApplyFailure("session", profile, first.Generation, "a", hardSkipFailure())
	if err != nil {
		t.Fatalf("ApplyFailure: %v", err)
	}
	if second.Reason != ReasonTierOrder+ReasonSuffixNextTier {
		t.Fatalf("reason = %q, want the next-tier suffix on the successor's own rule", second.Reason)
	}
}

// TestPersistedReasonPreservesLegacyCodes pins that a row without stored tier
// metadata keeps the established reason codes existing consumers already read.
func TestPersistedReasonPreservesLegacyCodes(t *testing.T) {
	engine := NewEngine(WithClock(func() time.Time { return time.Unix(1000, 0) }))
	legacy := Candidate{ID: "legacy", Enabled: true, BindingKey: "binding:legacy", Policies: routingpolicy.DefaultDocument()}
	profile := Profile{ID: "legacy-profile", Version: 1, Candidates: []Candidate{legacy}}
	decision, err := engine.Select("session", profile, 0, "")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if decision.Reason != "candidate_order" {
		t.Fatalf("reason = %q, want the established legacy code", decision.Reason)
	}
}
