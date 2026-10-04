package backendapp

import (
	"context"
	"errors"
	"math/big"
	"math/rand/v2"
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
	usage    *usageProviderAdapter
	manual   manualWindowTotalsReader
	health   PreviewHealthReader
	internal *accountUsageReader
	history  dynamicruntime.SelectionHistory
	now      func() time.Time
}

// WithInternalUsage orders candidates with unknown provider usage by Kandev's
// own recorded account usage instead of leaving them indistinguishable.
func (s *dynamicUsageSnapshot) WithInternalUsage(reader *accountUsageReader) *dynamicUsageSnapshot {
	s.internal = reader
	return s
}

// WithSelectionHistory lets a preview continue a round-robin tier from the
// candidate the live engine chose last.
func (s *dynamicUsageSnapshot) WithSelectionHistory(history dynamicruntime.SelectionHistory) *dynamicUsageSnapshot {
	s.history = history
	return s
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
		score := s.scoreFor(ctx, candidate, observedAt)
		if !score.Known && s.internal != nil {
			score.Internal = s.internal.Internal(ctx, candidate.ID, observedAt)
		}
		scores[candidate.ID] = score
	}
	return scores, nil
}

func (s *dynamicUsageSnapshot) scoreFor(
	ctx context.Context,
	candidate dynamicruntime.Candidate,
	observedAt time.Time,
) dynamicruntime.PaceScore {
	source := candidate.Selection.Model.UsageSource
	if source == dynamicruntime.UsageManual {
		return s.manualScore(ctx, candidate, observedAt)
	}
	// Usage is a property of the concrete profile's account, so any row that
	// does not override it with manual windows ranks on the profile's own
	// reading when one exists.
	if score, ok := s.profileScore(ctx, candidate, observedAt); ok {
		return score
	}
	if source == dynamicruntime.UsageAutomatic {
		return dynamicruntime.PaceScore{ObservedAt: observedAt}
	}
	// Without a reading, a free candidate that is explicitly windowless is a
	// known zero, and everything else is unknown.
	if candidate.Selection.Model.Cost == dynamicruntime.CostFree {
		return dynamicruntime.PaceScore{
			Known: true, Complete: true, Controlling: freeNoWindowLabel, ObservedAt: observedAt,
		}
	}
	return dynamicruntime.PaceScore{ObservedAt: observedAt}
}

const freeNoWindowLabel = "no_usage_window"

// profileScore reads the candidate's own account binding. It never borrows
// another profile's usage: an unavailable binding reports no reading.
func (s *dynamicUsageSnapshot) profileScore(
	ctx context.Context,
	candidate dynamicruntime.Candidate,
	observedAt time.Time,
) (dynamicruntime.PaceScore, bool) {
	// AC-003.3: automatic usage may only answer for a candidate whose execution
	// is the backend host, because that is the only place whose provider
	// credentials are the candidate's own account. A container, SSH or
	// Kubernetes execution authenticates somewhere else, so the host's reading
	// is another account's consumption and the answer is unknown rather than a
	// confident wrong number. Manual windows are unaffected: they are summed
	// from the task ledger by concrete candidate, not read from host credentials.
	if s.usage == nil || candidate.RemoteExecution {
		return dynamicruntime.PaceScore{}, false
	}
	observed := s.usage.ProfileUsage(ctx, candidate.ID)
	if observed.State != profileUsageOK || observed.Usage == nil {
		return dynamicruntime.PaceScore{}, false
	}
	windows := applicableAutomaticWindows(observed.ModelID, observed.Usage)
	if len(windows) == 0 {
		return dynamicruntime.PaceScore{}, false
	}
	score := dynamicruntime.PaceFromWindows(observedAt, windows)
	if !score.Known {
		return dynamicruntime.PaceScore{}, false
	}
	return score, true
}

// applicableAutomaticWindows keeps only the windows that are this candidate's own
// usage. A window scoped to a different model is that model's consumption, and a
// window with no usable length or reset is not a measurement at all, so both are
// skipped rather than borrowed, unless the provider reports it exhausted: an
// exhausted account is known to be busy even when its reset is not published.
// An account-wide window applies to whichever model the candidate launches.
func applicableAutomaticWindows(
	candidateModelID string,
	observed *agentusage.ProviderUsage,
) []dynamicruntime.WindowObservation {
	windows := make([]dynamicruntime.WindowObservation, 0, len(observed.Windows))
	for _, window := range observed.Windows {
		usable := window.UsableFor(candidateModelID)
		exhaustedAccountWindow := window.Exhausted() && window.ModelID == "" && !window.AmbiguousModelScope
		if !usable && !exhaustedAccountWindow {
			continue
		}
		fraction := window.UtilizationPct / 100
		windows = append(windows, dynamicruntime.WindowObservation{
			Label:         window.Label,
			UsageFraction: &fraction,
			StartAt:       window.StartAt,
			ResetAt:       window.ResetAt,
			ObservedAt:    observed.FetchedAt,
			Exhausted:     window.Exhausted(),
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
	totals, err := s.recordedTotals(ctx, executionProfileID, configured, resolved.Start, end)
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

// accountWindowTotalsReader sums several profiles' recorded usage, which an
// account-scoped window needs. It is optional so a narrower reader still serves
// candidate-scoped windows.
type accountWindowTotalsReader interface {
	GetManualWindowUsageForProfiles(
		ctx context.Context,
		executionProfileIDs []string,
		start time.Time,
		end time.Time,
	) (sqliterepo.ManualWindowUsage, error)
}

// recordedTotals sums the window's recorded usage. An account window counts
// every profile bound to the candidate's provider account; when the account
// cannot be resolved the window has no answer rather than silently shrinking
// to the candidate's own share of the quota.
func (s *dynamicUsageSnapshot) recordedTotals(
	ctx context.Context,
	executionProfileID string,
	configured dynamicruntime.UsageWindow,
	start time.Time,
	end time.Time,
) (sqliterepo.ManualWindowUsage, error) {
	if !configured.AccountScoped() {
		return s.manual.GetManualWindowUsage(ctx, executionProfileID, start, end)
	}
	reader, ok := s.manual.(accountWindowTotalsReader)
	if !ok || s.usage == nil {
		return sqliterepo.ManualWindowUsage{}, errAccountWindowUnavailable
	}
	profileIDs, err := s.usage.AccountProfileIDs(ctx, executionProfileID)
	if err != nil {
		return sqliterepo.ManualWindowUsage{}, err
	}
	return reader.GetManualWindowUsageForProfiles(ctx, profileIDs, start, end)
}

var errAccountWindowUnavailable = errors.New("account usage window cannot be resolved")

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
	suspensions := s.previewSuspensions(ctx, profile, now)
	for _, candidate := range profile.Candidates {
		if _, decided := merged[candidate.ID]; decided {
			continue
		}
		if code := suspensionIneligibility(suspensions[candidate.ID]); code != "" {
			merged[candidate.ID] = code
			continue
		}
		if _, detailed := suspensions[candidate.ID]; detailed {
			continue
		}
		if open, known := s.circuitOpen(ctx, candidate.ID, now); known && open {
			merged[candidate.ID] = dynamicruntime.IneligibleCircuit
		}
	}
	inputs := dynamicruntime.RankOptions{Scores: scores, Pick: rand.IntN, LastPicked: s.lastPicked(ctx, profile)}
	return dynamicruntime.PreviewSelectionWith(profile, inputs, merged, "", dynamicruntime.SelectionChain{}, now).
		WithSuspensions(suspensions)
}

// lastPicked reads the round-robin history for a saved profile. A draft that
// has never been saved has no history and starts from its first row.
func (s *dynamicUsageSnapshot) lastPicked(ctx context.Context, profile dynamicruntime.Profile) map[string]string {
	if s.history == nil || profile.ID == "" {
		return nil
	}
	last, err := s.history.LastSelections(ctx, profile.ID)
	if err != nil {
		return nil
	}
	return dynamicruntime.LastPickedByTier(profile, last)
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
