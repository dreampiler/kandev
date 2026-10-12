package dynamic

import (
	"context"
	"strings"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

// FailureCause names why a classified provider failure happened. One cause
// decides how far a suspension reaches and how long it lasts, so two notices
// with the same wording on two paths can mean different things: OpenCode Zen's
// "Rate limit exceeded. Please try again later" is its free daily allowance,
// while the same code from another gateway is a short per-minute limit.
//
// The mapping is data: failureCauseRules picks the cause from the classifier
// rule, the code and the candidate's path, and failureCausePolicies gives each
// cause its scope and duration. A newly collected signature is one rule in
// routingerr plus one row here.
type FailureCause string

const (
	// CauseAccountQuota is an exhausted subscription or prepaid plan: every
	// model the account reaches through this path stops until the stated reset.
	CauseAccountQuota FailureCause = "account_quota"
	// CauseGoUsageLimit is the OpenCode Go subscription usage limit. It covers
	// the paid models of the account; its free models keep working.
	CauseGoUsageLimit FailureCause = "go_usage_limit"
	// CauseFreeDailyLimit is a per-model free daily allowance. It resets at
	// 00:00 UTC (09:00 KST), so a retry the same day only spends a turn.
	CauseFreeDailyLimit FailureCause = "free_daily_limit"
	// CauseFreeAccountDailyLimit is a daily allowance shared by every free model
	// of one account (OpenRouter "free-models-per-day").
	CauseFreeAccountDailyLimit FailureCause = "free_account_daily_limit"
	// CauseUsageLimit is any other rate or quota limit. It keeps the metering
	// rules that predate this table: model scope for free and OpenCode Go
	// models, account scope otherwise, and the existing ladders.
	CauseUsageLimit FailureCause = "usage_limit"
	// CauseUpstreamHostLimit is a gateway's shared upstream pool that is
	// rate-limited for one model ("temporarily rate-limited upstream").
	CauseUpstreamHostLimit FailureCause = "upstream_host_limit"
	// CauseModelWithdrawn is a model the gateway no longer serves.
	CauseModelWithdrawn FailureCause = "model_withdrawn"
	// CauseAccountSetting is an account setting that excludes the model until
	// the owner changes it.
	CauseAccountSetting FailureCause = "account_setting"
	// CauseEndpointIntermittent is one model's upstream endpoint on one gateway
	// that drops out for minutes at a time.
	CauseEndpointIntermittent FailureCause = "endpoint_intermittent"
	// CauseModelHost is a slow or failing model host. A host serves the model
	// on every path, so a second path failing within the window suspends the
	// model everywhere.
	CauseModelHost FailureCause = "model_host"
	// CauseNetwork is a connection that could not be made or was dropped.
	CauseNetwork FailureCause = "network"
	// CauseCredential is a missing, expired or unentitled credential.
	CauseCredential FailureCause = "credential"
	// CauseLocalRuntime is the local agent runtime failing. It is not a
	// provider condition and suspends nothing.
	CauseLocalRuntime FailureCause = "local_runtime"
	// CauseTransient is an unavailable, overloaded or at-capacity notice with
	// no more specific signature.
	CauseTransient FailureCause = "transient"
)

// Provider prefixes of model IDs, which name the path a candidate reaches its
// model through.
const (
	pathOpenCodeZen = "opencode"
	pathOpenRouter  = "openrouter"
)

// causeRule maps one classified failure to its cause. Rules are tried in
// order and the first one whose non-empty fields all match wins, so a
// signature row precedes the code-only fallback for the same code.
type causeRule struct {
	// rule is the routingerr classifier rule ID; empty matches any rule.
	rule string
	// code is the normalized code; empty matches any code.
	code routingerr.Code
	// path is the provider prefix of the candidate's model ID; empty matches
	// any path.
	path  string
	cause FailureCause
}

// failureCauseRules is the single cause table. Evidence for each row is the
// 2026-10-12 failover error catalogue (signature, path, recovery and
// cross-path behaviour).
var failureCauseRules = []causeRule{
	// Account allowances with a stated reset or renewal.
	{rule: routingerr.RuleOpenCodeCredit, cause: CauseAccountQuota},
	{rule: routingerr.RuleOpenCodeUsageLimit, cause: CauseAccountQuota},
	{rule: routingerr.RuleClaudeQuota, cause: CauseAccountQuota},
	{rule: routingerr.RuleClaudeSessionLimit, cause: CauseAccountQuota},
	{rule: routingerr.RuleClaudeWeeklyLimit, cause: CauseAccountQuota},
	{rule: routingerr.RuleCodexQuota, cause: CauseAccountQuota},
	{rule: routingerr.RuleProviderQuotaNotice, cause: CauseAccountQuota},
	{rule: routingerr.RuleOpenCodeGoUsageLimit, cause: CauseGoUsageLimit},
	// Free daily allowances. Zen recovered after the next 09:00 KST in 500 of
	// 506 cases; the same notice from OpenRouter came from a withdrawn model,
	// which a daily block also retries only once a day.
	{rule: routingerr.RuleOpenCodeRateTryLater, path: pathOpenCodeZen, cause: CauseFreeDailyLimit},
	{rule: routingerr.RuleOpenCodeRateTryLater, path: pathOpenRouter, cause: CauseFreeDailyLimit},
	{rule: routingerr.RuleOpenCodeFreeModelsPerDay, cause: CauseFreeAccountDailyLimit},
	{rule: routingerr.RuleOpenCodeModelPerDay, cause: CauseFreeDailyLimit},
	{rule: routingerr.RuleOpenCodeUpstreamHostLimit, cause: CauseUpstreamHostLimit},
	// TODO(provider-account-block): an account restricted by its gateway
	// (403 user_blocked) holds that execution profile until the owner releases
	// it. That cause, its classifier rule and its release live in the
	// kd/fork-provider-account-block change; it runs before this table.
	{rule: routingerr.RuleOpenCodeModelWithdrawn, cause: CauseModelWithdrawn},
	{rule: routingerr.RuleOpenCodeGoAccountSetting, cause: CauseAccountSetting},
	{rule: routingerr.RuleOpenCodeEndpointUnavailable, cause: CauseEndpointIntermittent},
	{rule: routingerr.RuleProviderHeaderTimeout, cause: CauseModelHost},
	{rule: routingerr.RuleProviderStreamServerError, cause: CauseModelHost},
	{rule: routingerr.RuleProviderInternalServerError, cause: CauseModelHost},
	{rule: routingerr.RuleProviderNvidiaHost, cause: CauseModelHost},
	{rule: routingerr.RuleOpenCodeServiceFailure, cause: CauseLocalRuntime},
	// Code defaults.
	{code: routingerr.CodeRateLimited, cause: CauseUsageLimit},
	{code: routingerr.CodeQuotaLimited, cause: CauseUsageLimit},
	{code: routingerr.CodeNetworkUnavailable, cause: CauseNetwork},
	{code: routingerr.CodeAuthRequired, cause: CauseCredential},
	{code: routingerr.CodeMissingCredentials, cause: CauseCredential},
	{code: routingerr.CodeSubscriptionRequired, cause: CauseCredential},
	{code: routingerr.CodeProviderNotConfigured, cause: CauseCredential},
	{code: routingerr.CodeModelUnavailable, cause: CauseModelWithdrawn},
	{code: routingerr.CodeProviderUnavailable, cause: CauseTransient},
	{code: routingerr.CodeProviderOverloaded, cause: CauseTransient},
	{code: routingerr.CodeModelCapacity, cause: CauseTransient},
}

// BlockScope is how far one cause's suspension reaches.
type BlockScope string

const (
	// BlockNone suspends nothing.
	BlockNone BlockScope = "none"
	// BlockPathModel suspends this model on this path only.
	BlockPathModel BlockScope = "path_model"
	// BlockPath suspends the path's credential binding.
	BlockPath BlockScope = "path"
	// BlockAccount suspends every model the path's account reaches.
	BlockAccount BlockScope = "account"
	// BlockAccountTier suspends every model of the same cost tier (free or
	// paid) on the path's account.
	BlockAccountTier BlockScope = "account_tier"
	// BlockUsageLimit is the metering rule that predates the cause table: the
	// model for providers that meter models separately, the account otherwise.
	BlockUsageLimit BlockScope = "usage_limit"
)

// causePolicy is one cause's scope and duration.
type causePolicy struct {
	scope BlockScope
	// ladder is the block length by consecutive strike; the last step repeats.
	ladder []time.Duration
	// usageLimitDuration keeps the ladders, calendar and monthly resets that
	// predate the cause table.
	usageLimitDuration bool
	// holdUntilRenewal blocks until a stated renewal instant still ahead.
	holdUntilRenewal bool
	// untilNextUTCDay blocks until the next 00:00 UTC.
	untilNextUTCDay bool
	// nextUTCDayAfterStrikes switches to the next 00:00 UTC once strikes pass
	// this count; zero never switches.
	nextUTCDayAfterStrikes int
	// crossPathWindow suspends the model on every path when another path
	// reported the same cause for the same model within the window.
	crossPathWindow time.Duration
	// excludeSameModel keeps the same model on other paths out of the
	// successor search that this failure starts.
	excludeSameModel bool
}

// Durations. A reset hint stated by the provider always comes first.
var (
	// transientLadder lengthens repeated unavailable, overloaded and
	// at-capacity notices instead of retrying every minute.
	transientLadder = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour}
	// endpointLadder: 64% of "Endpoint is unavailable" recovered on the same
	// path within ten minutes, median twelve.
	endpointLadder = []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute}
	// modelHostLadder: header timeouts and 5xx recovered in 1 to 22 minutes.
	modelHostLadder = []time.Duration{5 * time.Minute, 15 * time.Minute, 30 * time.Minute}
	// upstreamHostLadder: a shared upstream pool that stays limited for more
	// than about six hours waits for the next daily reset.
	upstreamHostLadder = []time.Duration{5 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour}
	// withdrawnLadder: no withdrawn model recovered; it is probed once a day.
	withdrawnLadder = []time.Duration{24 * time.Hour}
	// accountSettingLadder: the owner changed the setting 3 to 10 hours later.
	accountSettingLadder = []time.Duration{6 * time.Hour, 24 * time.Hour}
	// networkLadder: dropped connections recovered in 1 to 8 minutes.
	networkLadder = []time.Duration{2 * time.Minute, 5 * time.Minute}
	// goUsageLadder: the Go notice states no reset; the window is unknown.
	goUsageLadder = []time.Duration{5 * time.Hour, 24 * time.Hour, 7 * 24 * time.Hour}
)

// modelHostWindow is how close in time two paths must fail for the model host
// to be the shared cause.
const modelHostWindow = 15 * time.Minute

// upstreamHostDailyAfterStrikes is the strike count after which the upstream
// ladder has spent about six hours (5+15+30 minutes, then hourly).
const upstreamHostDailyAfterStrikes = 8

var failureCausePolicies = map[FailureCause]causePolicy{
	CauseAccountQuota:          {scope: BlockAccount, usageLimitDuration: true, holdUntilRenewal: true},
	CauseGoUsageLimit:          {scope: BlockAccountTier, ladder: goUsageLadder},
	CauseFreeDailyLimit:        {scope: BlockPathModel, untilNextUTCDay: true},
	CauseFreeAccountDailyLimit: {scope: BlockAccountTier, untilNextUTCDay: true},
	CauseUsageLimit:            {scope: BlockUsageLimit, usageLimitDuration: true},
	CauseUpstreamHostLimit:     {scope: BlockPathModel, ladder: upstreamHostLadder, nextUTCDayAfterStrikes: upstreamHostDailyAfterStrikes},
	CauseModelWithdrawn:        {scope: BlockPathModel, ladder: withdrawnLadder},
	CauseAccountSetting:        {scope: BlockPathModel, ladder: accountSettingLadder},
	CauseEndpointIntermittent:  {scope: BlockPathModel, ladder: endpointLadder},
	CauseModelHost:             {scope: BlockPathModel, ladder: modelHostLadder, crossPathWindow: modelHostWindow, excludeSameModel: true},
	CauseNetwork:               {scope: BlockPathModel, ladder: networkLadder},
	CauseCredential:            {scope: BlockPath, ladder: []time.Duration{circuitBackoff}},
	CauseLocalRuntime:          {scope: BlockNone},
	// An unrecognized unavailable notice may be the path or the host, so the
	// successor search keeps the same model out as it did before the table.
	CauseTransient: {scope: BlockPathModel, ladder: transientLadder, excludeSameModel: true},
}

// FailureCauseFor returns the cause of a classified failure on a candidate.
func FailureCauseFor(candidate Candidate, failure *routingerr.Error) FailureCause {
	if failure == nil {
		return ""
	}
	path := ProviderOf(candidate.ModelID)
	for _, row := range failureCauseRules {
		if row.rule != "" && row.rule != failure.ClassifierRule {
			continue
		}
		if row.code != "" && row.code != failure.Code {
			continue
		}
		if row.path != "" && row.path != path {
			continue
		}
		return row.cause
	}
	return CauseTransient
}

func policyForFailure(candidate Candidate, failure *routingerr.Error) causePolicy {
	return failureCausePolicies[FailureCauseFor(candidate, failure)]
}

// pathModelKey is this model on this path. A model-scoped candidate already
// carries it; any other candidate derives it from its binding.
func (c Candidate) pathModelKey() string {
	if c.ModelKey != "" {
		return c.ModelKey
	}
	if c.ModelID == "" || c.BindingKey == "" {
		return c.BindingKey
	}
	return ResourceKey(ScopeModel, c.BindingKey+modelKeySeparator+c.ModelID)
}

// accountKey is every model the candidate's account reaches through its path.
// Without a resolved account the binding is the narrowest safe answer.
func (c Candidate) accountKey() string {
	if c.AccountKey != "" {
		return c.AccountKey
	}
	return c.BindingKey
}

// accountTierKey is every model of this candidate's cost tier on its account.
func (c Candidate) accountTierKey() string {
	if c.AccountKey == "" {
		return ""
	}
	tier := "paid"
	if freeCandidate(c) {
		tier = "free"
	}
	return c.AccountKey + modelKeySeparator + tier
}

// modelIdentityKey is this model on every path.
func (c Candidate) modelIdentityKey() string {
	identity := ModelIdentity(c.ModelID)
	if identity == "" {
		return ""
	}
	return ResourceKey(ScopeModel, anyBindingMarker+modelKeySeparator+identity)
}

// pathIdentity names the path a candidate reaches its model through, so two
// profiles of one model on one path are not counted as two paths.
func (c Candidate) pathIdentity() string {
	if c.AccountKey != "" {
		return c.AccountKey
	}
	return c.BindingKey
}

const (
	modelKeySeparator = "|"
	// anyBindingMarker stands for "every binding" in a model identity key, in
	// the place a path's binding takes in a path-model key.
	anyBindingMarker = "*"
)

// modelIdentityAliases folds a model's names on different gateways into one
// identity where the plain normalization cannot see they are the same model.
var modelIdentityAliases = map[string]string{
	"nemotron-3-ultra-550b-a55b": "nemotron-3-ultra",
}

// ModelIdentity normalizes a model ID to the model a host serves, independent
// of the gateway path: "opencode-go/space-bunny-free", "opencode/space-bunny"
// and "openrouter/x/space-bunny:free" are one model. An unknown model has no
// identity.
func ModelIdentity(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if unknownInterruptionModel(model) {
		return ""
	}
	if index := strings.LastIndex(model, "/"); index >= 0 {
		model = model[index+1:]
	}
	model = strings.TrimSuffix(model, ":free")
	model = strings.TrimSuffix(model, "-free")
	if unknownInterruptionModel(model) {
		return ""
	}
	if alias, ok := modelIdentityAliases[model]; ok {
		return alias
	}
	return model
}

// failureTargets lists the circuits one failure opens.
func failureTargets(candidate Candidate, failure *routingerr.Error, policy causePolicy) []string {
	var key string
	switch policy.scope {
	case BlockNone:
		return nil
	case BlockUsageLimit:
		key = suspensionTarget(candidate, failure.Code)
	case BlockPath:
		key = candidate.BindingKey
	case BlockAccount:
		key = candidate.accountKey()
	case BlockAccountTier:
		key = candidate.accountTierKey()
		if key == "" {
			key = candidate.pathModelKey()
		}
	default:
		key = candidate.pathModelKey()
	}
	if key == "" {
		return nil
	}
	return []string{key}
}

// causeSuspensionUntil sizes one suspension of a cause. A reset instant the
// provider stated wins, then a stated renewal still ahead, then the cause's own
// rule.
func (e *Engine) causeSuspensionUntil(
	ctx context.Context,
	candidate Candidate,
	failure *routingerr.Error,
	policy causePolicy,
	strikes int,
	now time.Time,
) time.Time {
	floor := now.Add(circuitBackoff)
	if failure.ResetHint != nil && failure.ResetHint.After(floor) {
		return *failure.ResetHint
	}
	if policy.holdUntilRenewal && failure.RenewalAt != nil && failure.RenewalAt.After(floor) {
		return *failure.RenewalAt
	}
	if policy.usageLimitDuration {
		return e.suspensionUntil(ctx, candidate, failure, strikes, now)
	}
	if policy.untilNextUTCDay ||
		(policy.nextUTCDayAfterStrikes > 0 && strikes > policy.nextUTCDayAfterStrikes) {
		return laterOf(nextUTCDay(now), floor)
	}
	if len(policy.ladder) == 0 {
		return floor
	}
	return now.Add(ladderStep(policy.ladder, strikes))
}

// nextUTCDay is the next 00:00 UTC, when daily allowances reset.
func nextUTCDay(now time.Time) time.Time {
	utc := now.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day()+1, 0, 0, 0, 0, time.UTC)
}

func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// openCircuit opens one key for a failure without ever shortening a block that
// is already running.
func (e *Engine) openCircuit(
	ctx context.Context,
	key string,
	candidate Candidate,
	failure *routingerr.Error,
	policy causePolicy,
	now time.Time,
) {
	until := e.causeSuspensionUntil(ctx, candidate, failure, policy, e.circuits.NextStrike(key), now)
	if running := e.circuits.Inspect(key, now); running.State == ResourceWaiting && running.Until.After(until) {
		until = running.Until
	}
	e.circuits.Open(key, until, failure.Code)
}

// noteCrossPathFailure records a model-host failure of one path and reports
// whether another path of the same model failed within the window.
func (e *Engine) noteCrossPathFailure(candidate Candidate, window time.Duration, now time.Time) bool {
	identity := ModelIdentity(candidate.ModelID)
	path := candidate.pathIdentity()
	if identity == "" || path == "" {
		return false
	}
	e.hostMu.Lock()
	defer e.hostMu.Unlock()
	if e.hostFailures == nil {
		e.hostFailures = make(map[string]map[string]time.Time)
	}
	paths := e.hostFailures[identity]
	if paths == nil {
		paths = make(map[string]time.Time)
		e.hostFailures[identity] = paths
	}
	shared := false
	for other, at := range paths {
		if now.Sub(at) > window {
			delete(paths, other)
			continue
		}
		if other != path {
			shared = true
		}
	}
	paths[path] = now
	return shared
}

// sameModelCandidateIDs lists the other candidates of the profile that run the
// failed candidate's model on another path.
func sameModelCandidateIDs(profile Profile, failedCandidateID string) []string {
	failed, ok := candidateByID(profile, failedCandidateID)
	if !ok {
		return nil
	}
	identity := ModelIdentity(failed.ModelID)
	if identity == "" {
		return nil
	}
	var ids []string
	for _, candidate := range profile.Candidates {
		if candidate.ID != failedCandidateID && ModelIdentity(candidate.ModelID) == identity {
			ids = append(ids, candidate.ID)
		}
	}
	return ids
}

// failureExclusions is the failed candidate plus, when the cause can reach the
// model on every path, the same model's other paths.
func failureExclusions(profile Profile, failedCandidateID string, failure *routingerr.Error) []string {
	excluded := []string{failedCandidateID}
	failed, ok := candidateByID(profile, failedCandidateID)
	if !ok || failure == nil || !policyForFailure(failed, failure).excludeSameModel {
		return excluded
	}
	return append(excluded, sameModelCandidateIDs(profile, failedCandidateID)...)
}
