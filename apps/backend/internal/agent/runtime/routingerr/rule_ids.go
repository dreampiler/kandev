package routingerr

// Rule identifiers that routing reads after classification. The classifier
// owns what each signature looks like; dynamic routing decides, per rule, why
// the failure happened and how far and how long it suspends a resource (see
// dynamic.failureCauseRules). Naming the rules here keeps both sides on one
// spelling, so a renamed rule cannot silently fall back to the code default.
const (
	// RuleOpenCodeRateTryLater is the AI-SDK "Rate limit exceeded. Please try
	// again later" notice, optionally wrapped as "Error from provider (<host>):".
	// OpenCode Zen sends it for its free daily allowance.
	RuleOpenCodeRateTryLater = "opencode.rate.try_later.v1"
	// RuleOpenCodeFreeModelsPerDay is OpenRouter's account-wide free-model daily
	// allowance ("free-models-per-day", optionally "-high-balance").
	RuleOpenCodeFreeModelsPerDay = "opencode.rate.free_models_per_day.v1"
	// RuleOpenCodeModelPerDay is OpenRouter's per-model daily allowance
	// ("limit_rpd/<model>").
	RuleOpenCodeModelPerDay = "opencode.rate.model_per_day.v1"
	// RuleOpenCodeUpstreamHostLimit is a shared upstream host pool that is
	// rate-limited ("<model> is temporarily rate-limited upstream").
	RuleOpenCodeUpstreamHostLimit = "opencode.rate.upstream_host.v1"
	// RuleOpenCodeModelWithdrawn is a model the gateway no longer serves:
	// "Not Found", "No endpoints found for <model>", "Model is unavailable".
	RuleOpenCodeModelWithdrawn = "opencode.model_withdrawn.v1"
	// RuleOpenCodeGoAccountSetting is an OpenCode Go workspace privacy setting
	// that excludes the model (Global regions, training endpoints).
	RuleOpenCodeGoAccountSetting = "opencode.go_account_setting.v1"
	// RuleOpenCodeEndpointUnavailable is "Upstream request failed: Endpoint is
	// unavailable": one model's upstream endpoint on one gateway is briefly down.
	RuleOpenCodeEndpointUnavailable = "opencode.upstream_endpoint_unavailable.v1"
	// RuleOpenCodeServiceFailure is the local OpenCode service failing, not a
	// provider.
	RuleOpenCodeServiceFailure = "opencode.service_failure.v1"
	// RuleOpenCodeGoUsageLimit is the OpenCode Go subscription usage limit.
	RuleOpenCodeGoUsageLimit = "opencode.acp.go_usage_limit.v1"
	// RuleOpenCodeCredit is an exhausted prepaid credit or plan allowance.
	RuleOpenCodeCredit = "opencode.stderr.credit.v1"
	// RuleOpenCodeUsageLimit is a stated N-hour/daily/weekly/monthly usage limit.
	RuleOpenCodeUsageLimit = "opencode.stderr.usage_limit.v1"

	// RuleProviderHeaderTimeout is a model host that never sent response
	// headers (ProviderHeaderTimeoutError).
	RuleProviderHeaderTimeout = "provider.header_timeout.v1"
	// RuleProviderStreamServerError is a streamed response that a model host
	// ended with a 5xx (idle timeout, empty response, upstream server error).
	RuleProviderStreamServerError = "provider.stream_server_error.v1"
	// RuleProviderInternalServerError is a bare AI-SDK "Internal Server Error"
	// from the model host.
	RuleProviderInternalServerError = "provider.internal_server_error.v1"
	// RuleProviderNvidiaHost is NVIDIA-hosted model capacity or readiness.
	RuleProviderNvidiaHost = "provider.nvidia_host.v1"
	// RuleProviderCannotConnect is the AI-SDK "Cannot connect to API" family.
	RuleProviderCannotConnect = "provider.cannot_connect.v1"
	// RuleProviderModelCapacity is a selected model at capacity.
	RuleProviderModelCapacity = "provider.model_capacity.v1"
	// RuleProviderQuotaNotice is a period quota notice with a stated reset.
	RuleProviderQuotaNotice = "provider.quota_notice.v1"

	// RuleClaudeQuota, RuleClaudeSessionLimit and RuleClaudeWeeklyLimit are the
	// Claude subscription limits; RuleCodexQuota is the Codex usage limit.
	RuleClaudeQuota        = "claude.stderr.quota.v1"
	RuleClaudeSessionLimit = "claude.stderr.session_limit.v1"
	RuleClaudeWeeklyLimit  = "claude.stderr.weekly_limit.v1"
	RuleCodexQuota         = "codex.stderr.quota.v1"
)
