package routingerr

import "testing"

const reasoningContentMessage = "Internal error: {\"message\":\"The `reasoning_content` in the thinking mode must be passed back to the API. (request_id: ***)\",\"type\":\"invalid_request_error\",\"param\":null,\"code\":\"invalid_request_error\"}"

func TestClassify_ReasoningContentMissing(t *testing.T) {
	e := Classify(Input{Phase: PhasePromptSend, ProviderID: "opencode-acp", Stderr: reasoningContentMessage})
	if e.Code != CodeAgentRuntime {
		t.Fatalf("Code = %q, want %q", e.Code, CodeAgentRuntime)
	}
	if e.Confidence != ConfHigh {
		t.Fatalf("Confidence = %q, want high", e.Confidence)
	}
	if e.ClassifierRule != reasoningContentMissingRuleID {
		t.Fatalf("ClassifierRule = %q, want %q", e.ClassifierRule, reasoningContentMissingRuleID)
	}
	if e.Class != ClassUnclassified {
		t.Fatalf("Class = %q, want unclassified", e.Class)
	}
}

func TestIsResumeCorrupted_ReasoningContent(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want bool
	}{
		{"deepseek reasoning_content 400", reasoningContentMessage, true},
		{"bare phrasing", "The reasoning_content in the thinking mode must be passed back to the API.", true},
		{"reasoning_content without the phrase", "the model emitted reasoning_content blocks inline", false},
		{"ordinary prose", "please pass the reasoning back to me", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		if got := IsResumeCorrupted(tc.msg); got != tc.want {
			t.Errorf("%s: IsResumeCorrupted = %v, want %v", tc.name, got, tc.want)
		}
	}
}
