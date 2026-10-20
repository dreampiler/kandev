package routingerr

import (
	"strings"
	"testing"
)

// The Zen/OpenRouter daily-limit notices collected for the fork failover
// catalogue. Each must reach its own rule so routing can scope the failure from
// the reason code, and the OpenRouter reason codes must survive sanitization.
func TestOpenCodeDailyLimitNoticesClassify(t *testing.T) {
	resetInjection()
	cases := []struct {
		name string
		text string
		rule string
	}{
		{
			name: "openrouter free per day high balance",
			text: "AI_APICallError: Rate limit exceeded: free-models-per-day-high-balance",
			rule: "opencode.rate.free_models_per_day.v1",
		},
		{
			name: "openrouter free per day short",
			text: "AI_APICallError: Rate limit exceeded: free-models-per-day. Add 10 credits to unlock 1000 free model requests per day",
			rule: "opencode.rate.free_models_per_day.v1",
		},
		{
			name: "openrouter model per day",
			text: "AI_APICallError: Rate limit exceeded: limit_rpd/nvidia/nemotron-3-ultra-550b-a55b:free",
			rule: "opencode.rate.model_per_day.v1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(Input{Phase: PhasePromptSend, ProviderID: "opencode-acp", Stderr: tc.text})
			if got.Code != CodeRateLimited || got.ClassifierRule != tc.rule {
				t.Fatalf("Classify(%q) = %s/%s, want rate_limited/%s", tc.text, got.Code, got.ClassifierRule, tc.rule)
			}
		})
	}
}

// Zen's "Rate limit exceeded. Please try again later" states no cadence, so it
// stays a generic rate limit: the rule names the signature, never a daily
// allowance, and the wrapped "Error from provider (host):" form is covered.
func TestOpenCodeTryLaterIsGenericRateLimitNotDaily(t *testing.T) {
	resetInjection()
	for _, text := range []string{
		"AI_APICallError: Rate limit exceeded. Please try again later",
		"AI_APICallError: Error from provider (Console): Rate limit exceeded. Please try again later",
	} {
		got := Classify(Input{Phase: PhasePromptSend, ProviderID: "opencode-acp", Stderr: text})
		if got.Code != CodeRateLimited {
			t.Fatalf("Classify(%q) = %s, want rate_limited", text, got.Code)
		}
		switch got.ClassifierRule {
		case "opencode.rate.free_models_per_day.v1", "opencode.rate.model_per_day.v1":
			t.Fatalf("Classify(%q) = %s, want a non-daily rule", text, got.ClassifierRule)
		}
	}
}

// A per-minute allowance stays on the generic rate rule; the per-day rule must
// not absorb it.
func TestOpenCodeFreePerMinuteStaysGeneric(t *testing.T) {
	resetInjection()
	got := Classify(Input{Phase: PhasePromptSend, ProviderID: "opencode-acp", Stderr: "AI_APICallError: Rate limit exceeded: free-models-per-min"})
	if got.Code != CodeRateLimited || got.ClassifierRule != "opencode.stderr.rate.v1" {
		t.Fatalf("classification = %s/%s, want rate_limited/opencode.stderr.rate.v1", got.Code, got.ClassifierRule)
	}
}

// Agent prose that quotes a gateway notice must not suspend a healthy model.
func TestOpenCodeDailyLimitRulesIgnoreQuotedProse(t *testing.T) {
	resetInjection()
	for _, text := range []string{
		"Child session log: AI_APICallError: Rate limit exceeded: free-models-per-day",
		"The previous run said Rate limit exceeded. Please try again later, so I waited.",
	} {
		got := Classify(Input{Phase: PhasePromptSend, ProviderID: "opencode-acp", Stderr: text})
		switch got.ClassifierRule {
		case "opencode.rate.free_models_per_day.v1", "opencode.rate.model_per_day.v1", "opencode.rate.try_later.v1":
			t.Fatalf("prose %q matched %s", text, got.ClassifierRule)
		}
	}
}

// A diagnostic that only wears the marker text must not be rewritten into a
// daily signature: classification reads the raw diagnostic, so an original
// string cannot gain a reason code it never carried.
func TestOpenCodeDailyLimitRulesDoNotFabricateSignature(t *testing.T) {
	resetInjection()
	got := Classify(Input{
		Phase:      PhasePromptSend,
		ProviderID: "opencode-acp",
		Stderr:     "Rate limit exceeded: kandevkeepmask0x\nquoted: free-models-per-day",
	})
	switch got.ClassifierRule {
	case "opencode.rate.free_models_per_day.v1", "opencode.rate.model_per_day.v1":
		t.Fatalf("classification = %s, want no daily rule", got.ClassifierRule)
	}
}

// The stored excerpt keeps the shared sanitizer's masking: a "limit_rpd/"
// prefix must not carry an opaque run into RawExcerpt.
func TestOpenCodeDailyLimitRulesMaskOpaqueExcerpt(t *testing.T) {
	resetInjection()
	run := strings.Repeat("a", 40)
	got := Classify(Input{Phase: PhasePromptSend, ProviderID: "opencode-acp", Stderr: "diagnostic: limit_rpd/" + run})
	if strings.Contains(got.RawExcerpt, run) {
		t.Fatalf("RawExcerpt leaked the opaque run: %q", got.RawExcerpt)
	}
}
