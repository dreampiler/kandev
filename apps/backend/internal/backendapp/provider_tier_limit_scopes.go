package backendapp

import (
	"context"
	"sort"
	"time"

	agentusage "github.com/kandev/kandev/internal/agent/usage"
)

// limitDimensionModelDaily is a per-model daily allowance error. It learns the
// model's request scope, never the account-wide daily allowance.
const limitDimensionModelDaily = "model_daily"

// readScopeUsage counts each scope's requests since its daily reset over the
// tier's account profiles whose model the scope covers.
func (s *providerTierLimitService) readScopeUsage(ctx context.Context, c tierLimitContext, now time.Time, used *agentusage.LimitUsage) error {
	if len(c.effective.Scopes) == 0 {
		return nil
	}
	used.Scopes = make(map[string]float64, len(c.effective.Scopes))
	models := s.profileModels(ctx, c.ids)
	for _, scope := range c.effective.Scopes {
		start, reset, err := agentusage.DailyLimitWindow(now, scope.DailyReset)
		if err != nil {
			return err
		}
		used.Resets[agentusage.ScopeUsageKey(scope.ID)] = reset
		ids := scopeProfileIDs(models, scope)
		if len(ids) == 0 {
			used.Scopes[scope.ID] = 0
			continue
		}
		count, err := s.readRequestCount(ctx, ids, start, now)
		if err != nil {
			return err
		}
		used.Scopes[scope.ID] = count
	}
	return nil
}

func (s *providerTierLimitService) profileModels(ctx context.Context, ids []string) map[string]string {
	models := make(map[string]string, len(ids))
	for _, id := range ids {
		if p, _, ok := s.usage.loadProfile(ctx, id); ok {
			models[id] = p.Model
		}
	}
	return models
}

func scopeProfileIDs(models map[string]string, scope agentusage.RequestScope) []string {
	var ids []string
	for id, model := range models {
		if scope.Covers(model) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// learnModelScope records the requests counted in the scope covering the
// profile's model as that scope's learned value. A model without a scope gets
// a learned model scope of its own.
func (s *providerTierLimitService) learnModelScope(ctx context.Context, c tierLimitContext, used agentusage.LimitUsage, at time.Time) error {
	scope, ok := agentusage.CoveringScope(c.effective.Scopes, c.profile.Model)
	counted := used.Scopes[scope.ID]
	if !ok {
		model := agentusage.ScopeModelID(c.profile.Model)
		scope = agentusage.RequestScope{ID: agentusage.ModelScopeID(model), Kind: agentusage.ScopeKindModel, Models: []string{model}}
		start, _, err := agentusage.DailyLimitWindow(at, "")
		if err != nil {
			return err
		}
		counted, err = s.readRequestCount(ctx, scopeProfileIDs(s.profileModels(ctx, c.ids), scope), start, at)
		if err != nil {
			return err
		}
	}
	if counted <= 0 {
		return nil
	}
	tier := c.learned.Tiers[c.tier]
	tier.Scopes = agentusage.LearnScope(tier.Scopes, scope, counted, at)
	c.learned.Tiers[c.tier] = tier
	return s.store.SaveAccountTierLearned(ctx, c.key, c.learned)
}
