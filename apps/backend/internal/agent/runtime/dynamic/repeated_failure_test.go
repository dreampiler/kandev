package dynamic

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/agent/runtime/routingpolicy"
)

func unclassifiedRuntimeFailure() *routingerr.Error {
	return &routingerr.Error{
		Code:             routingerr.CodeAgentRuntime,
		Class:            routingerr.ClassUnclassified,
		CatalogueVersion: routingerr.CatalogueVersion,
		FallbackAllowed:  false,
	}
}

func repeatedFailureDocument(threshold int64) routingpolicy.Document {
	document := routingpolicy.DefaultDocument()
	document.Unclassified = routingpolicy.Policy{
		OnExhausted:     routingpolicy.OutcomeStop,
		RepeatedFailure: routingpolicy.RepeatedFailurePolicy{Enabled: true, Threshold: threshold},
	}
	return document
}

func repeatedFailureProfile(document routingpolicy.Document) Profile {
	return Profile{ID: "repeated", Version: 1, Candidates: []Candidate{
		{ID: "first", Enabled: true, Policies: document},
		{ID: "second", Enabled: true},
	}}
}

func TestEngineRepeatedUnclassifiedFailureFallsBackAtThreshold(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine()
	profile := repeatedFailureProfile(repeatedFailureDocument(2))

	initial, err := engine.Select("repeated-session", profile, 0, "")
	if err != nil {
		t.Fatalf("initial Select: %v", err)
	}
	// First unclassified failure stays below the threshold and stops the
	// route, keeping the same generation for the next attempt.
	if _, err := engine.ApplyFailureContextWithEffect(
		ctx, "repeated-session", profile, initial.Generation, initial.ExecutionProfileID,
		unclassifiedRuntimeFailure(), true,
	); !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("first failure error = %v, want no eligible candidate", err)
	}
	state, ok := engine.State("repeated-session")
	if !ok || state.Status != "action_required" {
		t.Fatalf("state after first failure = %#v, want action_required", state)
	}

	// The second failure on the same profile reaches the threshold and moves
	// to the next candidate.
	next, err := engine.ApplyFailureContextWithEffect(
		ctx, "repeated-session", profile, state.Generation, "first",
		unclassifiedRuntimeFailure(), true,
	)
	if err != nil {
		t.Fatalf("second failure: %v", err)
	}
	if next.ExecutionProfileID != "second" || next.Reason != "policy_skip" {
		t.Fatalf("fallback decision = %#v, want second via policy_skip", next)
	}
	if next.Generation <= state.Generation {
		t.Fatalf("generation did not advance: %d -> %d", state.Generation, next.Generation)
	}
}

func TestEngineRepeatedUnclassifiedStaysStoppedBelowThreshold(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine()
	profile := repeatedFailureProfile(repeatedFailureDocument(3))

	initial, err := engine.Select("below-session", profile, 0, "")
	if err != nil {
		t.Fatalf("initial Select: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := engine.ApplyFailureContextWithEffect(
			ctx, "below-session", profile, initial.Generation, "first",
			unclassifiedRuntimeFailure(), true,
		); !errors.Is(err, ErrNoEligibleCandidate) {
			t.Fatalf("attempt %d error = %v, want no eligible candidate", attempt, err)
		}
	}
}

func TestEngineRepeatedUnclassifiedDisabledPolicyStops(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine()
	// Default document has no repeated-failure opt-in.
	profile := repeatedFailureProfile(routingpolicy.DefaultDocument())

	initial, err := engine.Select("disabled-session", profile, 0, "")
	if err != nil {
		t.Fatalf("initial Select: %v", err)
	}
	for attempt := 0; attempt < 5; attempt++ {
		if _, err := engine.ApplyFailureContextWithEffect(
			ctx, "disabled-session", profile, initial.Generation, "first",
			unclassifiedRuntimeFailure(), true,
		); !errors.Is(err, ErrNoEligibleCandidate) {
			t.Fatalf("attempt %d error = %v, want no eligible candidate", attempt, err)
		}
	}
}

func TestEngineRepeatedUnclassifiedUnsafeEffectDoesNotCount(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine()
	profile := repeatedFailureProfile(repeatedFailureDocument(1))

	initial, err := engine.Select("unsafe-session", profile, 0, "")
	if err != nil {
		t.Fatalf("initial Select: %v", err)
	}
	// threshold=1 would normally fall back immediately, but an effect-unsafe
	// failure must never satisfy the override.
	if _, err := engine.ApplyFailureContextWithEffect(
		ctx, "unsafe-session", profile, initial.Generation, initial.ExecutionProfileID,
		unclassifiedRuntimeFailure(), false,
	); !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("unsafe failure error = %v, want no eligible candidate", err)
	}
}

func TestEngineSuccessfulLaunchResetsRepeatedFailureStreak(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine()
	profile := repeatedFailureProfile(repeatedFailureDocument(2))

	initial, err := engine.Select("reset-session", profile, 0, "")
	if err != nil {
		t.Fatalf("initial Select: %v", err)
	}
	if _, err := engine.ApplyFailureContextWithEffect(
		ctx, "reset-session", profile, initial.Generation, initial.ExecutionProfileID,
		unclassifiedRuntimeFailure(), true,
	); !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("first failure error = %v, want no eligible candidate", err)
	}
	// Manual recovery moves the same generation to retrying, then a successful
	// launch clears the streak.
	if _, err := engine.ResumePendingNow(ctx, "reset-session", initial.Generation); err != nil {
		t.Fatalf("ResumePendingNow: %v", err)
	}
	if err := engine.MarkActive(ctx, "reset-session", initial.Generation); err != nil {
		t.Fatalf("MarkActive: %v", err)
	}
	// Were the streak not cleared, this second failure would reach the
	// threshold and fall back; it must instead stop below threshold.
	if _, err := engine.ApplyFailureContextWithEffect(
		ctx, "reset-session", profile, initial.Generation, "first",
		unclassifiedRuntimeFailure(), true,
	); !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("post-reset failure error = %v, want no eligible candidate", err)
	}
}

func TestNextConsecutiveFailuresTracking(t *testing.T) {
	unclassified := routingerr.ClassUnclassified
	tests := []struct {
		name  string
		state PolicyState
		class routingerr.Class
		want  int64
	}{
		{
			name:  "first unclassified failure starts the streak",
			state: PolicyState{},
			class: unclassified, want: 1,
		},
		{
			name:  "same profile unclassified failure extends the streak",
			state: PolicyState{LastExecutionProfileID: "first", ConsecutiveFailures: 2},
			class: unclassified, want: 3,
		},
		{
			name:  "different profile unclassified failure restarts the streak",
			state: PolicyState{LastExecutionProfileID: "other", ConsecutiveFailures: 4},
			class: unclassified, want: 1,
		},
		{
			name:  "non-unclassified failure clears the streak",
			state: PolicyState{LastExecutionProfileID: "first", ConsecutiveFailures: 4},
			class: routingerr.ClassTransient, want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextConsecutiveFailures(tt.state, "first", tt.class); got != tt.want {
				t.Fatalf("nextConsecutiveFailures = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestClearRepeatedFailureStreak(t *testing.T) {
	raw := string(mustJSON(PolicyState{LastExecutionProfileID: "first", ConsecutiveFailures: 3}))
	cleared := clearRepeatedFailureStreak(raw)
	var state PolicyState
	if err := json.Unmarshal([]byte(cleared), &state); err != nil {
		t.Fatalf("decode cleared state: %v", err)
	}
	if state.ConsecutiveFailures != 0 || state.LastExecutionProfileID != "" {
		t.Fatalf("streak not cleared: %#v", state)
	}
	if clearRepeatedFailureStreak("") != "" {
		t.Fatal("empty policy state must stay empty")
	}
}
