package routingerr

import (
	"sort"
	"strings"
)

// MayClassify reports whether Classify could return anything other than its
// default, unmatched classification for this input, without running the rule
// engine. It is a conservative pre-check: a false result guarantees Classify
// would have produced its default Low-confidence error, and a true result only
// means the rules still have to run.
//
// Callers on a per-frame hot path use it to skip classification for text that
// cannot carry any rule evidence. The ACP adapter classifies the text of every
// assistant message chunk, and measured on a 3-session synthetic load that
// single call was about three quarters of agentctl's CPU on that path, so the
// classifier has to stay off the per-chunk path for ordinary prose.
//
// Structured inputs are not gated: an injected provider failure, an HTTP
// status, or an exit code classifies from the Input alone, so any of them makes
// the answer true regardless of text.
func MayClassify(in Input) bool {
	if in.HTTPStatus != 0 || in.ExitCode != nil {
		return true
	}
	if inj := getInjection(); inj != nil {
		if _, ok := inj[in.ProviderID]; ok {
			return true
		}
	}
	return mayMatchText(in.Stderr) || mayMatchText(in.Stdout)
}

// mayMatchText reports whether text contains at least one literal that every
// matching rule requires. gateTokens is the union of those literals over the
// whole catalogue, so a rule can never match text the gate rejects.
//
// The union is deliberately derived from the rule patterns themselves (see
// literalRuns) rather than hand-listed, because a hand-listed token set rots
// silently: a new rule whose literals were never added would stop being
// detected while still reading as covered. Rules that match through a custom
// matcher instead of a pattern carry their token in gateTokensByRuleID.
func mayMatchText(text string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, token := range gateTokens {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

// gateTokenMinLen is the shortest literal run that may act as a gate token.
// Runs shorter than this are single regex character classes ("[0-9a-f]") or
// fragments that appear in ordinary prose often enough to make the gate
// useless, so they are dropped; a pattern left without a usable run must
// register an explicit gateTokensByRuleID entry.
const gateTokenMinLen = 4

// gateTokens is the sorted, de-duplicated union of every rule's literal runs.
var gateTokens = buildGateTokens()

// gateTokensByRuleID supplies the gate token for rules that match through a
// custom matcher rather than a pattern, keyed by rule ID. The values are the
// literal the matcher requires the text to contain.
var gateTokensByRuleID = map[string]string{
	"opencode.service_failure.v1":      "opencode",
	"cursor.retriable_stream_reset.v1": "retriableerror",
}

func buildGateTokens() []string {
	seen := make(map[string]struct{})
	for _, rules := range providerRules {
		for _, r := range rules {
			collectRuleTokens(seen, r.id, r.pattern.String())
		}
	}
	for _, r := range providerNeutralRules {
		collectRuleTokens(seen, r.id, r.pattern.String())
	}
	for _, r := range runtimeEnvironmentRules {
		if r.pattern != nil {
			collectRuleTokens(seen, r.id, r.pattern.String())
		} else if token, ok := gateTokensByRuleID[r.id]; ok {
			seen[strings.ToLower(token)] = struct{}{}
		}
	}
	tokens := make([]string, 0, len(seen))
	for token := range seen {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	return tokens
}

func collectRuleTokens(seen map[string]struct{}, ruleID, pattern string) {
	runs := literalRuns(pattern)
	if len(runs) == 0 {
		if token, ok := gateTokensByRuleID[ruleID]; ok {
			seen[strings.ToLower(token)] = struct{}{}
		}
		return
	}
	for _, run := range runs {
		seen[run] = struct{}{}
	}
}

// literalRuns returns the lowercased alphanumeric runs of at least
// gateTokenMinLen characters that a match of pattern must contain. Escape
// sequences are skipped as units so a word boundary does not fuse with the
// literal it precedes ("\bpayment" yields "payment", not "bpayment").
func literalRuns(pattern string) []string {
	var runs []string
	var current strings.Builder
	flush := func() {
		if current.Len() >= gateTokenMinLen {
			runs = append(runs, current.String())
		}
		current.Reset()
	}
	lower := strings.ToLower(pattern)
	for i := 0; i < len(lower); i++ {
		c := lower[i]
		if c == '\\' {
			flush()
			if i+1 < len(lower) {
				i++
			}
			continue
		}
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			current.WriteByte(c)
			continue
		}
		flush()
	}
	flush()
	return runs
}
