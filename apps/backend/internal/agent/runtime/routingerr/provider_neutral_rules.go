package routingerr

// providerNeutralRules contain stable evidence that can be emitted by more
// than one adapter. Keep these rules independent from provider IDs so adding a
// new ACP provider does not require copying the same transient fingerprints.
var providerNeutralRules = []rule{
	// Some providers announce an exhausted quota as an ordinary assistant
	// message and then never settle the prompt, so the notice is the only
	// failure signal. The high-confidence rule is anchored to the whole text and
	// requires the provider's own notice wording, because a rule that ends a
	// live turn on the bare phrase "usage limit reached" would also match an
	// agent quoting or explaining that phrase — and an ACP terminal error
	// carrying only that string must stay a generic failure. The loose rule
	// keeps such prose classifiable for diagnostics, but at a confidence that
	// never ends a turn.
	mustRule(
		"provider.quota_notice.v1",
		`(?is)^\s*(?:usage\s+limit\s+reached[.!]?\s*)?you have reached your current quota for this period\.\s*your limit will reset in\s+\d`,
		CodeQuotaLimited,
		ConfHigh,
	),
	mustRule(
		"provider.quota_notice_prose.v1",
		`(?i)\breached\s+your\s+current\s+quota\s+for\s+this\s+period\b`,
		CodeQuotaLimited,
		ConfMedium,
	),
	mustRule(
		"provider.model_capacity.v1",
		`(?i)(?:\bselected\s+model\b[^\n]{0,96}\bat\s+capacity\b|\bmodel\b[^\n]{0,96}\bat\s+capacity\b|\b(?:model|provider)[_-]?capacity(?:_reached)?\b|\b(?:model|provider)\s+capacity\s+(?:is\s+)?(?:reached|unavailable)\b)`,
		CodeModelCapacity,
		ConfHigh,
	),
	mustRule(
		"provider.network_unavailable.v1",
		`(?i)(?:\b(?:ECONNRESET|ECONNREFUSED|ENETUNREACH|EHOSTUNREACH|ETIMEDOUT|ENOTFOUND)\b|network\s+is\s+unreachable|no\s+route\s+to\s+host|connection\s+(?:reset|refused|timed\s+out)|temporary\s+failure\s+in\s+name\s+resolution|i/o\s+timeout|failed\s+to\s+fetch)`,
		CodeNetworkUnavailable,
		ConfHigh,
	),
	mustRule(
		"provider.unavailable.v1",
		`(?i)(?:\b(?:provider|service|upstream|gateway)\b[^\n]{0,64}\b(?:temporarily\s+)?unavailable\b|\btemporarily\s+unavailable\b)`,
		CodeProviderUnavailable,
		ConfMedium,
	),
}

func matchProviderNeutralRules(text string) (*Error, bool) {
	if text == "" {
		return nil, false
	}
	for _, r := range providerNeutralRules {
		if r.pattern.MatchString(text) {
			return &Error{
				Code:           r.code,
				Confidence:     r.confidence,
				ClassifierRule: r.id,
			}, true
		}
	}
	return nil, false
}
