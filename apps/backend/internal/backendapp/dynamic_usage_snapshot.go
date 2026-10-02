package backendapp

import (
	"context"
	"math/big"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
)

// subcentsPerDollar is the ledger's money unit for task_usage_events and
// office_cost_events: hundredths of a cent, so one dollar is 10,000 of them.
const subcentsPerDollar = 10_000

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
	health PreviewHealthReader
	now    func() time.Time
}

// PreviewHealthReader reports the route-health verdict for one candidate's own
// credential binding. It is the same circuit state the selection engine consults,
// so a preview and an actual selection agree on health as well as on usage.
type PreviewHealthReader interface {
	// OpenCircuit reports whether the candidate's binding is currently paused
	// after a provider failure. A binding that cannot be resolved is not open:
	// the caller then reports unknown rather than inventing a verdict.
	OpenCircuit(ctx context.Context, executionProfileID string, now time.Time) (open bool, known bool)
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

// WithPreviewHealth injects the shared route-health reader. Without it a preview
// can still name a choice, but it cannot account for a paused binding, so the
// wiring is explicit rather than assumed.
func (s *dynamicUsageSnapshot) WithPreviewHealth(reader PreviewHealthReader) *dynamicUsageSnapshot {
	s.health = reader
	return s
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
	return dynamicruntime.PaceFromWindows(
		observedAt, applicableAutomaticWindows(candidate.ModelID, observed),
	)
}

// applicableAutomaticWindows keeps only the windows that are this candidate's own
// usage. A window scoped to a different model is that model's consumption, and a
// window with no usable length or reset is not a measurement at all, so both are
// skipped rather than borrowed. An account-wide window applies to whichever model
// the candidate launches.
func applicableAutomaticWindows(
	candidateModelID string,
	observed *agentusage.ProviderUsage,
) []dynamicruntime.WindowObservation {
	windows := make([]dynamicruntime.WindowObservation, 0, len(observed.Windows))
	for _, window := range observed.Windows {
		if !window.UsableFor(candidateModelID) {
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
	return windows
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
	used, complete, ok := recordedFraction(totals, configured.Unit, limit)
	if !ok {
		return dynamicruntime.WindowObservation{}, false
	}
	return dynamicruntime.WindowObservation{
		Label:         configured.Period,
		UsageFraction: &used,
		StartAt:       resolved.Start,
		ResetAt:       resolved.Reset,
		ObservedAt:    observedAt,
		Partial:       !complete,
	}, true
}

// recordedFraction converts a recorded total into a fraction of the allowance.
// Money uses the ledger's USD subcent precision, so the limit is scaled into that
// unit with exact decimal arithmetic rather than by binary floating point. The
// ledger stores hundredths of a cent, so one dollar is 10,000 of them.
//
// A total with an unpriced or incompletely measured event is still converted.
// That figure undercounts, so it is returned as a lower bound with complete
// false rather than discarded: the caller keeps it visible and refuses to rank
// on it, which is what lets a preview say "at least this much" instead of
// silently reporting nothing.
func recordedFraction(
	totals sqliterepo.ManualWindowUsage,
	unit string,
	limit *big.Rat,
) (value float64, complete bool, ok bool) {
	var recorded *big.Rat
	switch unit {
	case agentusage.WindowUnitMoney, agentusage.WindowUnitTokens:
		if unit == agentusage.WindowUnitMoney {
			recorded = new(big.Rat).SetInt64(totals.CostSubcents)
			limit = new(big.Rat).Mul(limit, big.NewRat(subcentsPerDollar, 1))
		} else {
			recorded = new(big.Rat).SetInt64(totals.TokensTotal)
		}
	default:
		return 0, false, false
	}
	if limit.Sign() <= 0 {
		return 0, false, false
	}
	fraction := new(big.Rat).Quo(recorded, limit)
	value, _ = fraction.Float64()
	return value, totals.UnpricedCount == 0 && totals.IncompleteCount == 0, true
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
	// Route health is part of what a selection consults, so a preview that
	// ignored it could name a candidate an actual selection would refuse. The
	// caller's codes win: this only adds rows nobody has spoken for.
	merged := make(map[string]string, len(ineligible)+len(profile.Candidates))
	for id, reason := range ineligible {
		merged[id] = reason
	}
	for _, candidate := range profile.Candidates {
		if _, decided := merged[candidate.ID]; decided {
			continue
		}
		if open, known := s.circuitOpen(ctx, candidate.ID, now); known && open {
			merged[candidate.ID] = dynamicruntime.IneligibleCircuit
		}
	}
	return dynamicruntime.PreviewSelection(profile, scores, merged, "", dynamicruntime.SelectionChain{}, now)
}

// circuitOpen answers route health for one candidate. Without a health reader the
// verdict is unknown, and an unknown verdict must not be reported as healthy.
func (s *dynamicUsageSnapshot) circuitOpen(
	ctx context.Context,
	executionProfileID string,
	now time.Time,
) (open bool, known bool) {
	if s.health == nil {
		return false, false
	}
	return s.health.OpenCircuit(ctx, executionProfileID, now)
}
