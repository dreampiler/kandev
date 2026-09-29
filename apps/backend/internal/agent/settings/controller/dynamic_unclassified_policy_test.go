package controller

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/agent/settings/dto"
)

func canonicalPolicyWithUnclassified(repeated dto.DynamicRepeatedFailurePolicyDTO) *dto.DynamicAgentPolicyDTO {
	return &dto.DynamicAgentPolicyDTO{
		Version:      1,
		Transient:    dto.DynamicErrorPolicyDTO{OnExhausted: "skip"},
		Hard:         dto.DynamicErrorPolicyDTO{OnExhausted: "skip"},
		Unclassified: dto.DynamicUnclassifiedPolicyDTO{OnExhausted: "stop", RepeatedFailure: repeated},
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
	if err := validateDynamicAgentProfile(candidate(dto.DynamicRepeatedFailurePolicyDTO{Enabled: false, Threshold: 3})); err != nil {
		t.Fatalf("disabled nonzero threshold is normalized to zero, got error: %v", err)
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
	if policy.Unclassified.OnExhausted != dynamicPolicyOutcomeStop {
		t.Fatalf("unclassified on_exhausted = %q, want stop", policy.Unclassified.OnExhausted)
	}
	if policy.Unclassified.RepeatedFailure.Enabled {
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
	if !decoded.Unclassified.RepeatedFailure.Enabled || decoded.Unclassified.RepeatedFailure.Threshold != 4 {
		t.Fatalf("round-tripped unclassified policy = %#v", decoded.Unclassified)
	}
}
