package routingerr

import (
	"testing"
	"time"
)

// Agent reports that ended live turns as provider failures in production. They
// quote or describe other sessions' limit errors; none of them is the reporting
// agent's own provider failing.
var agentProseAboutLimits = []string{
	"표준 MCP 서버 작업을 이어받을 새 세션을 띄웠습니다. 제가 다시 지시한 내용을 받은 세션이 시작하자마자 사용량 한도(Rate limit)에 걸렸습니다.",
	"두 알림은 모두 이미 처리한 옛 턴입니다(08:39Z·08:52Z Rate limit).",
	"Now I have a clear picture. Other tasks are stuck on technical issues (rate limits, model not found).",
	"The child's sessions are all persistently rate-limited free models.",
	"The newest session (05:48) died on rate limit, so I moved the card to hold.",
	"The plan document hit its size quota earlier today; I trimmed old sections.",
	"This step does not need a subscription; it only reads the board.",
}

// claudeProseQuotingOtherProviders is Claude prose quoting another provider's
// exact error lines. A Claude prompt error that ends with the agent's final
// text carries it, and it names no Anthropic failure.
var claudeProseQuotingOtherProviders = []string{
	"[→운영 세션] 막힘 — R4 자식 6개가 세션을 재개할 때 모델 호출이 실패(`AI_APICallError: Not Found`·`Rate limit exceeded`)해 완료 신호 없이 멈췄습니다.",
	"Internal error: 자식 세션 기록: Agent encountered an error: AI_APICallError: Rate limit exceeded. Please try again later",
	"The OpenCode child reported `AI_APICallError: Go usage limit exceeded` and the DevPass child reported `Dev Plan credit limit reached`.",
}

func TestClassify_AgentProseAboutLimitsIsNotAProviderLimit(t *testing.T) {
	resetInjection()
	for _, text := range claudeProseQuotingOtherProviders {
		got := Classify(Input{Phase: PhasePromptSend, ProviderID: "claude-acp", Stderr: text})
		switch got.Code {
		case CodeRateLimited, CodeQuotaLimited, CodeSubscriptionRequired:
			t.Fatalf("claude prose %q classified as %s by %s", text, got.Code, got.ClassifierRule)
		}
	}
	for _, provider := range []string{"claude-acp", "opencode-acp", "copilot-acp", "amp-acp"} {
		for _, text := range agentProseAboutLimits {
			for _, phase := range []Phase{PhasePromptSend, PhaseStreaming} {
				got := Classify(Input{Phase: phase, ProviderID: provider, Stderr: text})
				switch got.Code {
				case CodeRateLimited, CodeQuotaLimited, CodeSubscriptionRequired:
					t.Fatalf("%s prose %q classified as %s by %s", provider, text, got.Code, got.ClassifierRule)
				}
			}
		}
	}
}

func TestClassify_ProviderLimitSignaturesStillClassify(t *testing.T) {
	resetInjection()
	cases := []struct {
		provider string
		text     string
		want     Code
	}{
		{"claude-acp", `Internal error: API Error: 429 {"type":"error","error":{"type":"rate_limit_error","message":"This request would exceed the rate limit for your organization"}}`, CodeRateLimited},
		{"claude-acp", "API Error: Request rejected (429) · This request would exceed your account's rate limit.", CodeRateLimited},
		{"claude-acp", "HTTP 429 Too Many Requests", CodeRateLimited},
		{"claude-acp", "You've hit your rate limit", CodeRateLimited},
		{"claude-acp", "rate limit exceeded", CodeRateLimited},
		{"claude-acp", "Internal error: Rate limit exceeded. Please retry shortly.", CodeRateLimited},
		{"claude-acp", "request failed\nRate limit reached for this organization", CodeRateLimited},
		{"claude-acp", "Your credit balance is too low to access the Anthropic API.", CodeQuotaLimited},
		{"claude-acp", "Claude Code requires an active Claude Pro or Max subscription.", CodeSubscriptionRequired},
		{"claude-acp", "Your subscription has expired.", CodeSubscriptionRequired},
		{"opencode-acp", "AI_APICallError: Rate limit exceeded. Please try again later", CodeRateLimited},
		{"opencode-acp", "AI_APICallError: Rate limit exceeded: free-models-per-min", CodeRateLimited},
		{"opencode-acp", "rate_limit_exceeded", CodeRateLimited},
		{"opencode-acp", "AI_APICallError: Too Many Requests", CodeRateLimited},
		{"opencode-acp", "AI_APICallError: You exceeded your current quota, please check your plan and billing details.", CodeQuotaLimited},
		{"opencode-acp", "AI_APICallError: Quota exceeded for quota metric 'Generate Content API requests per minute'", CodeQuotaLimited},
		{"opencode-acp", "AI_APICallError: RESOURCE_EXHAUSTED", CodeQuotaLimited},
		{"copilot-acp", "Error: rate limit exceeded", CodeRateLimited},
		{"amp-acp", "rate limited: rate limit reached for this account", CodeRateLimited},
		{"amp-acp", "insufficient_quota", CodeQuotaLimited},
	}
	for _, tc := range cases {
		got := Classify(Input{Phase: PhaseStreaming, ProviderID: tc.provider, Stderr: tc.text})
		if got.Code != tc.want {
			t.Fatalf("%s %q → %s (%s), want %s", tc.provider, tc.text, got.Code, got.ClassifierRule, tc.want)
		}
	}
}

func TestClassifyAgentNotice_ProseMentioningALimitIsNotANotice(t *testing.T) {
	resetInjection()
	for _, provider := range []string{"claude-acp", "opencode-acp", "codex-acp", "antigravity-acp"} {
		for _, text := range append(agentProseAboutLimits,
			"The child's last message was `API Error: 429 rate_limit_error`; I left it on hold.",
			"Codex said \"You've hit your usage limit\" for the reviewer, so I switched it.",
			"Status: Go usage limit exceeded on the parent session.",
		) {
			if got := ClassifyAgentNotice(Input{Phase: PhasePromptSend, ProviderID: provider, Stderr: text}); got != nil {
				t.Fatalf("%s prose %q classified as notice %s (%s)", provider, text, got.Code, got.ClassifierRule)
			}
		}
	}
}

func TestClassifyAgentNotice_NoticeOpeningTheOutput(t *testing.T) {
	resetInjection()
	observed := time.Date(2026, 10, 4, 15, 51, 8, 0, time.UTC)
	cases := []struct {
		provider string
		text     string
		want     Code
	}{
		{"antigravity-acp", "\n" + antigravityQuotaNotice, CodeQuotaLimited},
		{"codex-acp", "You’ve hit your usage limit. Visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at Sep 27th, 2026 3:09 AM.", CodeQuotaLimited},
		{"claude-acp", "You've hit your session limit · resets 11:10am (Europe/Helsinki)", CodeQuotaLimited},
		{"claude-acp", `API Error: 429 {"type":"error","error":{"type":"rate_limit_error"}}`, CodeRateLimited},
		{"opencode-acp", "AI_APICallError: Rate limit exceeded. Please try again later", CodeRateLimited},
	}
	for _, tc := range cases {
		got := ClassifyAgentNotice(Input{Phase: PhasePromptSend, ProviderID: tc.provider, Stderr: tc.text, OccurredAt: observed})
		if got == nil || got.Code != tc.want || got.Confidence != ConfHigh || !got.FallbackAllowed {
			t.Fatalf("%s notice %q → %+v, want high-confidence %s", tc.provider, tc.text, got, tc.want)
		}
	}
	agy := ClassifyAgentNotice(Input{Phase: PhasePromptSend, ProviderID: "antigravity-acp", Stderr: antigravityQuotaNotice, OccurredAt: observed})
	if agy.ResetHint == nil || !agy.ResetHint.Equal(observed.Add(88*time.Minute)) {
		t.Fatalf("notice reset hint = %v, want %s", agy.ResetHint, observed.Add(88*time.Minute))
	}
}
