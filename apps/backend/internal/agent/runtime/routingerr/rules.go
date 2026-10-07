package routingerr

import "regexp"

type rule struct {
	id         string
	pattern    *regexp.Regexp
	code       Code
	confidence Confidence
}

// Provider rules match provider error signatures, never a bare topic word.
// Every error channel these rules read can also carry an agent's own words
// (a turn that ends in error reports its final text), and agents routinely
// quote and explain the errors they read; a rule that fires on "rate limit"
// or "quota" anywhere would suspend a healthy account because its agent wrote
// about another one.
const (
	// genericRateLimitSignature is the rate-limit wording shared by the
	// AI-SDK based adapters: "AI_APICallError: Rate limit exceeded. Please
	// try again later", "Rate limit exceeded: free-models-per-min",
	// "rate_limit_exceeded", "429 Too Many Requests".
	genericRateLimitSignature = `(?i)\brate[\s_-]?limit(?:ed)?\b[\s:]*(?:exceeded|reached)\b|` +
		`\brate_limit_(?:exceeded|error)\b|\btoo\s+many\s+requests\b`
	// genericQuotaSignature is exhausted-quota wording such as OpenAI's
	// "insufficient_quota" and "You exceeded your current quota".
	genericQuotaSignature = `(?i)\binsufficient_quota\b|\bquota[\s_-]+(?:exceeded|exhausted|reached)\b|` +
		`\bexceeded\s+(?:your\s+)?(?:current\s+)?quota\b|\bout\s+of\s+quota\b|\bresource_exhausted\b`
	// claudeRateLimitSignature is the Anthropic API rate-limit error as Claude
	// Code reports it ("API Error: 429 {...rate_limit_error...}", "API Error:
	// Request rejected (429)"), its second-person rate-limit notice, and a
	// "Rate limit exceeded" error line. The bare phrase counts only when it
	// opens a line: a Claude prompt error can carry the agent's final prose,
	// and prose quotes other providers' "Rate limit exceeded" mid-sentence.
	claudeRateLimitSignature = `(?im)\bAPI\s+Error:\s*(?:429\b|Request\s+rejected\s+\(429\))|\brate_limit_error\b|` +
		`\b429\s+Too\s+Many\s+Requests\b|\byou(?:['’]ve|\s+have)?\s+hit\s+(?:your|the)\s+rate[\s_-]?limit\b|` +
		`^\s*(?:Internal\s+error:\s*)?(?:API\s+Error:\s*)?rate[\s_-]?limit(?:ed)?\b[\s:]*(?:exceeded|reached)\b`
	// claudeSubscriptionSignature is a missing or lapsed plan requirement, not
	// any mention of a subscription.
	claudeSubscriptionSignature = `(?i)\brequires?\s+(?:an?\s+)?(?:active\s+)?(?:paid\s+)?claude\s+` +
		`(?:(?:pro|max|team|enterprise)\s+(?:or\s+(?:pro|max|team|enterprise)\s+)?)?subscription\b|` +
		`\bsubscription\s+(?:has\s+)?(?:expired|is\s+required|required|is\s+inactive|is\s+not\s+active|not\s+active)\b|` +
		`\bno\s+active\s+subscription\b`
)

var providerRules = map[string][]rule{
	"claude-acp": {
		mustRule("claude.stderr.quota.v1", `(?i)\banthropic_quota_exceeded\b|\bcredit\s+balance\s+is\s+too\s+low\b|\binsufficient\s+credits\b`, CodeQuotaLimited, ConfHigh),
		mustRule("claude.stderr.session_limit.v1", `(?i)\b(?:you['’]ve|you\s+have)\s+hit\s+your\s+session\s+limit\b`, CodeQuotaLimited, ConfHigh),
		mustRule("claude.stderr.rate.v1", claudeRateLimitSignature, CodeRateLimited, ConfHigh),
		// A proxy can reject every account credential before it sends a request
		// upstream. This is a hard credential condition; a retry or a switch
		// back to the same proxy cannot repair it. Require proxy_error plus the
		// proxy's explicit credential/entitlement wording to avoid treating
		// ordinary Anthropic auth messages as proxy failures.
		mustRule("claude.proxy.credentials_refused.v1", `(?i)\bproxy_error\b[^\n]{0,512}\b(?:credentials?\s+(?:were\s+)?refused|oauth\s+entitlement)\b`, CodeMissingCredentials, ConfHigh),
		mustRule("claude.stderr.auth.v1", `(?i)not authenticated|please log in|run `+"`"+`claude`+"`"+` to authenticate`, CodeAuthRequired, ConfHigh),
		mustRule("claude.stderr.subscription.v1", claudeSubscriptionSignature, CodeSubscriptionRequired, ConfMedium),
		mustRule("claude.stderr.model.v1", `(?i)model.*not found|unknown model`, CodeModelUnavailable, ConfHigh),
		mustRule("claude.stderr.notinstalled.v1", `(?i)command not found|no such file`, CodeProviderNotConfigured, ConfMedium),
	},
	"codex-acp": {
		mustRule(
			"codex.stderr.quota.v1",
			`(?i)insufficient_quota|quota_exceeded|usagelimitexceeded|(?:you['’]ve|you have)\s+hit\s+your\s+usage\s+limit`,
			CodeQuotaLimited, ConfHigh,
		),
		mustRule("codex.stderr.rate.v1", `(?i)rate_limit_exceeded|too many requests`, CodeRateLimited, ConfHigh),
		mustRule("codex.stderr.auth.v1", `(?i)invalid api key|incorrect api key|missing api key`, CodeMissingCredentials, ConfHigh),
		mustRule("codex.stderr.model.v1", `(?i)model_not_found`, CodeModelUnavailable, ConfHigh),
	},
	"opencode-acp": {
		mustRule("opencode.stderr.usage_limit.v1", `(?i)\b(?:\d+[- ]hour(?:s)?|daily|weekly|monthly)\s+usage\s+limit\s+reached\b`, CodeQuotaLimited, ConfHigh),
		// OpenCode Go reports its subscription limit through the ACP prompt
		// error without a reset time; a correlated stderr reset notice, when
		// present, still supplies the reset hint.
		mustRule("opencode.acp.go_usage_limit.v1", `(?i)\bgo\s+usage\s+limit\s+exceeded\b`, CodeQuotaLimited, ConfHigh),
		// Explicit credit exhaustion takes precedence over generic payment wording.
		mustRule("opencode.stderr.credit.v1", `(?i)\b(?:credit\s+limit\s+reached|out\s+of\s+credits?|insufficient\s+credits?|insufficient\s+balance)\b`, CodeQuotaLimited, ConfHigh),
		mustRule("opencode.stderr.subscription.v1", `(?i)\bpayment\s+required\b`, CodeSubscriptionRequired, ConfHigh),
		mustRule("opencode.stderr.quota.v1", genericQuotaSignature, CodeQuotaLimited, ConfMedium),
		mustRule("opencode.stderr.rate.v1", genericRateLimitSignature, CodeRateLimited, ConfHigh),
		mustRule("opencode.stderr.auth.v1", `(?i)unauthorized|invalid token`, CodeAuthRequired, ConfHigh),
	},
	"copilot-acp": {
		mustRule("copilot.stderr.subscription.v1", `(?i)not entitled|subscription required|copilot is disabled`, CodeSubscriptionRequired, ConfHigh),
		mustRule("copilot.stderr.auth.v1", `(?i)please.*sign in|gh auth login`, CodeAuthRequired, ConfHigh),
		mustRule("copilot.stderr.rate.v1", genericRateLimitSignature, CodeRateLimited, ConfHigh),
	},
	"amp-acp": {
		mustRule("amp.stderr.auth.v1", `(?i)unauthorized|invalid token`, CodeAuthRequired, ConfHigh),
		mustRule("amp.stderr.rate.v1", genericRateLimitSignature, CodeRateLimited, ConfHigh),
		mustRule("amp.stderr.quota.v1", genericQuotaSignature, CodeQuotaLimited, ConfMedium),
	},
}

func mustRule(id, pat string, code Code, conf Confidence) rule {
	return rule{id: id, pattern: regexp.MustCompile(pat), code: code, confidence: conf}
}

// HasProviderRules reports whether providerID is a rules catalogue key (an
// agent ID such as "opencode-acp"). Diagnostics can carry a different
// provider identity, e.g. OpenCode's model-provider ID "opencode-go", which
// must not be used for rule lookup.
func HasProviderRules(providerID string) bool {
	_, ok := providerRules[providerID]
	return ok
}

func matchProviderRules(providerID, text string) (*Error, bool) {
	rules, ok := providerRules[providerID]
	if !ok || text == "" {
		return nil, false
	}
	for _, r := range rules {
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
