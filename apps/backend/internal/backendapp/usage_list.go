package backendapp

import (
	"context"
	"time"

	"github.com/kandev/kandev/internal/agent/settings/dto"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
)

// profileUsageLister answers the agents settings list. Provider readings come
// from the shared usage cache; an account without a usage API reports the
// usage Kandev recorded for the whole account in the current UTC day.
type profileUsageLister struct {
	usage  *usageProviderAdapter
	ledger accountWindowTotalsReader
	limits usageLimitHistory
	now    func() time.Time
}

func newProfileUsageLister(usage *usageProviderAdapter, ledger accountWindowTotalsReader) *profileUsageLister {
	lister := &profileUsageLister{usage: usage, ledger: ledger, now: time.Now}
	if history, ok := ledger.(usageLimitHistory); ok {
		lister.limits = history
	}
	return lister
}

// ListProfileUsage implements the settings controller's ProfileUsageProvider.
func (l *profileUsageLister) ListProfileUsage(ctx context.Context) ([]dto.AgentProfileUsageDTO, error) {
	profiles, err := l.usage.boundProfiles(ctx)
	if err != nil {
		return nil, err
	}
	l.usage.prefetchAccounts(ctx, profiles)
	accounts := make(map[string][]string)
	for _, bound := range profiles {
		accounts[bound.binding.accountKey] = append(accounts[bound.binding.accountKey], bound.profile.ID)
	}
	now := l.now()
	recorded := make(map[string]*dto.AgentProfileRecordedUsageDTO)
	internal := make(map[string]*dto.AgentProfileInternalUsageDTO)
	hits := l.limitHitsByAccount(ctx, now)
	result := make([]dto.AgentProfileUsageDTO, 0, len(profiles))
	for _, bound := range profiles {
		usage := l.usage.usageForProfile(ctx, bound.profile, bound.agentType)
		entry := profileUsageDTO(bound.profile.ID, usage)
		key := bound.binding.accountKey
		if usage.State == profileUsageNoUsageAPI {
			if _, done := recorded[key]; !done {
				recorded[key] = l.recordedToday(ctx, accounts[key])
			}
			entry.Recorded = recorded[key]
		}
		if _, done := internal[key]; !done {
			internal[key] = l.accountInternalUsage(ctx, accounts[key], now)
		}
		entry.Internal = internal[key]
		entry.LimitHits = hits[key]
		result = append(result, entry)
	}
	return result, nil
}

// recordedToday sums the account's recorded usage since midnight UTC, the
// boundary providers without a usage API reset their daily free quotas on.
func (l *profileUsageLister) recordedToday(ctx context.Context, profileIDs []string) *dto.AgentProfileRecordedUsageDTO {
	if l.ledger == nil || len(profileIDs) == 0 {
		return nil
	}
	now := l.now().UTC()
	start := now.Truncate(24 * time.Hour)
	totals, err := l.ledger.GetManualWindowUsageForProfiles(ctx, profileIDs, start, now)
	if err != nil {
		return nil
	}
	return &dto.AgentProfileRecordedUsageDTO{
		WindowStart: start, WindowEnd: start.Add(24 * time.Hour),
		Turns: totals.EventCount, TokensTotal: totals.TokensTotal, ProfileCount: len(profileIDs),
	}
}

func profileUsageDTO(profileID string, usage profileUsage) dto.AgentProfileUsageDTO {
	entry := dto.AgentProfileUsageDTO{
		ProfileID: profileID, State: usage.State, Source: usage.Source,
		ModelClass: string(usage.ModelClass), Status: usage.Status,
		Windows: []dto.AgentProfileUsageWindowDTO{},
	}
	if usage.Reason != "" {
		entry.Reason = string(usage.Reason)
	}
	if usage.Usage == nil {
		return entry
	}
	entry.Provider, entry.Plan = usage.Usage.Provider, usage.Usage.Plan
	entry.Stale = usage.Usage.Stale
	if !usage.Usage.FetchedAt.IsZero() {
		fetchedAt := usage.Usage.FetchedAt
		entry.FetchedAt = &fetchedAt
	}
	for _, window := range usage.Usage.Windows {
		if window.ModelID != "" && window.ModelID != usage.ModelID {
			continue
		}
		entry.Windows = append(entry.Windows, usageWindowDTO(window))
	}
	return entry
}

func usageWindowDTO(window agentusage.UtilizationWindow) dto.AgentProfileUsageWindowDTO {
	converted := dto.AgentProfileUsageWindowDTO{
		Label: window.Label, UtilizationPct: window.UtilizationPct,
		DurationSeconds: window.DurationSeconds, Scope: string(window.Scope), LimitReached: window.LimitReached,
	}
	if !window.ResetAt.IsZero() {
		resetAt := window.ResetAt
		converted.ResetAt = &resetAt
	}
	if !window.StartAt.IsZero() {
		startAt := window.StartAt
		converted.StartAt = &startAt
	}
	return converted
}
