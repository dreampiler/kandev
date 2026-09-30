package controller

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/agent/settings/dto"
)

func canonicalPolicyWithUnclassified(repeated dto.DynamicRepeatedFailurePolicyDTO) *dto.DynamicAgentPolicyDTO {
	return &dto.DynamicAgentPolicyDTO{
		Version:   1,
		Transient: dto.DynamicErrorPolicyDTO{OnExhausted: "skip"},
		Hard:      dto.DynamicErrorPolicyDTO{OnExhausted: "skip"},
		Unclassified: &dto.DynamicUnclassifiedPolicyDTO{
			Enabled:                     repeated.Enabled,
			ConsecutiveFailureThreshold: repeated.Threshold,
			OnExhausted:                 "stop",
			RepeatedFailure:             &repeated,
		},
	}
}

func TestValidateDynamicUnclassifiedRepeatedFailurePolicy(t *testing.T) {
	candidate := func(repeated dto.DynamicRepeatedFailurePolicyDTO) *dto.DynamicAgentProfileDTO {
		return &dto.DynamicAgentProfileDTO{Candidates: []dto.DynamicAgentCandidateDTO{{
			Position: 0, ExecutionProfileID: "a",
			Policies: canonicalPolicyWithUnclassified(repeated),
		}}}
	}
	if err := validateDynamicAgentProfile(candidate(dto.DynamicRepeatedFailurePolicyDTO{Enabled: true, Threshold: 2})); err != nil {
		t.Fatalf("enabled valid threshold rejected: %v", err)
	}
	if err := validateDynamicAgentProfile(candidate(dto.DynamicRepeatedFailurePolicyDTO{Enabled: true, Threshold: 0})); !errors.Is(err, ErrDynamicProfileRule) {
		t.Fatalf("enabled zero threshold error = %v, want ErrDynamicProfileRule", err)
	}
	if err := validateDynamicAgentProfile(candidate(dto.DynamicRepeatedFailurePolicyDTO{Enabled: false, Threshold: 3})); !errors.Is(err, ErrDynamicProfileRule) {
		t.Fatalf("disabled nonzero threshold error = %v, want ErrDynamicProfileRule", err)
	}
	if err := validateDynamicAgentProfile(candidate(dto.DynamicRepeatedFailurePolicyDTO{})); err != nil {
		t.Fatalf("disabled default rejected: %v", err)
	}
}

func TestDecodeDynamicPolicyDefaultsMissingUnclassified(t *testing.T) {
	// A document persisted before the unclassified section existed must decode
	// with a fail-closed default rather than an invalid empty on_exhausted.
	raw := `{"version":1,"transient":{"on_exhausted":"skip"},"hard":{"on_exhausted":"skip"}}`
	policy, err := decodeDynamicPolicyDocument(raw, 0)
	if err != nil {
		t.Fatalf("decodeDynamicPolicyDocument: %v", err)
	}
	if policy.Unclassified == nil {
		t.Fatal("unclassified section missing")
	}
	if policy.Unclassified.OnExhausted != dynamicPolicyOutcomeStop {
		t.Fatalf("unclassified on_exhausted = %q, want stop", policy.Unclassified.OnExhausted)
	}
	if policy.Unclassified.RepeatedFailure == nil || policy.Unclassified.RepeatedFailure.Enabled {
		t.Fatal("unclassified repeated failure must default to disabled")
	}
}

func TestDynamicPolicyJSONRoundTripsUnclassifiedSection(t *testing.T) {
	policy := canonicalPolicyWithUnclassified(dto.DynamicRepeatedFailurePolicyDTO{Enabled: true, Threshold: 4})
	payload, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := decodeDynamicPolicyDocument(string(payload), 0)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Unclassified == nil || !decoded.Unclassified.Enabled || decoded.Unclassified.ConsecutiveFailureThreshold != 4 ||
		decoded.Unclassified.RepeatedFailure == nil || !decoded.Unclassified.RepeatedFailure.Enabled || decoded.Unclassified.RepeatedFailure.Threshold != 4 {
		t.Fatalf("round-tripped unclassified policy = %#v", decoded.Unclassified)
	}
}

func TestDecodeLegacyDynamicUnclassifiedPolicyAndRejectConflicts(t *testing.T) {
	base := `{"version":1,"transient":{"on_exhausted":"skip"},"hard":{"on_exhausted":"skip"}`
	legacy := base + `,"unclassified":{"on_exhausted":"stop","repeated_failure":{"enabled":true,"threshold":3}}}`
	decoded, err := decodeDynamicPolicyDocument(legacy, 0)
	if err != nil {
		t.Fatalf("decode legacy policy: %v", err)
	}
	if decoded.Unclassified == nil || !decoded.Unclassified.Enabled || decoded.Unclassified.ConsecutiveFailureThreshold != 3 {
		t.Fatalf("legacy policy normalized to %#v, want enabled threshold 3", decoded.Unclassified)
	}
	if decoded.Unclassified.RepeatedFailure == nil || decoded.Unclassified.RepeatedFailure.Threshold != 3 {
		t.Fatalf("legacy alias not preserved: %#v", decoded.Unclassified.RepeatedFailure)
	}

	conflicting := base + `,"unclassified":{"enabled":true,"consecutive_failure_threshold":4,"on_exhausted":"stop","repeated_failure":{"enabled":true,"threshold":3}}}`
	if _, err := decodeDynamicPolicyDocument(conflicting, 0); !errors.Is(err, ErrDynamicProfileRule) {
		t.Fatalf("conflicting dual policy error = %v, want ErrDynamicProfileRule", err)
	}
}

func TestLegacyRepeatedFailureThresholdOneMigratesToTwoWithoutWideningCanonicalBounds(t *testing.T) {
	base := `{"version":1,"transient":{"on_exhausted":"skip"},"hard":{"on_exhausted":"skip"}`
	legacy := base + `,"unclassified":{"on_exhausted":"stop","repeated_failure":{"enabled":true,"threshold":1}}}`
	decoded, err := decodeDynamicPolicyDocument(legacy, 0)
	if err != nil {
		t.Fatalf("decode legacy threshold 1: %v", err)
	}
	if decoded.Unclassified == nil || !decoded.Unclassified.Enabled || decoded.Unclassified.ConsecutiveFailureThreshold != 2 ||
		decoded.Unclassified.RepeatedFailure == nil || decoded.Unclassified.RepeatedFailure.Threshold != 2 {
		t.Fatalf("legacy threshold 1 normalized to %#v", decoded.Unclassified)
	}
	serialized, err := json.Marshal(decoded.Unclassified)
	if err != nil {
		t.Fatalf("marshal legacy policy: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(serialized, &fields); err != nil {
		t.Fatalf("decode marshaled policy: %v", err)
	}
	var threshold int64
	if err := json.Unmarshal(fields["consecutive_failure_threshold"], &threshold); err != nil || threshold != 2 {
		t.Fatalf("canonical threshold = %d, error = %v, want normalized 2", threshold, err)
	}

	canonical := base + `,"unclassified":{"enabled":true,"consecutive_failure_threshold":1,"on_exhausted":"stop"}}`
	if _, err := decodeDynamicPolicyDocument(canonical, 0); !errors.Is(err, ErrDynamicProfileRule) {
		t.Fatalf("canonical threshold 1 error = %v, want ErrDynamicProfileRule", err)
	}
	conflicting := base + `,"unclassified":{"enabled":true,"consecutive_failure_threshold":3,"on_exhausted":"stop","repeated_failure":{"enabled":true,"threshold":1}}}`
	if _, err := decodeDynamicPolicyDocument(conflicting, 0); !errors.Is(err, ErrDynamicProfileRule) {
		t.Fatalf("canonical-plus-alias threshold 1 error = %v, want ErrDynamicProfileRule", err)
	}
}
