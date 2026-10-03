package backendapp

import (
	"context"
	"sort"
	"time"

	"github.com/kandev/kandev/internal/agent/settings/dto"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
)

// limitHitLookback bounds the limit observations summarized on the agents list.
const limitHitLookback = 30 * 24 * time.Hour

type usageLimitHistory interface {
	ListUsageLimitObservationsSince(ctx context.Context, since time.Time) ([]sqliterepo.UsageLimitObservation, error)
}

// internalWindows are the trailing windows shown for every account.
var internalWindows = []struct {
	label string
	span  time.Duration
}{
	{"5h", window5h},
	{"day", internalRankingWindow},
	{"week", windowWeek},
}

// accountInternalUsage sums the account's recorded usage over each trailing
// window. A window whose ledger read fails is omitted rather than shown as zero.
func (l *profileUsageLister) accountInternalUsage(
	ctx context.Context,
	profileIDs []string,
	now time.Time,
) *dto.AgentProfileInternalUsageDTO {
	if l.ledger == nil || len(profileIDs) == 0 {
		return nil
	}
	internal := &dto.AgentProfileInternalUsageDTO{ProfileCount: len(profileIDs)}
	for _, window := range internalWindows {
		totals, err := l.ledger.GetManualWindowUsageForProfiles(ctx, profileIDs, now.Add(-window.span), now)
		if err != nil {
			continue
		}
		internal.Windows = append(internal.Windows, dto.AgentProfileInternalWindowDTO{
			Label: window.label, Turns: totals.EventCount, TokensTotal: totals.TokensTotal,
			CostSubcents: totals.CostSubcents,
		})
	}
	if len(internal.Windows) == 0 {
		return nil
	}
	return internal
}

// limitHitsByAccount groups recent limit observations by account key.
func (l *profileUsageLister) limitHitsByAccount(ctx context.Context, now time.Time) map[string]*dto.AgentProfileLimitHitsDTO {
	if l.limits == nil {
		return nil
	}
	observations, err := l.limits.ListUsageLimitObservationsSince(ctx, now.Add(-limitHitLookback))
	if err != nil || len(observations) == 0 {
		return nil
	}
	grouped := make(map[string][]sqliterepo.UsageLimitObservation)
	for _, observation := range observations {
		grouped[observation.AccountKey] = append(grouped[observation.AccountKey], observation)
	}
	summaries := make(map[string]*dto.AgentProfileLimitHitsDTO, len(grouped))
	for key, hits := range grouped {
		summaries[key] = summarizeLimitHits(hits)
	}
	return summaries
}

// summarizeLimitHits reports the median recorded usage at the moment of a hit,
// which is robust to one hit caused by an unrelated short burst.
func summarizeLimitHits(hits []sqliterepo.UsageLimitObservation) *dto.AgentProfileLimitHitsDTO {
	summary := &dto.AgentProfileLimitHitsDTO{Count: len(hits)}
	for _, hit := range hits {
		if hit.ObservedAt.After(summary.LastAt) {
			summary.LastAt = hit.ObservedAt
		}
	}
	pick := func(value func(sqliterepo.UsageLimitObservation) int64) int64 {
		values := make([]int64, 0, len(hits))
		for _, hit := range hits {
			values = append(values, value(hit))
		}
		return median(values)
	}
	summary.MedianTurns5h = pick(func(h sqliterepo.UsageLimitObservation) int64 { return h.Turns5h })
	summary.MedianTurnsDay = pick(func(h sqliterepo.UsageLimitObservation) int64 { return h.TurnsDay })
	summary.MedianTokensDay = pick(func(h sqliterepo.UsageLimitObservation) int64 { return h.TokensDay })
	summary.MedianTurnsWeek = pick(func(h sqliterepo.UsageLimitObservation) int64 { return h.TurnsWeek })
	summary.MedianTokensWeek = pick(func(h sqliterepo.UsageLimitObservation) int64 { return h.TokensWeek })
	return summary
}

// median returns the lower median, so the estimate never exceeds a value that
// was actually recorded at a hit.
func median(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[(len(sorted)-1)/2]
}
