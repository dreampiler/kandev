package routingerr

import "testing"

// The structured "error_type":"provider_unavailable" envelope is emitted by the
// adapter, not written by the model, so it classifies at high confidence even
// though the underscore-joined value never matches the space-separated prose
// rule. The line may open with the status prefix the agent error surface adds.
func TestProviderStructuredErrorTypeClassifies(t *testing.T) {
	resetInjection()
	cases := []struct {
		name string
		text string
	}{
		{
			name: "raw envelope",
			text: `Internal error: {"code":503,"message":"JSON error injected into SSE stream","metadata":{"error_type":"provider_unavailable"}}`,
		},
		{
			name: "status prefix",
			text: `Agent encountered an error: Internal error: {"code":502,"message":"JSON error injected into SSE stream","metadata":{"error_type":"provider_unavailable"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(Input{Phase: PhasePromptSend, ProviderID: "opencode-acp", Stderr: tc.text})
			if got.Code != CodeProviderUnavailable || got.Confidence != ConfHigh ||
				got.ClassifierRule != "provider.structured_error_type.v1" {
				t.Fatalf("classification = %+v, want provider_unavailable/high/provider.structured_error_type.v1", got)
			}
		})
	}
}

// The context-length rejection keeps the agent_runtime_error code so a candidate
// with a larger context window remains reachable; naming the rule only removes
// the failure from the anonymous phase default.
func TestProviderContextLengthExceededClassifies(t *testing.T) {
	resetInjection()
	for _, text := range []string{
		"Internal error: This model's maximum context length is 32768 tokens. However, your messages resulted in 41000 tokens.",
		"This model's maximum context length is 32768 tokens.",
	} {
		got := Classify(Input{Phase: PhasePromptSend, ProviderID: "opencode-acp", Stderr: text})
		if got.Code != CodeAgentRuntime || got.Confidence != ConfHigh ||
			got.ClassifierRule != "provider.context_length_exceeded.v1" {
			t.Fatalf("%q classification = %+v, want agent_runtime_error/high/provider.context_length_exceeded.v1", text, got)
		}
	}
}

// The structured error-type field and the context-length phrase are provider
// signatures, not a topic an agent narrates. Prose that does not open a line
// with the error envelope must stay unclassified.
func TestProviderStructuredSignatureRulesIgnoreQuotedProse(t *testing.T) {
	resetInjection()
	for _, text := range []string{
		`The other session reported {"metadata":{"error_type":"provider_unavailable"}} before it retried.`,
		`Child session log: Agent encountered an error: Internal error: {"code":502,"message":"JSON error injected into SSE stream","metadata":{"error_type":"provider_unavailable"}}`,
		`{"error_type":"provider_unavailable","retry":true}`,
		`{"metadata":{"error_type":"provider_unavailable"}}`,
		"The reviewer said the model's maximum context length is 32768 tokens, so I split the file.",
	} {
		got := Classify(Input{Phase: PhasePromptSend, ProviderID: "opencode-acp", Stderr: text})
		switch got.ClassifierRule {
		case "provider.structured_error_type.v1", "provider.context_length_exceeded.v1":
			t.Fatalf("prose %q matched %s", text, got.ClassifierRule)
		}
	}
}
