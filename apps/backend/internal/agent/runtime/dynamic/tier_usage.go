package dynamic

import (
	"math"
	"sort"
	"time"
)

const (
	tierModePace            = "pace"
	tierModeHeadroom        = "headroom"
	tierModeConsumptionRate = "consumption_rate"
)

// UsageBucket contains existing, proved provider/account/window references.
// Empty Model identifies a shared account window, not a per-model allowance.
type UsageBucket struct{ Provider, OrganizationRef, AccountRef, Window, Model string }

// CandidateUsage is an internal observation projection. Profile IDs, circuit
// fingerprints and host cache keys cannot establish its account identity.
type CandidateUsage struct {
	Bucket                UsageBucket
	FetchedAt             time.Time
	Limited               bool
	Known                 bool
	RemainingPct          float64
	ConsumptionRateKnown  bool
	ConsumptionPctPerHour float64
}

func finiteUsageValue(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func normalizedCandidateUsage(value CandidateUsage, now time.Time) CandidateUsage {
	if value.Bucket.Provider == "" || value.Bucket.AccountRef == "" || value.Bucket.Window == "" || value.FetchedAt.IsZero() || value.FetchedAt.After(now) || now.Sub(value.FetchedAt) > 5*time.Minute {
		return CandidateUsage{}
	}
	value.Known = value.Known && finiteUsageValue(value.RemainingPct) && value.RemainingPct <= 100
	value.ConsumptionRateKnown = value.Known && value.ConsumptionRateKnown && finiteUsageValue(value.ConsumptionPctPerHour)
	return value
}

func sharedUsage(left, right CandidateUsage) CandidateUsage {
	left.Known = left.Known && right.Known && left.RemainingPct == right.RemainingPct
	left.ConsumptionRateKnown = left.Known && left.ConsumptionRateKnown && right.ConsumptionRateKnown && left.ConsumptionPctPerHour == right.ConsumptionPctPerHour
	return left
}

// A shared bucket is one observation. Conflicting projections become unknown
// rather than giving one of its profiles an artificial ranking advantage.
func applyTierUsage(rows []rankedTierRow, now time.Time) {
	groups := make(map[UsageBucket]CandidateUsage)
	for index := range rows {
		value := normalizedCandidateUsage(rows[index].candidate.Usage, now)
		if prior, exists := groups[value.Bucket]; exists {
			value = sharedUsage(prior, value)
		}
		groups[value.Bucket] = value
	}
	for index := range rows {
		value := groups[normalizedCandidateUsage(rows[index].candidate.Usage, now).Bucket]
		entry := &rows[index].entry
		entry.UsageKnown, entry.ConsumptionRateKnown = value.Known, value.ConsumptionRateKnown
		if value.Known {
			remaining := value.RemainingPct
			entry.RemainingPct = &remaining
		}
		if value.ConsumptionRateKnown {
			rate := value.ConsumptionPctPerHour
			entry.ConsumptionPctPerHour = &rate
		}
	}
}

func usageRank(entry TierPreviewEntry, rate bool) (float64, bool) {
	if rate {
		if entry.ConsumptionPctPerHour != nil {
			return *entry.ConsumptionPctPerHour, entry.ConsumptionRateKnown
		}
		return 0, false
	}
	if entry.RemainingPct != nil {
		return *entry.RemainingPct, entry.UsageKnown
	}
	return 0, false
}

func rankUsageRows(rows []rankedTierRow, mode string) []rankedTierRow {
	rate := mode == tierModeConsumptionRate
	if !rate && mode != tierModeHeadroom && mode != tierModePace {
		return rows
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left, leftKnown := usageRank(rows[i].entry, rate)
		right, rightKnown := usageRank(rows[j].entry, rate)
		if leftKnown != rightKnown {
			return leftKnown
		}
		if !leftKnown {
			return false
		}
		if rate {
			return left < right
		}
		return left > right
	})
	return rows
}
