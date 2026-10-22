package routingerr

import (
	"regexp"
	"strings"
	"unicode"
)

// ClassifyAgentNotice classifies assistant-message text that may itself be a
// provider's failure notice. It returns nil when the text is not a notice.
//
// Assistant text is the agent's own output, not a provider error channel: an
// agent routinely quotes, summarizes, and explains errors it has read, so a
// provider signature somewhere inside its prose says nothing about the
// agent's own provider. A notice therefore qualifies only when the text begins
// with a catalogue signature — the output is the notice itself, as when a
// provider states an exhausted quota as its whole reply. Text that merely
// mentions a limit is left to the real error channels (the prompt RPC error
// and stderr), which the full Classify reads.
func ClassifyAgentNotice(in Input) *Error {
	text := strings.TrimLeftFunc(in.Stderr, unicode.IsSpace)
	if text == "" {
		return nil
	}
	r, ok := leadingNoticeRule(in.ProviderID, text)
	if !ok {
		return nil
	}
	e := applyInvariants(&Error{
		Code:           r.code,
		Confidence:     r.confidence,
		Phase:          in.Phase,
		ClassifierRule: r.id,
		ResetHint:      in.ResetHint,
		RawExcerpt:     Sanitize(text),
	})
	deriveLimitTiming(e, Input{Stderr: text, OccurredAt: in.OccurredAt})
	return e
}

// noticeErrorLabel is the error-type label a provider error line may open with,
// such as "AI_APICallError: " or "Error: ".
var noticeErrorLabel = regexp.MustCompile(`(?i)^[a-z0-9_]*error:\s*`)

// leadingNoticeRule returns the first catalogue rule whose match starts at the
// beginning of text, or right after a leading error-type label, honoring the
// same provider-then-neutral precedence as Classify.
func leadingNoticeRule(providerID, text string) (rule, bool) {
	starts := []int{0}
	if label := noticeErrorLabel.FindStringIndex(text); label != nil {
		starts = append(starts, label[1])
	}
	for _, rules := range [][]rule{providerRules[providerID], providerNeutralRules} {
		for _, r := range rules {
			if matchesAtAnyStart(r.pattern, text, starts) {
				return r, true
			}
		}
	}
	return rule{}, false
}

// matchesAtAnyStart reports whether pattern can match beginning at one of the
// given offsets. The leftmost match in text[start:] starts at zero exactly
// when any match there can, so one search per offset answers it.
func matchesAtAnyStart(pattern *regexp.Regexp, text string, starts []int) bool {
	for _, start := range starts {
		if loc := pattern.FindStringIndex(text[start:]); loc != nil && loc[0] == 0 {
			return true
		}
	}
	return false
}
