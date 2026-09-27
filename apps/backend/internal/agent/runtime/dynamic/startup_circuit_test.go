package dynamic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/agent/runtime/routingpolicy"
)

// A launch that never produces output ("The agent could not start." /
// "agent produced no output since start") classifies as an unclassified code.
// The circuit gate must accept those codes so a startup failure can skip the
// failed candidate, while unrelated non-provider codes stay out.
func TestQualifiesForCircuitCoversStartupFailures(t *testing.T) {
	qualifying := []routingerr.Code{
		routingerr.CodeAuthRequired,
		routingerr.CodeMissingCredentials,
		routingerr.CodeSubscriptionRequired,
		routingerr.CodeProviderNotConfigured,
		routingerr.CodeRateLimited,
		routingerr.CodeQuotaLimited,
		routingerr.CodeNetworkUnavailable,
		routingerr.CodeModelCapacity,
		routingerr.CodeProviderUnavailable,
		routingerr.CodeProviderOverloaded,
		routingerr.CodeModelUnavailable,
		routingerr.CodeUnknownProvider,
		routingerr.CodeAgentRuntime,
	}
	for _, code := range qualifying {
		if !qualifiesForCircuit(code) {
			t.Errorf("qualifiesForCircuit(%q) = false, want true", code)
		}
	}
	excluded := []routingerr.Code{
		routingerr.CodeTask,
		routingerr.CodeRepo,
		routingerr.CodePermissionDeniedByUser,
		routingerr.CodeAgentTransportLost,
		routingerr.CodeResumeCorrupted,
		routingerr.CodeManagedRuntimeNpmResolution,
		routingerr.CodeManagedRuntimeNpmPolicy,
		routingerr.CodeNpxCacheCorrupted,
		routingerr.Code("future_provider_code"),
	}
	for _, code := range excluded {
		if qualifiesForCircuit(code) {
			t.Errorf("qualifiesForCircuit(%q) = true, want false", code)
		}
	}
}

// The orchestrator opens the circuit directly for a startup failure that never
// reaches a classified ApplyFailure. The next selection must skip the failed
// candidate and pick the next one.
func TestOpenCircuitForStartupFailureSkipsFailedCandidate(t *testing.T) {
	now := time.Unix(2000, 0)
	engine := NewEngine(WithClock(func() time.Time { return now }))
	profile := Profile{
		ID: "dynamic-startup", Version: 1,
		Candidates: []Candidate{
			{ID: "first", Enabled: true, BindingKey: "credential:first"},
			{ID: "second", Enabled: true, BindingKey: "credential:second"},
		},
	}
	failure := &routingerr.Error{
		Code:       routingerr.CodeAgentRuntime,
		Class:      routingerr.ClassUnclassified,
		Phase:      routingerr.PhaseProcessStart,
		Confidence: routingerr.ConfLow,
	}
	engine.OpenCircuitForFailure(profile, "first", failure)
	if !engine.Circuits().IsOpen("credential:first", now) {
		t.Fatal("startup failure did not open the failed candidate's circuit")
	}
	decision, err := engine.Select("session-startup", profile, 0, "")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if decision.ExecutionProfileID != "second" {
		t.Fatalf("decision = %q, want the open candidate skipped", decision.ExecutionProfileID)
	}
}

// The asynchronous failure path classifies a no-output runtime failure as
// CodeAgentRuntime. Applying it must open the circuit even when the route
// stops for manual recovery, so the user's next attempt does not reselect the
// same failed candidate.
func TestApplyFailureStartupCodeOpensCircuitWhileRouteStops(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(3000, 0)
	engine := NewEngine(WithClock(func() time.Time { return now }))
	profile := Profile{
		ID: "dynamic-async", Version: 1,
		Candidates: []Candidate{
			{ID: "first", Enabled: true, BindingKey: "credential:first", Policies: routingpolicy.DefaultDocument()},
			{ID: "second", Enabled: true, BindingKey: "credential:second", Policies: routingpolicy.DefaultDocument()},
		},
	}
	initial, err := engine.Select("session-async", profile, 0, "")
	if err != nil {
		t.Fatalf("initial Select: %v", err)
	}
	if initial.ExecutionProfileID != "first" {
		t.Fatalf("initial candidate = %q, want first", initial.ExecutionProfileID)
	}

	failure := &routingerr.Error{
		Code:       routingerr.CodeAgentRuntime,
		Class:      routingerr.ClassUnclassified,
		Phase:      routingerr.PhasePromptSend,
		Confidence: routingerr.ConfLow,
	}
	if _, err := engine.ApplyFailureContextWithEffect(
		ctx, "session-async", profile, initial.Generation, initial.ExecutionProfileID, failure, true,
	); !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("failure error = %v, want no eligible candidate", err)
	}
	if !engine.Circuits().IsOpen("credential:first", now) {
		t.Fatal("classified startup failure did not open the circuit")
	}
	next, err := engine.Select("session-async", profile, initial.Generation, "")
	if err != nil {
		t.Fatalf("subsequent Select: %v", err)
	}
	if next.ExecutionProfileID != "second" {
		t.Fatalf("subsequent candidate = %q, want second while the failed binding is open", next.ExecutionProfileID)
	}
}
