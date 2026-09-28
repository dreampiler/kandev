package dynamic

import (
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/agent/runtime/routingpolicy"
)

// A launch that never produced output classifies as an unclassified
// low-confidence code. Process-start failures now advance through the
// candidate policy instead of a candidate circuit, so those codes must not
// qualify for one. Classified provider-health failures still do.
func TestQualifiesForCircuitOnlyClassifiedProviderFailures(t *testing.T) {
	retained := []routingerr.Code{
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
	}
	for _, code := range retained {
		if !qualifiesForCircuit(code) {
			t.Errorf("qualifiesForCircuit(%q) = false, want true", code)
		}
	}
	excluded := []routingerr.Code{
		routingerr.CodeUnknownProvider,
		routingerr.CodeAgentRuntime,
		routingerr.CodeTask,
		routingerr.CodeRepo,
		routingerr.CodePermissionDeniedByUser,
		routingerr.CodeAgentTransportLost,
		routingerr.CodeResumeCorrupted,
		routingerr.CodeNpxCacheCorrupted,
		routingerr.Code("future_provider_code"),
	}
	for _, code := range excluded {
		if qualifiesForCircuit(code) {
			t.Errorf("qualifiesForCircuit(%q) = true, want false", code)
		}
	}
}

// An ordinary classified rate-limit failure still opens the failed candidate's
// circuit while the policy advances the route.
func TestApplyFailureRateLimitedStillOpensCircuit(t *testing.T) {
	now := time.Unix(5000, 0)
	engine := NewEngine(WithClock(func() time.Time { return now }))
	profile := Profile{
		ID: "dynamic-rate-limit", Version: 1,
		Candidates: []Candidate{
			{ID: "first", Enabled: true, BindingKey: "credential:first", Policies: routingpolicy.DefaultDocument()},
			{ID: "second", Enabled: true, BindingKey: "credential:second", Policies: routingpolicy.DefaultDocument()},
		},
	}
	initial, err := engine.Select("session-rate-limit", profile, 0, "")
	if err != nil {
		t.Fatalf("initial Select: %v", err)
	}
	failure := &routingerr.Error{
		Code: routingerr.CodeRateLimited, Class: routingerr.ClassTransient,
		Phase: routingerr.PhaseProcessStart, Confidence: routingerr.ConfHigh, FallbackAllowed: true,
	}
	decision, err := engine.ApplyFailure("session-rate-limit", profile, initial.Generation, initial.ExecutionProfileID, failure)
	if err != nil {
		t.Fatalf("ApplyFailure: %v", err)
	}
	if decision.ExecutionProfileID != "second" {
		t.Fatalf("decision = %q, want the policy to advance to second", decision.ExecutionProfileID)
	}
	if !engine.Circuits().IsOpen("credential:first", now) {
		t.Fatal("rate_limited failure did not open the failed candidate's circuit")
	}
}
