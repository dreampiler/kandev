package routingerr

import (
	"strings"
	"testing"
)

// reasoningContentMessage is the exact DeepSeek thinking-mode 400 an agent
// surfaces after a session/load resume reconstructed the assistant history
// without its `reasoning_content`. The persisted reasoning state is poisoned,
// so resuming replays the same 400 and only a fresh session recovers.
const reasoningContentMessage = "Internal error: {\"message\":\"The `reasoning_content` in the thinking mode " +
	"must be passed back to the API. (request_id: ***)\",\"type\":\"invalid_request_error\",\"param\":null," +
	"\"code\":\"invalid_request_error\"}"

func TestClassify_ReasoningContentNonReplayable(t *testing.T) {
	resetInjection()
	e := Classify(Input{Phase: PhasePromptSend, ProviderID: "opencode-acp", Stderr: reasoningContentMessage})
	if e == nil {
		t.Fatal("expected non-nil Error")
	}
	if e.Code != CodeResumeCorrupted {
		t.Fatalf("Code = %q, want %q", e.Code, CodeResumeCorrupted)
	}
	if e.Confidence != ConfHigh {
		t.Fatalf("Confidence = %q, want %q", e.Confidence, ConfHigh)
	}
	if e.ClassifierRule != reasoningContentNonReplayableRuleID {
		t.Fatalf("ClassifierRule = %q, want %q", e.ClassifierRule, reasoningContentNonReplayableRuleID)
	}
	if e.Class != ClassUnclassified {
		t.Fatalf("Class = %q, want unclassified", e.Class)
	}
	if e.RemediationPath != RemediationStartFreshSession {
		t.Fatalf("RemediationPath = %q, want %q", e.RemediationPath, RemediationStartFreshSession)
	}
	if !e.UserAction {
		t.Errorf("UserAction = false, want true")
	}
	if e.AutoRetryable {
		t.Errorf("AutoRetryable = true, want false (resuming is futile)")
	}
	if e.FallbackAllowed {
		t.Errorf("FallbackAllowed = true, want false (session-state corruption, not a provider problem)")
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
		{"gap beyond the bound", "reasoning_content" + strings.Repeat("x", 81) + "must be passed back", false},
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
