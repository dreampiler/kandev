package backendapp

import (
	"context"
	"errors"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	"math"
	"strings"
	"time"
)

func (s *providerTierLimitService) readUsage(ctx context.Context, c tierLimitContext, exclude string, now time.Time) (agentusage.LimitUsage, error) {
	used := agentusage.LimitUsage{Windows: make(map[string]float64), Resets: make(map[string]time.Time), Estimated: true}
	active, err := s.store.CountLimitActiveTurns(ctx, c.ids, exclude)
	if err != nil {
		return used, err
	}
	used.ActiveTurns = active
	var attribution error
	if err := s.readRequestUsage(ctx, c, now, &used, &attribution); err != nil {
		return used, err
	}
	if err := s.readUsageBudgets(ctx, c, now, &used, &attribution); err != nil {
		return used, err
	}
	if err := deferModelAttribution(&attribution, s.readScopeUsage(ctx, c, now, &used, c.profile.Model)); err != nil {
		return used, err
	}
	return used, attribution
}

func (s *providerTierLimitService) readUsageBudgets(ctx context.Context, c tierLimitContext, now time.Time, used *agentusage.LimitUsage, attribution *error) error {
	for _, budget := range c.effective.Windows {
		amount, reset, known, err := s.readBudgetUsage(ctx, c, budget, now)
		if err := deferModelAttribution(attribution, err); err != nil {
			return err
		}
		if known {
			used.Windows[budget.Key()] = amount
			used.Resets[budget.Key()] = reset
		}
	}
	return nil
}

func deferModelAttribution(first *error, err error) error {
	if !errors.Is(err, errModelAttributionUnavailable) {
		return err
	}
	if *first == nil {
		*first = err
	}
	return nil
}

func (s *providerTierLimitService) readRequestUsage(ctx context.Context, c tierLimitContext, now time.Time, used *agentusage.LimitUsage, attribution *error) error {
	var err error
	used.RequestsMinute, err = s.readMinuteRequestCount(ctx, c, now.Add(-time.Minute), now)
	if err := deferModelAttribution(attribution, err); err != nil {
		return err
	}
	start, reset, err := agentusage.DailyLimitWindow(now, c.effective.DailyReset)
	if err != nil {
		return err
	}
	day, err := s.store.GetManualWindowUsageForProfiles(ctx, c.ids, start, now)
	if err != nil {
		return err
	}
	used.RequestsDay = math.Max(float64(day.EventCount), s.requests.count(c.ids, start, now))
	used.TurnsDay = day.TurnCount
	used.TokensDay = day.TokensTotal
	used.Resets["day"] = reset
	if c.effective.RequestWindowSeconds > 0 {
		used.RequestsWindow, err = s.readMinuteRequestCount(ctx, c, now.Add(-time.Duration(c.effective.RequestWindowSeconds)*time.Second), now)
		if err := deferModelAttribution(attribution, err); err != nil {
			return err
		}
	}
	if c.tier == agentusage.TierFree && c.observed != nil && c.observed.FreeModelDailyRequests != nil && !c.observed.FetchedAt.Before(start) {
		counter := c.observed.FreeModelDailyRequests
		used.RequestsDay = counter.Used
		if counter.Remaining == 0 && c.effective.RequestsPerDay != nil {
			used.RequestsDay = math.Max(used.RequestsDay, *c.effective.RequestsPerDay)
		}
	}
	return nil
}

func (s *providerTierLimitService) readMinuteRequestCount(ctx context.Context, c tierLimitContext, start, end time.Time) (float64, error) {
	if c.effective.RequestsPerModel {
		return s.readModelRequestCount(ctx, c.ids, []string{c.profile.Model}, start, end)
	}
	return s.readRequestCount(ctx, c.ids, start, end)
}

func (s *providerTierLimitService) readRequestCount(ctx context.Context, ids []string, start, end time.Time) (float64, error) {
	totals, err := s.store.GetManualWindowUsageForProfiles(ctx, ids, start, end)
	return math.Max(float64(totals.EventCount), s.requests.count(ids, start, end)), err
}

func (s *providerTierLimitService) readBudgetUsage(ctx context.Context, c tierLimitContext, budget agentusage.LimitBudget, now time.Time) (float64, time.Time, bool, error) {
	if budget.Unit == "percent" {
		window, ok := findProviderBudget(c.observed, budget, c.profile.Model)
		if !ok {
			return 0, time.Time{}, false, nil
		}
		if !window.ResetAt.IsZero() && !window.ResetAt.After(now) {
			return 0, time.Time{}, false, nil
		}
		amount := window.UtilizationPct
		if window.LimitReached {
			amount = math.Max(100, amount)
		}
		return amount, window.ResetAt, true, nil
	}
	start, reset, ok := agentusage.LimitWindowBounds(now, budget)
	if budget.Reset == hostSourceProvider {
		if window, found := findProviderBudget(c.observed, budget, c.profile.Model); found && !window.StartAt.IsZero() && window.ResetAt.After(now) {
			start, reset, ok = window.StartAt, window.ResetAt, true
		}
	}
	if !ok {
		return 0, time.Time{}, false, nil
	}
	if budget.Scope != "" {
		amount, known, err := s.readModelBudgetUsage(ctx, c, budget, start, now)
		return amount, reset, known, err
	}
	total, err := s.store.GetManualWindowUsageForProfiles(ctx, c.ids, start, now)
	return budgetAmount(total, budget.Unit), reset, true, err
}

func findProviderBudget(observed *agentusage.ProviderUsage, b agentusage.LimitBudget, model string) (agentusage.UtilizationWindow, bool) {
	if observed == nil {
		return agentusage.UtilizationWindow{}, false
	}
	var shared agentusage.UtilizationWindow
	foundShared := false
	var family agentusage.UtilizationWindow
	foundFamily := false
	for _, w := range observed.Windows {
		if providerWindowPeriod(w) != b.Window || string(w.Scope) != b.Scope {
			continue
		}
		if w.ModelID != "" {
			if exactProviderBudgetModel(w.ModelID, model) {
				return w, true
			}
			continue
		}
		if w.Model != "" {
			if exactProviderBudgetModel(w.Model, model) {
				return w, true
			}
			if !foundFamily && knownProviderBudgetFamily(w, model) {
				family, foundFamily = w, true
			}
			continue
		}
		if !foundShared {
			shared, foundShared = w, true
		}
	}
	if foundFamily {
		return family, true
	}
	return shared, foundShared
}

func knownProviderBudgetFamily(window agentusage.UtilizationWindow, model string) bool {
	switch strings.ToLower(strings.TrimSpace(window.Model)) {
	case "opus", "sonnet", "haiku":
		return model != "" && agentusage.ExecutionUsageWindowApplies(window, model)
	default:
		return false
	}
}

func exactProviderBudgetModel(recorded, model string) bool {
	if model == "" {
		return false
	}
	recordedProvider, _, recordedQualified := strings.Cut(recorded, "/")
	modelProvider, _, modelQualified := strings.Cut(model, "/")
	if recordedQualified && modelQualified {
		return strings.EqualFold(recordedProvider, modelProvider) &&
			strings.EqualFold(agentusage.ScopeModelID(recorded), agentusage.ScopeModelID(model))
	}
	return strings.EqualFold(agentusage.ScopeModelID(recorded), agentusage.ScopeModelID(model))
}

func budgetAmount(total sqliterepo.ManualWindowUsage, unit string) float64 {
	switch unit {
	case "usd":
		return float64(total.CostSubcents) / subcentsPerDollar
	case "tokens":
		return float64(total.TokensTotal)
	case "turns":
		return float64(total.TurnCount)
	default:
		return float64(total.EventCount)
	}
}

func (s *providerTierLimitService) windowReset(ctx context.Context, id, period string, now time.Time, exhaustedOnly bool) (time.Time, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := s.contextFor(ctx, id)
	if err != nil {
		return time.Time{}, false
	}
	used, err := s.readUsage(ctx, c, "", now)
	unknownModel := errors.Is(err, errModelAttributionUnavailable)
	if err != nil && !unknownModel {
		return time.Time{}, false
	}
	var latest time.Time
	for _, budget := range c.effective.Windows {
		if unknownModel && budget.Scope != "" {
			continue
		}
		if period != "" && budget.Window != period {
			continue
		}
		if exhaustedOnly && (budget.Value == nil || used.Windows[budget.Key()] < *budget.Value) {
			continue
		}
		reset := used.Resets[budget.Key()]
		if !reset.After(now) {
			_, reset, _ = agentusage.LimitWindowBounds(now, budget)
		}
		if reset.After(latest) {
			latest = reset
		}
	}
	return latest, latest.After(now)
}
