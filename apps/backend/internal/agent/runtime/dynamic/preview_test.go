package dynamic

import (
	"testing"
	"time"
)

func previewCandidate(id string, mode TierMode, join bool, cost CostClass) Candidate {
	candidate := Candidate{
		ID: id, Enabled: true, BindingKey: "binding:" + id,
		Selection: Selection{
			JoinPrevious: join,
			Model:        ModelOptions{Cost: cost, UsageSource: UsageAutomatic},
		},
	}
	if !join {
		candidate.Selection.Tier = &TierPolicy{Mode: mode, OnFailure: FailureSameTierNext}
	}
	return candidate
}

func TestPreviewSelectionMatchesTheEngineForTheSameInputs(t *testing.T) {
	now := time.Unix(1000, 0)
	scores := map[string]PaceScore{
		"a": {Known: true, Complete: true, Pace: 0.9, UsageFraction: 0.36, ElapsedFraction: 0.4},
		"b": {Known: true, Complete: true, Pace: 0.1, UsageFraction: 0.04, ElapsedFraction: 0.4},
		"c": {Known: true, Complete: true, Pace: 0.2, UsageFraction: 0.08, ElapsedFraction: 0.4},
	}
	profile := Profile{
		ID: "preview-parity", Version: 1,
		Candidates: []Candidate{
			previewCandidate("a", TierModePace, false, CostFree),
			previewCandidate("b", TierModePace, true, CostFree),
			previewCandidate("c", TierModePace, true, CostFree),
		},
	}
	engine := NewEngine(
		WithClock(func() time.Time { return now }),
		WithUsageSnapshotProvider(fixedUsage(scores)),
	)
	decision, err := engine.Select("session", profile, 0, "")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	preview := PreviewSelection(profile, scores, nil, "", SelectionChain{}, now)

	if preview.State() != PreviewReady {
		t.Fatalf("state = %q, want ready", preview.State())
	}
	// The preview must name the same concrete candidate the engine selected.
	if preview.CandidateID != decision.ExecutionProfileID {
		t.Fatalf("preview = %q, engine = %q, want agreement", preview.CandidateID, decision.ExecutionProfileID)
	}
	if preview.Reason != decision.Reason {
		t.Fatalf("preview reason = %q, engine reason = %q, want agreement", preview.Reason, decision.Reason)
	}
	if preview.TierIndex != 1 || preview.TierHeadID != "a" {
		t.Fatalf("preview tier = %d/%q, want tier 1 headed by a", preview.TierIndex, preview.TierHeadID)
	}
	if !preview.Score.Known || preview.Score.Pace != scores["b"].Pace {
		t.Fatalf("preview score = %#v, want the controlling evidence", preview.Score)
	}
}

func TestPreviewSelectionDistinguishesTheMaterialStates(t *testing.T) {
	now := time.Unix(1000, 0)
	tests := []struct {
		name       string
		profile    Profile
		ineligible map[string]string
		want       PreviewState
	}{
		{
			name:    "no candidates",
			profile: Profile{ID: "empty"},
			want:    PreviewNoCandidates,
		},
		{
			name: "no eligible candidate",
			profile: Profile{ID: "blocked", Candidates: []Candidate{
				previewCandidate("a", TierModePace, false, CostFree),
			}},
			ineligible: map[string]string{"a": IneligibleCircuit},
			want:       PreviewNoEligible,
		},
		{
			name: "ready",
			profile: Profile{ID: "ready", Candidates: []Candidate{
				previewCandidate("a", TierModePace, false, CostFree),
			}},
			want: PreviewReady,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			preview := PreviewSelection(tt.profile, nil, tt.ineligible, "", SelectionChain{}, now)
			if preview.State() != tt.want {
				t.Fatalf("state = %q, want %q", preview.State(), tt.want)
			}
		})
	}
	// A disabled row is filtered before ranking, so a lone disabled candidate
	// reports no eligible candidate rather than an error.
	disabled := Profile{ID: "disabled", Candidates: []Candidate{
		{ID: "a", Enabled: false, Selection: Selection{Tier: &TierPolicy{Mode: TierModePace}}},
	}}
	if state := PreviewSelection(disabled, nil, map[string]string{"a": IneligibleDisabled}, "", SelectionChain{}, now).State(); state != PreviewNoEligible {
		t.Fatalf("state = %q, want no eligible candidate", state)
	}
}

func TestPreviewSelectionExplainsEveryConsideredCandidate(t *testing.T) {
	now := time.Unix(1000, 0)
	profile := Profile{
		ID: "explained", Version: 1,
		Candidates: []Candidate{
			previewCandidate("a", TierModeCost, false, CostMetered),
			previewCandidate("b", TierModeCost, true, CostSubscription),
			previewCandidate("c", TierModeCost, true, CostFree),
		},
	}
	preview := PreviewSelection(profile, nil, nil, "", SelectionChain{}, now)
	if preview.CandidateID != "c" {
		t.Fatalf("candidate = %q, want the free row under cost ordering", preview.CandidateID)
	}
	if len(preview.Considered) != 3 {
		t.Fatalf("considered = %d, want every ranked candidate", len(preview.Considered))
	}
	selected := 0
	for _, entry := range preview.Considered {
		if entry.Selected {
			selected++
			if entry.CandidateID != "c" {
				t.Fatalf("selected entry = %q, want c", entry.CandidateID)
			}
		}
	}
	if selected != 1 {
		t.Fatalf("selected entries = %d, want exactly one", selected)
	}
	// The preview must explain why a filtered candidate lost.
	blocked := PreviewSelection(profile, nil, map[string]string{"c": IneligibleCircuit}, "", SelectionChain{}, now)
	if blocked.CandidateID != "b" {
		t.Fatalf("candidate = %q, want the subscription row once c is filtered", blocked.CandidateID)
	}
	for _, entry := range blocked.Considered {
		if entry.CandidateID == "c" && entry.IneligibleReason != IneligibleCircuit {
			t.Fatalf("reason for c = %q, want the circuit code", entry.IneligibleReason)
		}
	}
}

// TestPreviewSelectionIsReadOnly is the load-bearing safety property: a preview
// must not create durable route state or take a probe lease.
func TestPreviewSelectionIsReadOnly(t *testing.T) {
	now := time.Unix(1000, 0)
	store := &recordingPersistence{}
	engine := NewEngine(
		WithClock(func() time.Time { return now }),
		WithPersistence(store),
		WithUsageSnapshotProvider(fixedUsage(map[string]PaceScore{
			"a": {Known: true, Complete: true, Pace: 0.1},
		})),
	)
	profile := Profile{
		ID: "read-only", Version: 1,
		Candidates: []Candidate{previewCandidate("a", TierModePace, false, CostFree)},
	}
	// The same profile, but the circuit is open so a live selection would need a
	// probe lease.
	engine.Circuits().Open(profile.Candidates[0].BindingKey, now.Add(time.Hour), "provider_unavailable")

	preview := PreviewSelection(profile, map[string]PaceScore{
		"a": {Known: true, Complete: true, Pace: 0.1},
	}, nil, "", SelectionChain{}, now)
	if preview.CandidateID != "a" {
		t.Fatalf("candidate = %q, want a", preview.CandidateID)
	}
	if len(store.attempts) != 0 {
		t.Fatalf("attempts = %d, want a preview to record none", len(store.attempts))
	}
	if store.row.Generation != 0 {
		t.Fatalf("generation = %d, want a preview to claim none", store.row.Generation)
	}
	if _, ok := engine.State("preview-session"); ok {
		t.Fatal("preview created in-memory route state")
	}
}
