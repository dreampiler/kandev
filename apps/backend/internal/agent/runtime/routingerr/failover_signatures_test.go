package routingerr

import (
	"strings"
	"testing"
	"time"
)

// The samples are the provider texts collected for the 2026-10-12 failover
// catalogue (path, signature, observed meaning). Each one must reach the rule
// routing reads to decide how far and how long the failure suspends a resource.
func TestFailoverCatalogueSignatures(t *testing.T) {
	resetInjection()
	cases := []struct {
		name     string
		provider string
		text     string
		code     Code
		rule     string
	}{
		{"zen daily limit", "opencode-acp", "AI_APICallError: Rate limit exceeded. Please try again later", CodeRateLimited, RuleOpenCodeRateTryLater},
		{"zen console daily limit", "opencode-acp", "AI_APICallError: Error from provider (Console): Rate limit exceeded. Please try again later", CodeRateLimited, RuleOpenCodeRateTryLater},
		{"openrouter free per day", "opencode-acp", "AI_APICallError: Rate limit exceeded: free-models-per-day-high-balance", CodeRateLimited, RuleOpenCodeFreeModelsPerDay},
		{"openrouter free per day short", "opencode-acp", "AI_APICallError: Rate limit exceeded: free-models-per-day. Add 10 credits to unlock 1000 free model requests per day", CodeRateLimited, RuleOpenCodeFreeModelsPerDay},
		{"openrouter model per day", "opencode-acp", "AI_APICallError: Rate limit exceeded: limit_rpd/nvidia/nemotron-3-ultra-550b-a55b:free", CodeRateLimited, RuleOpenCodeModelPerDay},
		{"novita upstream pool", "opencode-acp", "AI_APICallError: [Novita] inclusionai/ling-3.1-flash is temporarily rate-limited upstream. Please retry shortly, or add your own key to accumulate your rate limits", CodeRateLimited, RuleOpenCodeUpstreamHostLimit},
		{"zen not found", "opencode-acp", "AI_APICallError: Not Found", CodeProviderUnavailable, RuleOpenCodeModelWithdrawn},
		{"openrouter no endpoints", "opencode-acp", "AI_APICallError: No endpoints found for stealth/space-bunny-alpha", CodeProviderUnavailable, RuleOpenCodeModelWithdrawn},
		{"zen model unavailable", "opencode-acp", "AI_APICallError: Upstream request failed: Model is unavailable", CodeProviderUnavailable, RuleOpenCodeModelWithdrawn},
		{"go global region", "opencode-acp", "AI_APICallError: Upstream request failed: This Go model requires Global regions. Select Global in your workspace's Privacy settings to use it", CodeProviderUnavailable, RuleOpenCodeGoAccountSetting},
		{"go trains on data", "opencode-acp", "AI_APICallError: Upstream request failed: This Go model trains on request data. Allow paid endpoints that train on request data in your workspace's Privacy settings to use it", CodeProviderUnavailable, RuleOpenCodeGoAccountSetting},
		{"endpoint unavailable", "opencode-acp", "AI_APICallError: Upstream request failed: Endpoint is unavailable", CodeProviderUnavailable, RuleOpenCodeEndpointUnavailable},
		{"header timeout", "opencode-acp", "ProviderHeaderTimeoutError: Provider response headers timed out after 300000ms", CodeProviderUnavailable, RuleProviderHeaderTimeout},
		{"stream 504", "opencode-acp", `Internal error: {"message":"Streaming response failed: [504] Upstream idle timeout exceeded","type":"server_error"}`, CodeProviderUnavailable, RuleProviderStreamServerError},
		{"stream 502", "opencode-acp", "Streaming response failed: [502] Provider returned an empty response", CodeProviderUnavailable, RuleProviderStreamServerError},
		{"internal server error", "opencode-acp", "AI_APICallError: Internal Server Error", CodeProviderUnavailable, RuleProviderInternalServerError},
		{"nvidia overloaded", "opencode-acp", `Internal error: {"code":503,"message":"Upstream error from Nvidia: Service temporarily overloaded","metadata":{"error_type":"provider_overloaded"}}`, CodeProviderUnavailable, RuleProviderNvidiaHost},
		{"nvidia not ready", "opencode-acp", "AI_APICallError: [Nvidia] Service is not ready", CodeProviderUnavailable, RuleProviderNvidiaHost},
		{"cannot connect", "opencode-acp", "AI_APICallError: Cannot connect to API: The socket connection was closed unexpectedly", CodeNetworkUnavailable, RuleProviderCannotConnect},
		// Signatures whose rule did not change.
		{"go usage limit", "opencode-acp", "AI_APICallError: Go usage limit exceeded", CodeQuotaLimited, RuleOpenCodeGoUsageLimit},
		{"opencode service failure", "opencode-acp", "Internal error: OpenCode service failure", CodeProviderUnavailable, RuleOpenCodeServiceFailure},
		{"free per minute stays generic", "opencode-acp", "AI_APICallError: Rate limit exceeded: free-models-per-min", CodeRateLimited, "opencode.stderr.rate.v1"},
		{"bare internal error is not a provider cause", "opencode-acp", "Internal error", CodeAgentRuntime, "phase.poststart.unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(Input{Phase: PhasePromptSend, ProviderID: tc.provider, Stderr: tc.text})
			if got.Code != tc.code || got.ClassifierRule != tc.rule {
				t.Fatalf("Classify(%q) = %s/%s, want %s/%s", tc.text, got.Code, got.ClassifierRule, tc.code, tc.rule)
			}
			if !MayClassify(Input{ProviderID: tc.provider, Stderr: tc.text}) {
				t.Fatalf("the prefilter gate rejects %q", tc.text)
			}
		})
	}
	// Every rule added for the catalogue carries a literal the prefilter can
	// gate on, so it does not switch the gate off for the whole catalogue.
	added := map[string]bool{
		RuleOpenCodeRateTryLater: true, RuleOpenCodeFreeModelsPerDay: true, RuleOpenCodeModelPerDay: true,
		RuleOpenCodeUpstreamHostLimit: true, RuleOpenCodeModelWithdrawn: true, RuleOpenCodeGoAccountSetting: true,
		RuleOpenCodeEndpointUnavailable: true, RuleProviderHeaderTimeout: true, RuleProviderStreamServerError: true,
		RuleProviderInternalServerError: true, RuleProviderNvidiaHost: true, RuleProviderCannotConnect: true,
	}
	all := append([]rule{}, providerNeutralRules...)
	for _, rules := range providerRules {
		all = append(all, rules...)
	}
	found := 0
	for _, r := range all {
		if !added[r.id] {
			continue
		}
		found++
		if len(patternGateTokens(r.pattern)) == 0 {
			t.Fatalf("rule %s has no derivable prefilter token", r.id)
		}
	}
	if found != len(added) {
		t.Fatalf("found %d of %d added rules in the catalogue", found, len(added))
	}
}

// Agent prose that quotes a gateway notice must not suspend a healthy model.
func TestFailoverSignaturesIgnoreQuotedProse(t *testing.T) {
	resetInjection()
	for _, text := range []string{
		"I checked the logs and the endpoint is unavailable sometimes, so I retried.",
		"The previous run said Not Found for that file.",
		"Earlier the model returned an internal server error, but it works now.",
	} {
		got := Classify(Input{Phase: PhasePromptSend, ProviderID: "opencode-acp", Stderr: text})
		switch got.ClassifierRule {
		case RuleOpenCodeEndpointUnavailable, RuleOpenCodeModelWithdrawn, RuleProviderInternalServerError:
			t.Fatalf("prose %q matched %s", text, got.ClassifierRule)
		}
	}
}

// A stored notice must keep the facts a suspension length is read from: the
// renewal date and the limit reason code. Credentials stay redacted.
func TestSanitizeKeepsLimitDatesAndReasons(t *testing.T) {
	cases := []struct {
		in       string
		keep     string
		mustDrop string
	}{
		{
			in:   "AI_APICallError: Dev Plan credit limit reached. Upgrade your plan or wait for renewal on 10/7/2026. Or enable pay-as-you-go overflow in your DevPass dashboard to keep going past your allowance",
			keep: "renewal on 10/7/2026",
		},
		{in: "AI_APICallError: Rate limit exceeded: free-models-per-day-high-balance", keep: "free-models-per-day-high-balance"},
		{in: "AI_APICallError: Rate limit exceeded: limit_rpd/nvidia/nemotron-3-ultra-550b-a55b:free", keep: "limit_rpd/nvidia/nemotron-3-ultra-550b-a55b:free"},
		{
			in:       "AI_APICallError: Rate limit exceeded: free-models-per-day api_key=sk-abcdefghijklmnopqrstuvwxyz0123456789",
			keep:     "free-models-per-day",
			mustDrop: "abcdefghijklmnopqrstuvwxyz0123456789",
		},
		{in: "token ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij0123 leaked", mustDrop: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij0123"},
		{in: "opened /home/alice/project/secret.txt", mustDrop: "alice"},
	}
	for _, tc := range cases {
		got := Sanitize(tc.in)
		if tc.keep != "" && !strings.Contains(got, tc.keep) {
			t.Fatalf("Sanitize(%q) = %q, want it to keep %q", tc.in, got, tc.keep)
		}
		if tc.mustDrop != "" && strings.Contains(got, tc.mustDrop) {
			t.Fatalf("Sanitize(%q) = %q, want %q redacted", tc.in, got, tc.mustDrop)
		}
		if again := Sanitize(got); again != got {
			t.Fatalf("Sanitize is not idempotent: %q -> %q", got, again)
		}
	}
}

// The renewal day survives classification, so a credit limit is held until the
// stated renewal instead of being probed every two hours.
func TestCreditLimitKeepsRenewalDay(t *testing.T) {
	resetInjection()
	now := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	got := Classify(Input{
		Phase: PhasePromptSend, ProviderID: "opencode-acp", OccurredAt: now,
		Stderr: "AI_APICallError: Dev Plan credit limit reached. Upgrade your plan or wait for renewal on 10/7/2026. Or enable pay-as-you-go overflow",
	})
	if got.ClassifierRule != RuleOpenCodeCredit || got.RenewalAt == nil {
		t.Fatalf("credit notice = %s renewal=%v, want %s with a renewal day", got.ClassifierRule, got.RenewalAt, RuleOpenCodeCredit)
	}
	if got.RenewalAt.Month() != time.October || got.RenewalAt.Before(now) {
		t.Fatalf("renewal = %v, want a day in October after %v", got.RenewalAt, now)
	}
}
