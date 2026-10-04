package routingerr

import (
	"regexp"
	"regexp/syntax"
	"strings"
	"testing"
)

// TestEveryRuleIsGateable is the guard that keeps MayClassify conservative. A
// rule the gate cannot cover would silently stop matching once callers adopt the
// pre-check, so every rule must contribute at least one token: derived from the
// literals its matches require, or declared for a custom matcher and for
// patterns with a path that requires no usable literal.
func TestEveryRuleIsGateable(t *testing.T) {
	assertGateable := func(ruleID string, tokens []string) {
		t.Helper()
		if len(tokens) > 0 {
			return
		}
		if len(gateTokensByRuleID[ruleID]) == 0 {
			t.Errorf("rule %q contributes no gate token: declare one per match path in gateTokensByRuleID", ruleID)
		}
	}

	for providerID, rules := range providerRules {
		for _, r := range rules {
			assertGateable(providerID+"/"+r.id, patternGateTokens(r.pattern))
		}
	}
	for _, r := range providerNeutralRules {
		assertGateable(r.id, patternGateTokens(r.pattern))
	}
	for _, r := range runtimeEnvironmentRules {
		assertGateable(r.id, patternGateTokens(r.pattern))
	}
}

// TestGateTokensComeFromRequiredLiterals pins the derivation itself. Harvesting
// every literal run in a pattern is not conservative: a rule whose only long run
// sits in an optional group or one alternation branch still matches text that
// lacks it, so such a pattern must contribute nothing and be declared instead.
func TestGateTokensComeFromRequiredLiterals(t *testing.T) {
	cases := []struct {
		name      string
		pattern   string
		wantGate  bool
		wantMatch string
	}{
		{name: "required literal", pattern: `(?i)verylongtoken`, wantGate: true},
		{name: "both branches required", pattern: `(?i)firstlongtoken|secondlongtoken`, wantGate: true},
		{name: "optional tail", pattern: `(?i)verylongtoken|otherlongtoken?`, wantGate: true},
		{name: "short alternation", pattern: `(?i)ab|verylongtoken`, wantGate: false, wantMatch: "ab"},
		{name: "optional group", pattern: `(?i)x(?:verylongtoken)?y`, wantGate: false, wantMatch: "xy"},
		{name: "optional repeat", pattern: `(?i)x(?:verylongtoken)*y`, wantGate: false, wantMatch: "xy"},
		{name: "optional branch", pattern: `(?i)(?:verylongtoken|otherlongtoken)?`, wantGate: false, wantMatch: ""},
		{name: "only short literals", pattern: `(?i)ab|cd`, wantGate: false, wantMatch: "ab"},
		{name: "character classes only", pattern: `(?i)[0-9a-f]{8}-[0-9a-f]{4}`, wantGate: false, wantMatch: "0123abcd-4567"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tokens := patternGateTokens(regexp.MustCompile(tc.pattern))
			if got := len(tokens) > 0; got != tc.wantGate {
				t.Fatalf("patternGateTokens(%q) gateable = %v, want %v (tokens %v)", tc.pattern, got, tc.wantGate, tokens)
			}
			if tc.wantMatch == "" {
				return
			}
			if !regexp.MustCompile(tc.pattern).MatchString(tc.wantMatch) {
				t.Fatalf("fixture %q does not match pattern %q", tc.wantMatch, tc.pattern)
			}
			if containsToken(tokens, tc.wantMatch) {
				t.Fatalf("match %q carries a gate token %v, so the case proves nothing", tc.wantMatch, tokens)
			}
		})
	}
}

// TestGateTokensStayOutOfOrdinaryProse guards the other half of the trade: a
// required literal that appears in everyday text makes the gate accept
// everything and the pre-check buys nothing. This is a ratchet on the current
// catalogue, not a ban on a specific word.
func TestGateTokensStayOutOfOrdinaryProse(t *testing.T) {
	ordinary := []string{
		"the quick brown fox jumps over the lazy dog",
		"I refactored the parser and added tests for it.",
		"done: 3 changes, 128 insertions",
	}
	for _, text := range ordinary {
		if mayMatchText(text) {
			t.Errorf("gate accepted ordinary text %q", text)
		}
	}
}

// TestDeclaredGateTokensAreReal guards the hand-declared tokens for custom
// matchers: the token must be a literal the matcher actually requires, so a
// renamed control frame cannot leave the gate matching nothing.
func TestDeclaredGateTokensAreReal(t *testing.T) {
	tokens, ok := gateTokensByRuleID["cursor.retriable_stream_reset.v1"]
	if !ok {
		t.Fatal("cursor retriable rule has no declared gate token")
	}
	for _, token := range tokens {
		if !strings.Contains(strings.ToLower(cursorRetriableStreamResetPrefix), token) {
			t.Errorf("cursor gate token %q is not part of the required prefix %q", token, cursorRetriableStreamResetPrefix)
		}
	}
	tokens, ok = gateTokensByRuleID["opencode.service_failure.v1"]
	if !ok {
		t.Fatal("opencode service failure rule has no declared gate token")
	}
	for _, token := range tokens {
		if !strings.Contains(token, "opencode") {
			t.Errorf("opencode gate token %q does not cover the matched service-failure wording", token)
		}
	}
}

// TestMayClassifyAcceptsEveryRulePath is the behavioural half of the guarantee:
// for every path a pattern can take, text that takes it must survive the
// pre-check. Witnesses are generated from the same syntax tree the token
// derivation reads, so a pattern whose gate tokens stop covering one of its own
// branches fails here.
func TestMayClassifyAcceptsEveryRulePath(t *testing.T) {
	for providerID, rules := range providerRules {
		for _, r := range rules {
			assertGateAcceptsPaths(t, providerID, r.id, r.pattern)
		}
	}
	for _, r := range providerNeutralRules {
		assertGateAcceptsPaths(t, "opencode-acp", r.id, r.pattern)
	}
	for _, r := range runtimeEnvironmentRules {
		if r.pattern == nil {
			continue
		}
		assertGateAcceptsPaths(t, r.providerID, r.id, r.pattern)
	}
}

func assertGateAcceptsPaths(t *testing.T, providerID, ruleID string, pattern *regexp.Regexp) {
	t.Helper()
	witnesses := rulePathWitnesses(t, ruleID, pattern)
	if len(witnesses) == 0 {
		t.Fatalf("rule %q has no path that could be witnessed", ruleID)
	}
	for _, witness := range witnesses {
		in := Input{Phase: PhasePromptSend, ProviderID: providerID, Stderr: witness}
		if !MayClassify(in) {
			t.Errorf("MayClassify rejected %q, which rule %q matches", witness, ruleID)
		}
	}
}

// rulePathWitnesses returns one text per top-level alternation path, each
// asserted to match pattern. Nested branches take their first alternative; the
// gate has to cover those too, and the derivation unions every branch it finds.
func rulePathWitnesses(t *testing.T, ruleID string, pattern *regexp.Regexp) []string {
	t.Helper()
	re, err := syntax.Parse(pattern.String(), syntax.Perl)
	if err != nil {
		t.Fatalf("rule %q does not parse: %v", ruleID, err)
	}
	paths := []*syntax.Regexp{re}
	if re.Op == syntax.OpAlternate {
		paths = re.Sub
	}
	var witnesses []string
	for _, path := range paths {
		witness, ok := matchableWitness(path, pattern)
		if !ok {
			t.Errorf("rule %q pattern %q has no witness this generator could build", ruleID, pattern)
			continue
		}
		witnesses = append(witnesses, witness)
	}
	return witnesses
}

// matchableWitness builds a text for one path and returns the first variant the
// real pattern accepts.
func matchableWitness(path *syntax.Regexp, pattern *regexp.Regexp) (string, bool) {
	var fallback string
	for _, fill := range []string{"", " ", "\n"} {
		var b strings.Builder
		writeWitness(&b, path, fill)
		candidate := b.String()
		if fallback == "" {
			fallback = candidate
		}
		if candidate != "" && pattern.MatchString(candidate) {
			return candidate, true
		}
	}
	return fallback, fallback != "" && pattern.MatchString(fallback)
}

// writeWitness writes the shortest text a syntax tree can accept: the pattern's
// own literals, the first printable rune of a character class, the first
// alternative of a branch, and one instance for a repetition that may be
// omitted. fill is what an optional or unbounded element contributes, because
// skipping it can fuse two literals into one word and filling it with a space
// can break a line anchor the pattern requires.
func writeWitness(b *strings.Builder, re *syntax.Regexp, fill string) {
	switch re.Op {
	case syntax.OpLiteral:
		b.WriteString(string(re.Rune))
	case syntax.OpCharClass:
		b.WriteString(classWitnessText(re))
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		b.WriteString("x")
	case syntax.OpCapture, syntax.OpPlus:
		writeWitness(b, re.Sub[0], fill)
	case syntax.OpConcat:
		for _, sub := range re.Sub {
			writeWitness(b, sub, fill)
		}
	case syntax.OpAlternate:
		writeWitness(b, re.Sub[0], fill)
	case syntax.OpRepeat:
		if re.Min >= 1 {
			writeWitness(b, re.Sub[0], fill)
			return
		}
		if re.Max != 0 {
			b.WriteString(fill)
		}
	case syntax.OpStar, syntax.OpQuest:
		b.WriteString(fill)
	}
}

// classWitnessText picks a printable rune the character class accepts. The
// class is stored as inclusive ranges, already complemented where the pattern
// negates it, so scanning for the first accepted printable rune covers both.
func classWitnessText(re *syntax.Regexp) string {
	for r := rune(0x20); r < 0x7f; r++ {
		if classContainsRune(re.Rune, r) {
			return string(r)
		}
	}
	return ""
}

func classContainsRune(ranges []rune, target rune) bool {
	for i := 0; i+1 < len(ranges); i += 2 {
		if ranges[i] <= target && target <= ranges[i+1] {
			return true
		}
	}
	return false
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

func containsToken(tokens []string, text string) bool {
	lower := strings.ToLower(text)
	for _, token := range tokens {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}
