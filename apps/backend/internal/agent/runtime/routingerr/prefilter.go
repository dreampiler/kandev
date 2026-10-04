package routingerr

import (
	"regexp"
	"regexp/syntax"
	"sort"
	"strings"
	"unicode"
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
// The guarantee covers Stderr and Stdout only. Structured evidence is never
// gated: an injected provider failure, an HTTP status, or an exit code
// classifies from the Input alone, so any of them makes the answer true
// regardless of text.
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

// mayMatchText reports whether text contains at least one literal that some
// matching rule requires. gateTokens is the union of those literals over the
// whole catalogue, so a rule can never match text the gate rejects.
//
// The union is deliberately derived from the rule patterns themselves (see
// patternGateTokens) rather than hand-listed, because a hand-listed token set
// rots silently: a new rule whose literals were never added would stop being
// detected while still reading as covered. Rules that match through a custom
// matcher instead of a pattern carry their tokens in gateTokensByRuleID.
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

// gateTokens is the sorted, de-duplicated union of every rule's required
// literals.
var gateTokens = buildGateTokens()

// gateTokensByRuleID supplies gate tokens for the cases a pattern cannot cover:
// rules that match through a custom matcher, and patterns whose every match path
// still leaves the longest required run below gateTokenMinLen. Each entry lists
// one token per alternative match path, and a match must contain the token of
// the path it took, so a single token cannot stand in for several branches.
var gateTokensByRuleID = map[string][]string{
	"opencode.service_failure.v1":      {"opencode"},
	"cursor.retriable_stream_reset.v1": {"retriableerror"},
}

func buildGateTokens() []string {
	seen := make(map[string]struct{})
	for _, rules := range providerRules {
		for _, r := range rules {
			collectRuleTokens(seen, r.id, r.pattern)
		}
	}
	for _, r := range providerNeutralRules {
		collectRuleTokens(seen, r.id, r.pattern)
	}
	for _, r := range runtimeEnvironmentRules {
		collectRuleTokens(seen, r.id, r.pattern)
	}
	tokens := make([]string, 0, len(seen))
	for token := range seen {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	return tokens
}

func collectRuleTokens(seen map[string]struct{}, ruleID string, pattern *regexp.Regexp) {
	tokens := patternGateTokens(pattern)
	if len(tokens) == 0 {
		tokens = gateTokensByRuleID[ruleID]
	}
	for _, token := range tokens {
		seen[strings.ToLower(token)] = struct{}{}
	}
}

// patternGateTokens returns every literal the gate may look for on behalf of
// pattern, or nil when no such cover exists: a custom matcher, or a pattern with
// a match path that requires no usable literal.
//
// Deriving this per required literal rather than per pattern is what keeps the
// gate conservative. Harvesting every run in a pattern is not enough: for
// `x(?:verylongtoken)?y` or `ab|verylongtoken` only "verylongtoken" is long
// enough to gate with, yet "xy" and "ab" match without it, so a pattern-wide
// harvest would reject real matches. A pattern with such a path must declare
// its tokens in gateTokensByRuleID, one per path.
func patternGateTokens(pattern *regexp.Regexp) []string {
	terms := patternRequiredTerms(pattern)
	if len(terms) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	for _, term := range terms {
		// Every token of a term is guaranteed present together, so keeping the
		// longest is still a cover and leaves the global token set smaller,
		// which is what makes the gate reject ordinary prose.
		seen[longestToken(term)] = struct{}{}
	}
	tokens := make([]string, 0, len(seen))
	for token := range seen {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	return tokens
}

func patternRequiredTerms(pattern *regexp.Regexp) [][]string {
	if pattern == nil {
		return nil
	}
	re, err := syntax.Parse(pattern.String(), syntax.Perl)
	if err != nil {
		return nil
	}
	return requiredTerms(mergeAdjacentLiterals(re))
}

// mergeAdjacentLiterals joins literal siblings of a concatenation. The parser
// factors alternations around their shared parts, so `invalid|incorrect` arrives
// as a leading "i" followed by two literal branches; merging first keeps the
// derived tokens whole words instead of fragments like "ncorrect".
func mergeAdjacentLiterals(re *syntax.Regexp) *syntax.Regexp {
	if re.Op != syntax.OpConcat || len(re.Sub) == 0 {
		for i, sub := range re.Sub {
			if merged := mergeAdjacentLiterals(sub); merged != sub {
				re.Sub[i] = merged
			}
		}
		return re
	}
	merged := &syntax.Regexp{Op: re.Op, Flags: re.Flags, Min: re.Min, Max: re.Max}
	appendMerged := func(sub *syntax.Regexp) {
		if sub.Op == syntax.OpLiteral && len(merged.Sub) > 0 && merged.Sub[len(merged.Sub)-1].Op == syntax.OpLiteral {
			last := merged.Sub[len(merged.Sub)-1]
			merged.Sub[len(merged.Sub)-1] = &syntax.Regexp{
				Op:    syntax.OpLiteral,
				Flags: last.Flags | sub.Flags,
				Rune:  append(append([]rune{}, last.Rune...), sub.Rune...),
			}
			return
		}
		merged.Sub = append(merged.Sub, sub)
	}
	for _, sub := range re.Sub {
		appendMerged(mergeAdjacentLiterals(sub))
	}
	return merged
}

// maxRequiredTerms bounds the disjunctive form. Crossing a concatenation with
// several alternations grows exponentially, and a rule that wide cannot be
// declared either, so the derivation gives up and the guard test demands a
// narrower pattern.
const maxRequiredTerms = 64

// requiredTerms returns the literals every match of re is guaranteed to carry,
// as a disjunction of conjunctions: a match contains every token of at least one
// returned term. An empty result means no match path requires a usable literal.
func requiredTerms(re *syntax.Regexp) [][]string {
	switch re.Op {
	case syntax.OpLiteral:
		if token := longestLiteralRun(re.Rune); token != "" {
			return [][]string{{token}}
		}
	case syntax.OpCapture:
		if len(re.Sub) == 1 {
			return requiredTerms(re.Sub[0])
		}
	case syntax.OpConcat:
		return concatTerms(re.Sub)
	case syntax.OpAlternate:
		// A match takes exactly one branch, so an unguarded branch would let a
		// match through with no token at all.
		var terms [][]string
		for _, sub := range re.Sub {
			branch := requiredTerms(sub)
			if len(branch) == 0 {
				return nil
			}
			terms = append(terms, branch...)
		}
		return terms
	case syntax.OpPlus, syntax.OpRepeat:
		// A quantified element with Min < 1 is optional; from Min on, the
		// element's own requirement holds.
		if re.Op == syntax.OpPlus || re.Min >= 1 {
			return requiredTerms(re.Sub[0])
		}
	}
	return nil
}

// concatTerms combines the requirements of a concatenation: every element that
// requires a literal does so simultaneously, so the terms are crossed and the
// unrepresentable product is reported as an absence rather than approximated.
func concatTerms(subs []*syntax.Regexp) [][]string {
	terms := [][]string{{}}
	required := false
	for _, sub := range subs {
		element := requiredTerms(sub)
		if len(element) == 0 {
			continue
		}
		crossed := crossTerms(terms, element)
		if len(crossed) == 0 {
			return nil
		}
		terms = crossed
		required = true
	}
	if !required {
		return nil
	}
	return terms
}

func crossTerms(terms, elements [][]string) [][]string {
	if len(terms)*len(elements) > maxRequiredTerms {
		return nil
	}
	crossed := make([][]string, 0, len(terms)*len(elements))
	for _, term := range terms {
		for _, element := range elements {
			crossed = append(crossed, mergeTokens(term, element))
		}
	}
	return crossed
}

func longestToken(tokens []string) string {
	longest := ""
	for _, token := range tokens {
		if len(token) > len(longest) {
			longest = token
		}
	}
	return longest
}

func mergeTokens(term, element []string) []string {
	merged := make([]string, 0, len(term)+len(element))
	seen := make(map[string]struct{}, len(term)+len(element))
	for _, token := range append(append([]string{}, term...), element...) {
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		merged = append(merged, token)
	}
	sort.Strings(merged)
	return merged
}

// longestLiteralRun returns the longest alphanumeric run of at least
// gateTokenMinLen characters in runes, lowercased. Runs shorter than that are
// fragments that ordinary prose contains too often to gate with.
func longestLiteralRun(runes []rune) string {
	best := ""
	var current strings.Builder
	flush := func() {
		if current.Len() > len(best) {
			best = current.String()
		}
		current.Reset()
	}
	for _, r := range runes {
		if !isLiteralRune(r) {
			flush()
			continue
		}
		current.WriteRune(unicode.ToLower(r))
	}
	flush()
	if len(best) < gateTokenMinLen {
		return ""
	}
	return best
}

func isLiteralRune(r rune) bool {
	return r == '_' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}
