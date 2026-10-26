package backendapp

import (
	"context"
	"time"

	"github.com/google/uuid"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
)

// tierLimitAdapter maps the provider tier service onto the dynamic routing
// allowance seams.
type tierLimitAdapter struct {
	usage    *usageProviderAdapter
	calendar *dynamicLimitCalendar
}

func newTierLimitAdapter(usage *usageProviderAdapter, calendar *dynamicLimitCalendar) *tierLimitAdapter {
	return &tierLimitAdapter{usage: usage, calendar: calendar}
}

// CheckLimit reports the first exhausted configured allowance.
func (a *tierLimitAdapter) CheckLimit(
	ctx context.Context,
	candidate dynamicruntime.Candidate,
	sessionID string,
	now time.Time,
) (dynamicruntime.LimitVerdict, error) {
	if a == nil || a.usage == nil || a.usage.tierLimits == nil || candidate.RemoteExecution {
		return dynamicruntime.LimitVerdict{}, nil
	}
	verdict, err := a.usage.tierLimits.Check(ctx, candidate.ID, sessionID, now)
	return dynamicruntime.LimitVerdict{Reason: verdict.Reason, RetryAt: verdict.RetryAt}, err
}

// ResolveLimitFailure refines a classified limit with account evidence.
func (a *tierLimitAdapter) ResolveLimitFailure(
	ctx context.Context,
	candidate dynamicruntime.Candidate,
	failure *routingerr.Error,
) (*routingerr.Error, dynamicruntime.LimitSuspension) {
	if a == nil || a.usage == nil || a.usage.tierLimits == nil {
		return failure, dynamicruntime.SuspensionBinding
	}
	resolved := a.usage.tierLimits.resolveFailure(ctx, candidate, failure)
	if resolved == nil {
		resolved = failure
	}
	scope := dynamicruntime.SuspensionBinding
	switch {
	case !candidate.RemoteExecution && dynamicruntime.ProviderOf(candidate.ModelID) == accountPrefixOpenCodeGo &&
		resolved.Code == routingerr.CodeQuotaLimited:
		// Go subscription exhaustion covers its models on this account, while
		// other providers on the same agent credential binding remain usable.
		scope = dynamicruntime.SuspensionProvider
	case resolved != failure:
		// The OpenRouter key counter separates a minute block from the
		// account's daily free allowance; both are account/provider scope.
		scope = dynamicruntime.SuspensionProvider
	case dynamicruntime.FailureCauseFor(candidate, failure) == dynamicruntime.CauseFreeDailyLimit &&
		(dynamicruntime.IsFreeModel(candidate.ModelID) || dynamicruntime.ModelScoped(candidate.ModelID)):
		scope = dynamicruntime.SuspensionModel
	}
	// A classified quota limit on a paid model without the provider's own
	// reset uses the operator-entered monthly reset. The monthly allowance is
	// provider-scoped.
	if resolved.Code == routingerr.CodeQuotaLimited && resolved.ResetHint == nil &&
		a.calendar != nil && !dynamicruntime.IsFreeModel(candidate.ModelID) {
		now := time.Now()
		if reset, ok := a.calendar.MonthlyReset(ctx, candidate, now); ok && reset.After(now) {
			copy := *resolved
			copy.ResetHint = &reset
			return &copy, dynamicruntime.SuspensionProvider
		}
	}
	return resolved, scope
}

// ObserveLimit records the classified limit and learns an undisclosed
// allowance. It runs off the routing path.
func (a *tierLimitAdapter) ObserveLimit(
	_ context.Context,
	candidate dynamicruntime.Candidate,
	failure *routingerr.Error,
	at time.Time,
) {
	if a == nil || a.usage == nil || a.usage.tierLimits == nil || failure == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		observation := &sqliterepo.UsageLimitObservation{
			ID: uuid.NewString(), ExecutionProfileID: candidate.ID,
			AccountKey: a.accountKey(ctx, candidate.ID), Code: string(failure.Code), ObservedAt: at,
		}
		_ = a.usage.tierLimits.recordObservation(ctx, candidate, failure, observation)
	}()
}

func (a *tierLimitAdapter) accountKey(ctx context.Context, profileID string) string {
	if a != nil && a.usage != nil {
		if profile, agentType, ok := a.usage.loadProfile(ctx, profileID); ok {
			if binding, bound := a.usage.bindingFor(profile, agentType); bound && binding.accountKey != "" {
				return binding.accountKey
			}
		}
	}
	return "profile:" + profileID
}
