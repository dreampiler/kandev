package backendapp

import (
	"context"
	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	"strings"
	"time"
)

const limitDimensionUnknown = "unknown"

func (s *providerTierLimitService) resolveFailure(ctx context.Context, candidate dynamicruntime.Candidate, failure *routingerr.Error) *routingerr.Error {
	if candidate.RemoteExecution || dynamicruntime.ProviderOf(candidate.ModelID) != "openrouter" || failure.Code != routingerr.CodeRateLimited {
		return failure
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	counter, err := s.refreshOpenRouterCounter(ctx, candidate)
	if err != nil || counter == nil {
		return failure
	}
	copy := *failure
	now := time.Now()
	until := minuteResetFromFailure(failure, now)
	copy.ClassifierRule = "account_limit.minute"
	if counter.Remaining == 0 {
		copy.ClassifierRule = "account_limit.daily"
		until = now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	}
	copy.ResetHint = &until
	return &copy
}

func (s *providerTierLimitService) refreshOpenRouterCounter(ctx context.Context, candidate dynamicruntime.Candidate) (*agentusage.FreeModelCounter, error) {
	profile, a, ok := s.usage.loadProfile(ctx, candidate.ID)
	if !ok {
		return nil, nil
	}
	binding, ok := s.usage.bindingFor(profile, a)
	if !ok {
		return nil, nil
	}
	free := candidate.CatalogFree || dynamicruntime.IsFreeModel(candidate.ModelID)
	if !free && binding.class != nil {
		free = binding.class(ctx) == agentusage.ModelClassFree
	}
	if !free {
		return nil, nil
	}
	if binding.client == nil {
		return nil, nil
	}
	s.usage.svc.Register(candidate.ID, binding.client, binding.cacheKey)
	observed, err := s.usage.svc.RefreshUsage(ctx, candidate.ID)
	if err != nil || observed == nil {
		return nil, err
	}
	return observed.FreeModelDailyRequests, nil
}

// classifyDimension never guesses a long window from an ambiguous plan notice.
func classifyLimitDimension(c dynamicruntime.Candidate, failure *routingerr.Error, observed *agentusage.ProviderUsage) string {
	switch dynamicruntime.FailureCauseFor(c, failure) {
	case dynamicruntime.CauseAccountMinuteLimit:
		return "minute"
	case dynamicruntime.CauseFreeDailyLimit, dynamicruntime.CauseFreeAccountDailyLimit:
		return "daily"
	}
	if failure.ClassifierRule == routingerr.RuleClaudeWeeklyLimit {
		return "week"
	}
	if failure.ClassifierRule == routingerr.RuleClaudeSessionLimit {
		return "5h"
	}
	if observed != nil {
		for _, w := range observed.Windows {
			if w.Exhausted() {
				if period := providerWindowPeriod(w); period != "" {
					return period
				}
			}
		}
	}
	if strings.Contains(strings.ToLower(failure.RawExcerpt), "concurrent requests") {
		return "concurrency"
	}
	return limitDimensionUnknown
}

func (s *providerTierLimitService) enrichObservation(ctx context.Context, candidate dynamicruntime.Candidate, failure *routingerr.Error, observation *sqliterepo.UsageLimitObservation) error {
	if candidate.RemoteExecution {
		return nil
	}
	s.learningMu.Lock()
	defer s.learningMu.Unlock()
	c, err := s.contextFor(ctx, candidate.ID)
	if err != nil {
		return err
	}
	used, err := s.readUsage(ctx, c, "", observation.ObservedAt)
	if err != nil {
		return err
	}
	dimension := classifyLimitDimension(candidate, failure, c.observed)
	observation.Tier = string(c.tier)
	observation.Dimension = dimension
	observation.RequestsMinute = used.RequestsMinute
	observation.RequestsDay = used.RequestsDay
	observation.ActiveTurns = used.ActiveTurns
	if c.learned.Tiers == nil {
		c.learned.Tiers = make(map[agentusage.BillingTier]agentusage.TierLimits)
	}

	windows, err := s.snapshotLimitWindows(ctx, c, observation.ObservedAt, observation)
	if err != nil {
		return err
	}
	used.Windows = learningTokenWindows(windows)
	base := agentusage.MergeTierLimits(c.defaults, c.learned.Tiers[c.tier])
	learned := agentusage.LearnTierLimits(base, learningDimensionFor(c.tier, dimension), used)

	if !learned.Learned {
		return nil
	}
	c.learned.Tiers[c.tier] = learnedOnly(c.defaults, c.learned.Tiers[c.tier], learned)

	return s.store.SaveAccountTierLearned(ctx, c.key, c.learned)
}

func learnedOnly(defaults, prior, learned agentusage.TierLimits) agentusage.TierLimits {
	if defaults.RequestsPerMinute == nil {
		prior.RequestsPerMinute = learned.RequestsPerMinute
	}
	if defaults.RequestsPerDay == nil {
		prior.RequestsPerDay = learned.RequestsPerDay
		prior.DailyReset = learned.DailyReset
	}
	if defaults.MaxConcurrentTurns == nil {
		prior.MaxConcurrentTurns = learned.MaxConcurrentTurns
	}
	known := make(map[string]bool)
	for _, budget := range defaults.Windows {
		known[budget.Key()] = true
	}
	for _, budget := range learned.Windows {
		if !known[budget.Key()] {
			prior = agentusage.MergeTierLimits(prior, agentusage.TierLimits{Windows: []agentusage.LimitBudget{budget}})
		}
	}
	prior.Learned = true
	return prior
}
