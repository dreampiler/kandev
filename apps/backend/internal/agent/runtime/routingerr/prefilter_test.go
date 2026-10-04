package routingerr

import (
	"strings"
	"testing"
)

// TestEveryRuleIsGateable is the guard that keeps MayClassify conservative. A
// rule the gate cannot cover would silently stop matching once callers adopt the
// pre-check, so every rule must contribute at least one token: derived from its
// pattern, or declared for a custom matcher.
func TestEveryRuleIsGateable(t *testing.T) {
	assertGateable := func(ruleID string, runs []string) {
		t.Helper()
		if len(runs) > 0 {
			return
		}
		if _, ok := gateTokensByRuleID[ruleID]; !ok {
			t.Errorf("rule %q contributes no gate token: add one to gateTokensByRuleID", ruleID)
		}
	}

	for providerID, rules := range providerRules {
		for _, r := range rules {
			assertGateable(providerID+"/"+r.id, literalRuns(r.pattern.String()))
		}
	}
	for _, r := range providerNeutralRules {
		assertGateable(r.id, literalRuns(r.pattern.String()))
	}
	for _, r := range runtimeEnvironmentRules {
		if r.pattern != nil {
			assertGateable(r.id, literalRuns(r.pattern.String()))
			continue
		}
		assertGateable(r.id, nil)
	}
}

// TestDeclaredGateTokensAreReal guards the hand-declared tokens for custom
// matchers: the token must be a literal the matcher actually requires, so a
// renamed control frame cannot leave the gate matching nothing.
func TestDeclaredGateTokensAreReal(t *testing.T) {
	if token, ok := gateTokensByRuleID["cursor.retriable_stream_reset.v1"]; !ok {
		t.Fatal("cursor retriable rule has no declared gate token")
	} else if !strings.Contains(strings.ToLower(cursorRetriableStreamResetPrefix), token) {
		t.Errorf("cursor gate token %q is not part of the required prefix %q", token, cursorRetriableStreamResetPrefix)
	}
	if token, ok := gateTokensByRuleID["opencode.service_failure.v1"]; !ok {
		t.Fatal("opencode service failure rule has no declared gate token")
	} else if !strings.Contains(token, "opencode") {
		t.Errorf("opencode gate token %q does not cover the matched service-failure wording", token)
	}
}

// TestMayClassifyNeverRejectsAMatchingRule is the behavioural half of the
// guarantee: for every representative text the catalogue classifies, the
// pre-check must agree and let the classification through.
func TestMayClassifyNeverRejectsAMatchingRule(t *testing.T) {
	cases := []struct {
		name      string
		provider  string
		text      string
		wantRule  string
		structure Input
	}{
		{name: "claude quota", provider: "claude-acp", text: "Error: anthropic_quota_exceeded", wantRule: "claude.stderr.quota.v1"},
		{name: "claude rate", provider: "claude-acp", text: "rate limit reached, slow down", wantRule: "claude.stderr.rate.v1"},
		{name: "claude session limit", provider: "claude-acp", text: "You've hit your session limit for now", wantRule: "claude.stderr.session_limit.v1"},
		{name: "claude auth", provider: "claude-acp", text: "Please run `claude` to authenticate", wantRule: "claude.stderr.auth.v1"},
		{name: "claude model", provider: "claude-acp", text: "API error: model gpt-9 not found", wantRule: "claude.stderr.model.v1"},
		{name: "claude proxy credentials", provider: "claude-acp", text: "proxy_error: credentials were refused by the gateway", wantRule: "claude.proxy.credentials_refused.v1"},
		{name: "codex quota", provider: "codex-acp", text: "insufficient_quota", wantRule: "codex.stderr.quota.v1"},
		{name: "codex rate", provider: "codex-acp", text: "429 too many requests", wantRule: "codex.stderr.rate.v1"},
		{name: "codex auth", provider: "codex-acp", text: "invalid api key provided", wantRule: "codex.stderr.auth.v1"},
		{name: "opencode usage limit", provider: "opencode-acp", text: "You have reached your 5-hour usage limit reached", wantRule: "opencode.stderr.usage_limit.v1"},
		{name: "opencode credit", provider: "opencode-acp", text: "You are out of credits", wantRule: "opencode.stderr.credit.v1"},
		{name: "opencode payment", provider: "opencode-acp", text: "payment required to continue", wantRule: "opencode.stderr.subscription.v1"},
		{name: "opencode service failure", provider: "opencode-acp", text: "Internal error: OpenCode service failure", wantRule: "opencode.service_failure.v1"},
		{name: "copilot entitled", provider: "copilot-acp", text: "You are not entitled to this model", wantRule: "copilot.stderr.subscription.v1"},
		{name: "amp unauthorized", provider: "amp-acp", text: "unauthorized", wantRule: "amp.stderr.auth.v1"},
		{name: "provider quota notice", provider: "unknown-provider", text: "You have reached your current quota for this period. Your limit will reset in 3 hours.", wantRule: "provider.quota_notice.v1"},
		{name: "neutral model capacity", provider: "opencode-acp", text: "selected model is at capacity", wantRule: "provider.model_capacity.v1"},
		{name: "neutral network", provider: "opencode-acp", text: "connect ECONNRESET while streaming", wantRule: "provider.network_unavailable.v1"},
		{name: "overloaded 529", provider: "opencode-acp", text: "API Error: 529 overloaded", wantRule: "anthropic.overloaded.529.v1"},
		{name: "gateway 502", provider: "opencode-acp", text: "API Error: 502 Bad Gateway", wantRule: "acp.gateway_server_failure.v1"},
		{name: "thinking blocks", provider: "opencode-acp", text: "thinking blocks cannot be modified", wantRule: resumeCorruptedRuleID},
		{name: "transport lost", provider: "opencode-acp", text: "peer disconnected", wantRule: transportLostRuleID},
		{name: "npm etarget", provider: "opencode-acp", text: "npm error code ETARGET\nnpm error notarget No matching version found for pkg@1.0.0", wantRule: "npm.etarget.managed_runtime.v1"},
		{name: "cursor retriable", provider: cursorRetriableProviderID, text: "Error: RetriableError: upstream refused the request", wantRule: cursorRetriableStreamResetRuleID},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := Input{Phase: PhasePromptSend, ProviderID: tc.provider, Stderr: tc.text}
			if !MayClassify(in) {
				t.Fatalf("MayClassify rejected text that rule %q matches: %q", tc.wantRule, tc.text)
			}
			got := Classify(in)
			if got.ClassifierRule != tc.wantRule {
				t.Errorf("Classify rule = %q, want %q (text %q)", got.ClassifierRule, tc.wantRule, tc.text)
			}
		})
	}
}

// TestMayClassifySkipsOrdinaryProse is the other half: text with no rule
// evidence must be rejected, otherwise the pre-check buys nothing.
func TestMayClassifySkipsOrdinaryProse(t *testing.T) {
	ordinary := []string{
		"",
		"the quick brown fox jumps over the lazy dog",
		"I refactored the parser and added tests for it.",
		"done: 3 changes, 128 insertions",
	}
	for _, text := range ordinary {
		in := Input{Phase: PhasePromptSend, ProviderID: "opencode-acp", Stderr: text}
		if MayClassify(in) {
			t.Errorf("MayClassify accepted ordinary text %q", text)
		}
	}
}

// TestMayClassifyKeepsStructuredInputs covers the inputs that classify without
// any text evidence.
func TestMayClassifyKeepsStructuredInputs(t *testing.T) {
	exitCode := 127
	if !MayClassify(Input{Phase: PhaseProcessStart, ExitCode: &exitCode}) {
		t.Error("MayClassify rejected an exit-code classification")
	}
	if !MayClassify(Input{Phase: PhasePromptSend, HTTPStatus: 429}) {
		t.Error("MayClassify rejected an HTTP-status classification")
	}
}
