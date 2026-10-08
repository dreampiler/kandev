package dynamic

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/agent/runtime/routingpolicy"
)

// reasoningContentDiagnostic mirrors the safe, bounded diagnostic a DeepSeek
// thinking-mode `reasoning_content` 400 produces after sanitization.
const reasoningContentDiagnostic = "Internal error: reasoning_content in the thinking mode must be passed back to the API"

func nonReplayablePostStartEvidence(generation int64, attemptID string) UnclassifiedFailureEvidence {
	return UnclassifiedFailureEvidence{
		TaskScope: true, TaskID: "task-1", SessionID: "session-unclassified",
		LogicalProfileID: "dynamic-unclassified", ExecutionProfileID: "candidate-a",
		RouteGeneration: generation, StepID: "step-1", StepKnown: true,
		AttemptID: attemptID, Origin: UnclassifiedOriginPostStartNoResult,
		Phase: routingerr.PhaseStreaming, ProviderID: "opencode-acp",
		DiagnosticText: reasoningContentDiagnostic, DiagnosticComplete: false,
		CurrentAttempt: true, EvidenceKnown: true,
		NonReplayable: true, OutputObserved: true, EffectObserved: true,
	}
}

func reasoningContentFailure() *routingerr.Error {
	return &routingerr.Error{
		Code:           routingerr.CodeAgentRuntime,
		Class:          routingerr.ClassUnclassified,
		Phase:          routingerr.PhaseStreaming,
		ClassifierRule: "deepseek.reasoning_content.no_result.v1",
	}
}

func nonReplayableProfile() Profile {
	document := routingpolicy.DefaultDocument()
	document.Unclassified = &routingpolicy.UnclassifiedPolicy{
		Enabled:                     true,
		ConsecutiveFailureThreshold: 2,
	}
	return Profile{
		ID:      "dynamic-unclassified",
		Version: 4,
		Candidates: []Candidate{
			{ID: "candidate-a", Enabled: true, Policies: document},
			{ID: "candidate-b", Enabled: true, Policies: document},
		},
	}
}

func retrySameCandidate(t *testing.T, engine *Engine, profile Profile, decision RouteDecision) RouteDecision {
	t.Helper()
	retried, err := engine.SelectContextWithPreference(
		context.Background(), "session-unclassified", profile,
		decision.Generation, "", "candidate-a",
	)
	if err != nil {
		t.Fatalf("manual retry: %v", err)
	}
	if err := engine.MarkActive(context.Background(), "session-unclassified", retried.Generation); err != nil {
		t.Fatalf("mark active after retry: %v", err)
	}
	return retried
}

// A recognized non-replayable session-state failure (lost reasoning state) must
// still be admitted after the turn produced output, because its successor is a
// fresh session that discards that output. Without the marker the same evidence
// is rejected as post-result output.
func TestUnclassifiedNonReplayableAdmittedWithObservedOutput(t *testing.T) {
	profile := nonReplayableProfile()
	engine := NewEngine()
	decision, err := engine.Select("session-unclassified", profile, 0, "")
	if err != nil {
		t.Fatalf("initial select: %v", err)
	}
	if err := engine.MarkActive(context.Background(), "session-unclassified", decision.Generation); err != nil {
		t.Fatalf("mark active: %v", err)
	}
	failure := reasoningContentFailure()

	evidence := nonReplayablePostStartEvidence(decision.Generation, "prompt-1")
	if _, err := engine.ApplyUnclassifiedFailureContext(
		context.Background(), "session-unclassified", profile, decision.Generation, "candidate-a", failure, evidence,
	); !errors.Is(err, ErrRecoveryPending) {
		t.Fatalf("first failure error = %v, want manual recovery below threshold", err)
	}
	state, _ := engine.State("session-unclassified")
	var policyState PolicyState
	if err := json.Unmarshal([]byte(state.PolicyStateJSON), &policyState); err != nil {
		t.Fatalf("decode policy state: %v", err)
	}
	if policyState.Unclassified == nil || policyState.Unclassified.Count != 1 {
		t.Fatalf("non-replayable streak = %+v, want count one despite observed output", policyState.Unclassified)
	}

	decision = retrySameCandidate(t, engine, profile, decision)
	second := nonReplayablePostStartEvidence(decision.Generation, "prompt-2")
	advanced, err := engine.ApplyUnclassifiedFailureContext(
		context.Background(), "session-unclassified", profile, decision.Generation, "candidate-a", failure, second,
	)
	if err != nil {
		t.Fatalf("threshold attempt error = %v, want successor", err)
	}
	if advanced.ExecutionProfileID != "candidate-b" {
		t.Fatalf("threshold decision = %+v, want candidate-b", advanced)
	}
}

// Without the non-replayable marker the same observed-output evidence stays
// ineligible, preserving the existing post-result safety boundary.
func TestUnclassifiedObservedOutputRejectedWithoutNonReplayable(t *testing.T) {
	profile := nonReplayableProfile()
	engine := NewEngine()
	decision, err := engine.Select("session-unclassified", profile, 0, "")
	if err != nil {
		t.Fatalf("initial select: %v", err)
	}
	if err := engine.MarkActive(context.Background(), "session-unclassified", decision.Generation); err != nil {
		t.Fatalf("mark active: %v", err)
	}
	evidence := nonReplayablePostStartEvidence(decision.Generation, "prompt-1")
	evidence.NonReplayable = false
	if _, err := engine.ApplyUnclassifiedFailureContext(
		context.Background(), "session-unclassified", profile, decision.Generation, "candidate-a", reasoningContentFailure(), evidence,
	); !errors.Is(err, ErrRecoveryPending) {
		t.Fatalf("unmarked failure error = %v, want manual recovery", err)
	}
	state, _ := engine.State("session-unclassified")
	var policyState PolicyState
	if state.PolicyStateJSON != "" {
		if err := json.Unmarshal([]byte(state.PolicyStateJSON), &policyState); err != nil {
			t.Fatalf("decode policy state: %v", err)
		}
	}
	if policyState.Unclassified != nil {
		t.Fatalf("unmarked observed-output failure retained a streak: %+v", policyState.Unclassified)
	}
}
