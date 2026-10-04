package dynamic

import (
	"context"
	"strings"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

// SuspensionScope names the resource a usage-limit suspension pauses.
type SuspensionScope string

const (
	// SuspensionScopeCredential pauses every model reached through one account.
	SuspensionScopeCredential SuspensionScope = "credential"
	// SuspensionScopeModel pauses one model on one account. Providers that meter
	// each model separately use it, so a limit on one model never pauses its
	// siblings, and a free model never shares a paid model's suspension.
	SuspensionScopeModel SuspensionScope = "model"
	// SuspensionScopeProvider pauses every paid model of one provider. Only an
	// operator-entered block uses it.
	SuspensionScopeProvider SuspensionScope = "provider"
)

// SuspensionSource tells whether a suspension came from a routing failure or
// from an operator-entered block.
type SuspensionSource string

const (
	SuspensionSourceFailure SuspensionSource = "failure"
	SuspensionSourceManual  SuspensionSource = "manual"
)

const providerOpenCodeGo = "opencode-go"

// paidUnknownResetLadder is the block length for consecutive paid-account limit
// failures whose reset instant is unknown: two hours three times, then a day.
// Strikes past the ladder wait for the next monthly reset, or repeat the last
// step when that reset is unknown.
//
// It applies to every paid provider, not only OpenCode Go. A paid account that
// cannot say when its capacity returns recovers on its own clock, so a one
// minute retry only spends the whole ladder again from the start.
var paidUnknownResetLadder = []time.Duration{2 * time.Hour, 2 * time.Hour, 2 * time.Hour, 24 * time.Hour}

// freeRateLimitLadder is the block length for consecutive free-model rate limit
// failures. A free tier's own limits are short and repeat often, so the ladder
// starts at the ordinary backoff and reaches a four hour ceiling rather than a
// day: a free model that stays limited should not be written off for longer than
// a paid one, and a success returns it to the first step.
//
// Concurrent-run limits arrive as the same notice as per-minute rate limits, so
// both follow this ladder.
var freeRateLimitLadder = []time.Duration{
	time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 4 * time.Hour,
}

// limitPolicy is the scope and duration rule for usage-limit failures of one
// model.
type limitPolicy struct {
	modelScoped bool
	ladder      []time.Duration
	// untilMonthlyReset extends strikes past the ladder to the next monthly
	// reset.
	untilMonthlyReset bool
}

// ProviderOf returns the provider prefix of a provider-qualified model ID such
// as "opencode-go/kimi-k2". An unqualified model has no provider.
func ProviderOf(modelID string) string {
	provider, _, found := strings.Cut(strings.TrimSpace(modelID), "/")
	if !found {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(provider))
}

// IsFreeModel reports whether a model ID names a free model. Free models carry
// their own per-model limits, so they are suspended separately from the paid
// models of the same account.
func IsFreeModel(modelID string) bool {
	normalized := strings.ToLower(strings.TrimSpace(modelID))
	return strings.HasSuffix(normalized, ":free") || strings.HasSuffix(normalized, "-free") ||
		strings.HasSuffix(normalized, "/free")
}

func policyForModel(modelID string) limitPolicy {
	if IsFreeModel(modelID) {
		return limitPolicy{modelScoped: true, ladder: freeRateLimitLadder}
	}
	// OpenCode Go meters and reports its limit per model, so its suspension
	// stays model scoped while its durations are the paid ladder.
	if ProviderOf(modelID) == providerOpenCodeGo {
		return limitPolicy{modelScoped: true, ladder: paidUnknownResetLadder, untilMonthlyReset: true}
	}
	// Every other paid provider meters the account, so the suspension reaches
	// the whole credential and follows the same ladder.
	return limitPolicy{ladder: paidUnknownResetLadder, untilMonthlyReset: true}
}

// ModelScoped reports whether usage-limit failures of this model suspend only
// the model rather than the whole account.
func ModelScoped(modelID string) bool { return policyForModel(modelID).modelScoped }

// isUsageLimitCode is the set of failures that report exhausted capacity rather
// than a broken account. Only these follow the model scope and the ladder.
func isUsageLimitCode(code routingerr.Code) bool {
	return code == routingerr.CodeQuotaLimited || code == routingerr.CodeRateLimited
}

// LimitCalendar answers the reset instants a suspension may wait for. Both
// answers are optional: an unknown reset keeps the ladder's duration.
type LimitCalendar interface {
	// ExhaustedUntil reports the latest reset among the candidate's observed
	// usage windows that are fully consumed.
	ExhaustedUntil(ctx context.Context, candidate Candidate, now time.Time) (time.Time, bool)
	// MonthlyReset reports the next monthly reset of the candidate's provider,
	// from observed usage when available and otherwise from operator settings.
	MonthlyReset(ctx context.Context, candidate Candidate, now time.Time) (time.Time, bool)
}

// WithLimitCalendar injects the reset source used to size usage-limit
// suspensions.
func WithLimitCalendar(calendar LimitCalendar) EngineOption {
	return func(engine *Engine) { engine.calendar = calendar }
}

// suspensionTarget returns the circuit key a failure pauses for the candidate.
// An exhausted capacity limit follows the model's metering: a provider that
// meters each model separately pauses that model, while one that meters the
// account pauses the whole credential. The latter matters when several profiles
// share one API key — a limit reached through one of them is spent for all of
// them, so pausing only the model that reported it would let each sibling
// profile spend the same exhausted quota in turn.
func suspensionTarget(candidate Candidate, code routingerr.Code) string {
	if isUsageLimitCode(code) && candidate.ModelKey != "" &&
		policyForModel(candidate.ModelID).modelScoped {
		return candidate.ModelKey
	}
	return candidate.BindingKey
}

// suspensionUntil sizes one suspension. A known reset instant wins: the
// failure's own reset hint, then a fully consumed usage window. Without one,
// a laddered provider blocks by strike count and never past its next monthly
// reset; every other failure keeps the standard short backoff.
func (e *Engine) suspensionUntil(
	ctx context.Context,
	candidate Candidate,
	failure *routingerr.Error,
	strikes int,
	now time.Time,
) time.Time {
	until := now.Add(circuitBackoff)
	if failure.ResetHint != nil && failure.ResetHint.After(until) {
		return *failure.ResetHint
	}
	policy := policyForModel(candidate.ModelID)
	if !isUsageLimitCode(failure.Code) || len(policy.ladder) == 0 {
		return until
	}
	if e.calendar != nil {
		if exhausted, ok := e.calendar.ExhaustedUntil(ctx, candidate, now); ok && exhausted.After(until) {
			return exhausted
		}
	}
	if candidate.SuspendedUntil.After(until) {
		return candidate.SuspendedUntil
	}
	return e.ladderUntil(ctx, candidate, policy, strikes, now)
}

// ladderUntil sizes a suspension whose reset instant is unknown by its strike
// count, never past the next monthly reset.
func (e *Engine) ladderUntil(
	ctx context.Context,
	candidate Candidate,
	policy limitPolicy,
	strikes int,
	now time.Time,
) time.Time {
	monthly, monthlyKnown := time.Time{}, false
	if e.calendar != nil {
		monthly, monthlyKnown = e.calendar.MonthlyReset(ctx, candidate, now)
		monthlyKnown = monthlyKnown && monthly.After(now)
	}
	strikes = max(strikes, 1)
	if strikes > len(policy.ladder) && policy.untilMonthlyReset && monthlyKnown {
		return monthly
	}
	until := now.Add(policy.ladder[min(strikes, len(policy.ladder))-1])
	if monthlyKnown && monthly.Before(until) {
		return monthly
	}
	return until
}
