package dynamic

import (
	"math"
	"time"
)

// Pace ranking is pure computation over a bounded usage snapshot, so the same
// function answers both a settings preview and a live selection.
const (
	// minPaceElapsed keeps a just-reset window from producing a near-zero pace
	// and making a fresh window look like unlimited capacity.
	minPaceElapsed = 0.05
)

// WindowObservation is one candidate's controlling usage window. A nil
// UsageFraction is unknown usage: it is never rendered as zero usage or
// unlimited capacity, and an unknown-only candidate sorts after a known one.
type WindowObservation struct {
	Label            string
	UsageFraction    *float64
	StartAt          time.Time
	ResetAt          time.Time
	ExplicitNoWindow bool
	ObservedAt       time.Time
	// Partial marks a measurement that is known to undercount, for example a
	// ledger total with an unpriced or incompletely measured event. The figure
	// stays visible as a lower bound but must not produce a pace.
	Partial bool
	// Exhausted marks a window the provider reports as used up. When the window
	// has no usable span, its usage is still a known fact: the candidate is
	// scored as busy as possible rather than left unknown.
	Exhausted bool
}

func (w WindowObservation) partial() bool { return w.Partial && !w.ExplicitNoWindow }

// PaceScore is the result of ranking one candidate's windows.
type PaceScore struct {
	Known           bool
	Pace            float64
	Controlling     string
	UsageFraction   float64
	ElapsedFraction float64
	// HasRecord distinguishes a genuinely unobserved candidate from one whose
	// recorded usage fraction is zero. Without it a missing read and a real zero
	// would be indistinguishable, which is exactly the confusion the unknown
	// state exists to prevent.
	HasRecord bool
	// Complete is false when the recorded evidence is partial, for example
	// unpriced or incompletely measured events. The recorded fraction is then a
	// visible lower bound rather than a usable pace.
	Complete bool
	// FloorApplied records that the elapsed floor, not the real elapsed share,
	// divided the usage fraction, so a preview can explain the number.
	FloorApplied bool
	ObservedAt   time.Time
}

// PaceFromWindows selects the largest pace across the applicable windows.
//
//	elapsed = clamp((now-start)/(reset-start), 0, 1)
//	pace    = usage_fraction / max(elapsed, minPaceElapsed)
//
// Usage is deliberately not capped at 100% before the division: an overrun is
// the strongest signal a candidate is busy. A window whose start is in the
// future, whose reset has passed, or whose span is not positive is invalid and
// is skipped rather than clamped into a usable score.
//
// A partially measured window still contributes its recorded figure, so the
// lower bound stays visible, but it never produces a pace: an incomplete total
// understates usage, and ranking on it would overstate the candidate's
// remaining capacity.
func PaceFromWindows(now time.Time, windows []WindowObservation) PaceScore {
	best := PaceScore{}
	found := false
	recorded := false
	incomplete := false
	for _, window := range windows {
		if window.UsageFraction != nil && window.partial() {
			// The window is a real measurement, so it is reported as a record,
			// but its figure is a lower bound rather than a usable pace.
			recorded = true
			incomplete = true
			if !best.HasRecord {
				best.UsageFraction = *window.UsageFraction
				best.Controlling = window.Label
				best.ObservedAt = window.ObservedAt
			}
			continue
		}
		score, ok := paceForWindow(now, window)
		if !ok {
			continue
		}
		if !found || score.Pace > best.Pace {
			best = score
			found = true
		}
	}
	if !found && (recorded || incomplete) {
		best.HasRecord = recorded
		best.Complete = false
	}
	return best
}

func paceForWindow(now time.Time, window WindowObservation) (PaceScore, bool) {
	// A free candidate that is explicitly configured without a window is known
	// to have no consumption, which is different from unknown usage.
	if window.ExplicitNoWindow {
		return PaceScore{
			Known: true, Complete: true, Pace: 0,
			Controlling: window.Label, ObservedAt: window.ObservedAt,
		}, true
	}
	if window.UsageFraction == nil {
		return PaceScore{}, false
	}
	if !window.ResetAt.After(window.StartAt) {
		return exhaustedWithoutSpan(window)
	}
	if !now.Before(window.ResetAt) || now.Before(window.StartAt) {
		return PaceScore{}, false
	}
	elapsed := now.Sub(window.StartAt).Seconds() / window.ResetAt.Sub(window.StartAt).Seconds()
	elapsed = math.Min(math.Max(elapsed, 0), 1)
	denominator := math.Max(elapsed, minPaceElapsed)
	return PaceScore{
		Known:           true,
		Complete:        true,
		Pace:            *window.UsageFraction / denominator,
		Controlling:     window.Label,
		UsageFraction:   *window.UsageFraction,
		ElapsedFraction: elapsed,
		HasRecord:       true,
		FloorApplied:    elapsed < minPaceElapsed,
		ObservedAt:      window.ObservedAt,
	}, true
}

// exhaustedWithoutSpan scores a used-up window whose elapsed share is unknown.
// The elapsed floor is the most conservative divisor, so the candidate ranks
// behind every candidate whose window still has capacity.
func exhaustedWithoutSpan(window WindowObservation) (PaceScore, bool) {
	if !window.Exhausted {
		return PaceScore{}, false
	}
	usage := math.Max(*window.UsageFraction, 1)
	return PaceScore{
		Known:         true,
		Complete:      true,
		Pace:          usage / minPaceElapsed,
		Controlling:   window.Label,
		UsageFraction: usage,
		HasRecord:     true,
		FloorApplied:  true,
		ObservedAt:    window.ObservedAt,
	}, true
}

// CostRank orders the configured marginal-cost classes. The unknown legacy
// classification sorts last so an unclassified row never wins on a guess, and
// row order breaks ties inside a class. This is class ordering, not numerical
// price comparison: no workload mix is configured.
func CostRank(cost CostClass) int {
	switch cost {
	case CostFree:
		return 0
	case CostSubscription:
		return 1
	case CostMetered:
		return 2
	default:
		return 3
	}
}

// ReservationExclusion reports whether a configured user-reserved share keeps a
// candidate out of new selections. The share is an admission gate, not a
// modification of pace: raw pace is unchanged by a reservation.
//
// A positive share with a positive usable limit excludes the candidate once
// observed usage reaches 1 - share. A positive share against unknown usage
// also excludes, because an unobservable reserve must not be spent silently.
// A zero share adds no gate at all.
func ReservationExclusion(options ModelOptions, score PaceScore) bool {
	if options.ReservedUserSharePct <= 0 {
		return false
	}
	if !score.Known {
		return true
	}
	return score.UsageFraction >= 1-float64(options.ReservedUserSharePct)/100
}

// RankCandidate is one candidate's rank within its tier.
type RankCandidate struct {
	Candidate Candidate
	TierIndex int
	Score     PaceScore
	Eligible  bool
	// IneligibleReason is a stable code, never provider text.
	IneligibleReason string
}

// RankOptions carries what a ranking pass may consult besides the candidates.
type RankOptions struct {
	Now      time.Time
	Scores   map[string]PaceScore
	Eligible map[string]string
	Excluded map[string]bool
}

// RankTier orders the eligible candidates of one tier using the tier's own
// rule. Tiers are always traversed in list order, so a caller walks tiers and
// ranks only the first one that still has an eligible candidate.
//
// Known pace sorts before unknown, equal values keep saved row order, and cost
// ranking uses the configured class with row-order ties. An explicit exclusion
// from the durable transition chain is applied before ranking.
func RankTier(tier Tier, options RankOptions) []RankCandidate {
	ranked := make([]RankCandidate, 0, len(tier.Candidates))
	for _, candidate := range tier.Candidates {
		score, known := options.Scores[candidate.ID]
		if !known {
			score = PaceScore{}
		}
		if reason, ok := options.Eligible[candidate.ID]; ok && reason != "" {
			ranked = append(ranked, RankCandidate{
				Candidate: candidate, TierIndex: tier.Index, Score: score,
				Eligible: false, IneligibleReason: reason,
			})
			continue
		}
		if options.Excluded[candidate.ID] {
			ranked = append(ranked, RankCandidate{
				Candidate: candidate, TierIndex: tier.Index, Score: score,
				Eligible: false, IneligibleReason: IneligibleTried,
			})
			continue
		}
		if ReservationExclusion(candidate.Selection.Model, score) {
			ranked = append(ranked, RankCandidate{
				Candidate: candidate, TierIndex: tier.Index, Score: score,
				Eligible: false, IneligibleReason: IneligibleReserved,
			})
			continue
		}
		ranked = append(ranked, RankCandidate{
			Candidate: candidate, TierIndex: tier.Index, Score: score, Eligible: true,
		})
	}
	stableSortRanked(ranked, tier.Policy.Mode)
	return ranked
}

// Ineligible reason codes. They are a closed set so a route reason and a
// preview never embed provider-controlled text.
const (
	IneligibleDisabled = "disabled"
	IneligibleCircuit  = "circuit_open"
	IneligibleTried    = "already_tried"
	IneligibleReserved = "reserved_share"
	IneligibleUnusable = "unusable_binding"
)

// stableSortRanked keeps saved row order for every tie, which is what makes
// deterministic row-order tie-breaking possible without a secondary key.
func stableSortRanked(ranked []RankCandidate, mode TierMode) {
	less := func(left, right RankCandidate) bool {
		switch mode {
		case TierModePace:
			return rankByPace(left, right)
		case TierModeCost:
			return rankByCost(left, right)
		default:
			return false
		}
	}
	insertionSort(ranked, less)
}

func rankByPace(left, right RankCandidate) bool {
	if left.Score.Known != right.Score.Known {
		// Known pace is preferred; an unknown candidate is not a zero.
		return left.Score.Known
	}
	if !left.Score.Known || !right.Score.Known {
		return false
	}
	return left.Score.Pace < right.Score.Pace
}

func rankByCost(left, right RankCandidate) bool {
	return CostRank(left.Candidate.Selection.Model.Cost) < CostRank(right.Candidate.Selection.Model.Cost)
}

func insertionSort(ranked []RankCandidate, less func(RankCandidate, RankCandidate) bool) {
	for index := 1; index < len(ranked); index++ {
		current := ranked[index]
		position := index - 1
		for position >= 0 && less(current, ranked[position]) {
			ranked[position+1] = ranked[position]
			position--
		}
		ranked[position+1] = current
	}
}

// FirstEligible returns the winning candidate of a ranked tier.
func FirstEligible(ranked []RankCandidate) (RankCandidate, bool) {
	for _, entry := range ranked {
		if entry.Eligible {
			return entry, true
		}
	}
	return RankCandidate{}, false
}
