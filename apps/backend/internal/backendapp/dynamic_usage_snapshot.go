package backendapp

import (
	"context"
	"math/big"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
)

// manualWindowTotalsReader is the narrow ledger seam tier ranking needs, so the
// composition boundary depends on a read contract rather than on the whole
// task repository.
type manualWindowTotalsReader interface {
	GetManualWindowUsage(
		ctx context.Context,
		executionProfileID string,
		start time.Time,
		end time.Time,
	) (sqliterepo.ManualWindowUsage, error)
}

// dynamicUsageSnapshot turns provider observations and recorded ledger totals
// into the per-candidate pace scores the pure ranking consumes. It is the input
// adapter shared by a live selection and the settings preview, so both answer
// from the same evidence.
//
// A candidate whose usage cannot be observed stays unknown. Unknown sorts after
// known pace, so an unavailable reading can never make a busy candidate look
// idle.
type dynamicUsageSnapshot struct {
	usage  *usageProviderAdapter
	manual manualWindowTotalsReader
	now    func() time.Time
}

func newDynamicUsageSnapshot(
	usage *usageProviderAdapter,
	manual manualWindowTotalsReader,
	now func() time.Time,
) *dynamicUsageSnapshot {
	if now == nil {
		now = time.Now
	}
	return &dynamicUsageSnapshot{usage: usage, manual: manual, now: now}
}

// UsageSnapshot implements dynamic.UsageSnapshotProvider.
func (s *dynamicUsageSnapshot) UsageSnapshot(
	ctx context.Context,
	profile dynamicruntime.Profile,
) (map[string]dynamicruntime.PaceScore, error) {
	observedAt := s.now()
	scores := make(map[string]dynamicruntime.PaceScore, len(profile.Candidates))
	for _, candidate := range profile.Candidates {
		scores[candidate.ID] = s.scoreFor(ctx, candidate, observedAt)
	}
	return scores, nil
}

func (s *dynamicUsageSnapshot) scoreFor(
	ctx context.Context,
	candidate dynamicruntime.Candidate,
	observedAt time.Time,
) dynamicruntime.PaceScore {
	switch candidate.Selection.Model.UsageSource {
	case dynamicruntime.UsageAutomatic:
		return s.automaticScore(ctx, candidate, observedAt)
	case dynamicruntime.UsageManual:
		return s.manualScore(ctx, candidate, observedAt)
	default:
		// No usage source configured: free candidates that are explicitly
		// windowless are a known zero, everything else is unknown.
		if candidate.Selection.Model.Cost == dynamicruntime.CostFree {
			return dynamicruntime.PaceScore{
				Known: true, Complete: true, Controlling: freeNoWindowLabel, ObservedAt: observedAt,
			}
		}
		return dynamicruntime.PaceScore{ObservedAt: observedAt}
	}
}

const freeNoWindowLabel = "no_usage_window"

// automaticScore reads the candidate's own account binding. It never borrows
// another profile's usage: an unavailable binding stays unknown.
func (s *dynamicUsageSnapshot) automaticScore(
	ctx context.Context,
	candidate dynamicruntime.Candidate,
	observedAt time.Time,
) dynamicruntime.PaceScore {
	if s.usage == nil {
		return dynamicruntime.PaceScore{ObservedAt: observedAt}
	}
	observed, err := s.usage.GetUsage(ctx, candidate.ID)
	if err != nil || observed == nil || len(observed.Windows) == 0 {
		return dynamicruntime.PaceScore{ObservedAt: observedAt}
	}
	windows := make([]dynamicruntime.WindowObservation, 0, len(observed.Windows))
	for _, window := range observed.Windows {
		if !window.UsableFor("") {
			continue
		}
		fraction := window.UtilizationPct / 100
		windows = append(windows, dynamicruntime.WindowObservation{
			Label:         window.Label,
			UsageFraction: &fraction,
			StartAt:       window.StartAt,
			ResetAt:       window.ResetAt,
			ObservedAt:    observed.FetchedAt,
		})
	}
	return dynamicruntime.PaceFromWindows(observedAt, windows)
}

// manualScore aggregates recorded ledger usage for the configured windows. The
// scope is the concrete execution profile, never the logical dynamic profile.
func (s *dynamicUsageSnapshot) manualScore(
	ctx context.Context,
	candidate dynamicruntime.Candidate,
	observedAt time.Time,
) dynamicruntime.PaceScore {
	if s.manual == nil {
		return dynamicruntime.PaceScore{ObservedAt: observedAt}
	}
	windows := make([]dynamicruntime.WindowObservation, 0, len(candidate.Selection.Model.Windows))
	for _, configured := range candidate.Selection.Model.Windows {
		observation, ok := s.resolveManualWindow(ctx, candidate.ID, configured, observedAt)
		if !ok {
			continue
		}
		windows = append(windows, observation)
	}
	score := dynamicruntime.PaceFromWindows(observedAt, windows)
	if !score.Known && score.HasRecord {
		// A partial recorded total stays unknown for ranking but keeps the lower
		// bound visible so a preview can explain what is actually known.
		score.Complete = false
	}
	return score
}

func (s *dynamicUsageSnapshot) resolveManualWindow(
	ctx context.Context,
	executionProfileID string,
	configured dynamicruntime.UsageWindow,
	observedAt time.Time,
) (dynamicruntime.WindowObservation, bool) {
	location, err := time.LoadLocation(configured.Timezone)
	if err != nil {
		return dynamicruntime.WindowObservation{}, false
	}
	anchor, err := agentusage.ParseResetAnchor(configured.ResetAnchor)
	if err != nil {
		return dynamicruntime.WindowObservation{}, false
	}
	resolved, ok := agentusage.ResolveResetWindow(observedAt, configured.Period, anchor, location)
	if !ok {
		return dynamicruntime.WindowObservation{}, false
	}
	limit, ok := configured.LimitValue()
	if !ok {
		return dynamicruntime.WindowObservation{}, false
	}
	// The interval is half-open and clamped to the reset, so an event exactly at
	// the reset instant belongs to the next window.
	end := observedAt
	if resolved.Reset.Before(end) {
		end = resolved.Reset
	}
	totals, err := s.manual.GetManualWindowUsage(ctx, executionProfileID, resolved.Start, end)
	if err != nil {
		return dynamicruntime.WindowObservation{}, false
	}
	used, ok := recordedFraction(totals, configured.Unit, limit)
	if !ok {
		return dynamicruntime.WindowObservation{}, false
	}
	return dynamicruntime.WindowObservation{
		Label:         configured.Period,
		UsageFraction: &used,
		StartAt:       resolved.Start,
		ResetAt:       resolved.Reset,
		ObservedAt:    observedAt,
	}, true
}

// recordedFraction converts a recorded total into a fraction of the allowance.
// Money uses the ledger's USD subcent precision, so the limit is scaled by 100
// with exact decimal arithmetic rather than by binary floating point.
func recordedFraction(
	totals sqliterepo.ManualWindowUsage,
	unit string,
	limit *big.Rat,
) (float64, bool) {
	var recorded *big.Rat
	switch unit {
	case agentusage.WindowUnitMoney:
		// A money total with an unpriced or incompletely measured event stays a
		// recorded lower bound rather than a complete figure.
		if totals.UnpricedCount > 0 || totals.IncompleteCount > 0 {
			return 0, false
		}
		recorded = new(big.Rat).SetInt64(totals.CostSubcents)
		limit = new(big.Rat).Mul(limit, big.NewRat(100, 1))
	case agentusage.WindowUnitTokens:
		recorded = new(big.Rat).SetInt64(totals.TokensTotal)
	default:
		return 0, false
	}
	if limit.Sign() <= 0 {
		return 0, false
	}
	fraction := new(big.Rat).Quo(recorded, limit)
	value, _ := fraction.Float64()
	return value, true
}

// PreviewDynamicSelection implements the settings controller's read-only preview
// seam. It reuses the same usage snapshot and the same pure preview the engine
// uses, so an identical profile, clock, health and usage state answers
// identically on both paths.
func (s *dynamicUsageSnapshot) PreviewDynamicSelection(
	ctx context.Context,
	profile dynamicruntime.Profile,
	ineligible map[string]string,
	now time.Time,
) dynamicruntime.SelectionPreview {
	scores, _ := s.UsageSnapshot(ctx, profile)
	return dynamicruntime.PreviewSelection(profile, scores, ineligible, "", dynamicruntime.SelectionChain{}, now)
}
