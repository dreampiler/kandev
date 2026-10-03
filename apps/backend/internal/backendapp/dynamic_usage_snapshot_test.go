package backendapp

import (
	"context"
	"math/big"
	"testing"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
)

type fakeManualTotals struct {
	totals sqliterepo.ManualWindowUsage
	err    error
	calls  int
}

func (f *fakeManualTotals) GetManualWindowUsage(
	_ context.Context,
	_ string,
	_ time.Time,
	_ time.Time,
) (sqliterepo.ManualWindowUsage, error) {
	f.calls++
	return f.totals, f.err
}

func snapshotAt(now time.Time, manual manualWindowTotalsReader) *dynamicUsageSnapshot {
	return newDynamicUsageSnapshot(nil, manual, func() time.Time { return now })
}

func automaticCandidate(id string, source dynamicruntime.UsageSource, cost dynamicruntime.CostClass) dynamicruntime.Candidate {
	return dynamicruntime.Candidate{
		ID: id, Enabled: true,
		Selection: dynamicruntime.Selection{Model: dynamicruntime.ModelOptions{
			Cost: cost, UsageSource: source,
		}},
	}
}

func TestSnapshotNoneSourceIsUnknownUnlessFree(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	snapshot := snapshotAt(now, nil)
	scores, err := snapshot.UsageSnapshot(context.Background(), dynamicruntime.Profile{
		Candidates: []dynamicruntime.Candidate{
			automaticCandidate("metered", dynamicruntime.UsageNone, dynamicruntime.CostMetered),
			automaticCandidate("free", dynamicruntime.UsageNone, dynamicruntime.CostFree),
		},
	})
	if err != nil {
		t.Fatalf("UsageSnapshot: %v", err)
	}
	// An unobservable subscription must not look idle.
	if scores["metered"].Known || scores["metered"].HasRecord {
		t.Fatalf("metered = %#v, want unknown rather than a zero", scores["metered"])
	}
	// An explicitly windowless free candidate is a known zero, and stays
	// subject to its circuit in the caller.
	if !scores["free"].Known || scores["free"].Pace != 0 {
		t.Fatalf("free = %#v, want a known zero pace", scores["free"])
	}
}

func TestSnapshotManualComputesMoneyFractionAgainstTheLedger(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	// The ledger stores hundredths of a cent, so half of a $100 month is
	// 500,000 of them, not 5,000. Getting this constant wrong scales every
	// reported money fraction by 100.
	manual := &fakeManualTotals{totals: sqliterepo.ManualWindowUsage{
		CostSubcents: 500_000, TokensTotal: 900, EventCount: 2,
	}}
	candidate := automaticCandidate("manual", dynamicruntime.UsageManual, dynamicruntime.CostSubscription)
	candidate.Selection.Model.Windows = []dynamicruntime.UsageWindow{{
		Period: agentusage.WindowPeriodMonth, Unit: agentusage.WindowUnitMoney,
		Limit: "100.00", ResetAnchor: "00:00 day 1", Timezone: "UTC",
	}}
	scores, err := snapshotAt(now, manual).UsageSnapshot(context.Background(), dynamicruntime.Profile{
		Candidates: []dynamicruntime.Candidate{candidate},
	})
	if err != nil {
		t.Fatalf("UsageSnapshot: %v", err)
	}
	score := scores["manual"]
	// Half of the 100.00 USD allowance, with the window barely elapsed, so the
	// pace must be well above the fraction.
	if !score.Known {
		t.Fatalf("score = %#v, want a known pace", score)
	}
	if score.UsageFraction != 0.5 {
		t.Fatalf("usage fraction = %v, want 0.5", score.UsageFraction)
	}
	if manual.calls != 1 {
		t.Fatalf("ledger calls = %d, want exactly one window query", manual.calls)
	}
}

func TestSnapshotManualTokensFraction(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	manual := &fakeManualTotals{totals: sqliterepo.ManualWindowUsage{TokensTotal: 1000, EventCount: 1}}
	candidate := automaticCandidate("tokens", dynamicruntime.UsageManual, dynamicruntime.CostSubscription)
	candidate.Selection.Model.Windows = []dynamicruntime.UsageWindow{{
		Period: agentusage.WindowPeriodFiveHour, Unit: agentusage.WindowUnitTokens,
		Limit: "10000", ResetAnchor: "07:00", Timezone: "UTC",
	}}
	scores, _ := snapshotAt(now, manual).UsageSnapshot(context.Background(), dynamicruntime.Profile{
		Candidates: []dynamicruntime.Candidate{candidate},
	})
	if !scores["tokens"].Known || scores["tokens"].UsageFraction != 0.1 {
		t.Fatalf("score = %#v, want a known 10%% token fraction", scores["tokens"])
	}
}

// TestSnapshotManualIncompleteTotalsStayUnknown pins that partial accounting is
// not presented as a usable pace and not as zero usage, while still remaining
// visible as the lower bound AC-003.4 requires. Discarding the figure entirely
// would make a partial total indistinguishable from no measurement at all.
func TestSnapshotManualIncompleteTotalsStayUnknown(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	manual := &fakeManualTotals{totals: sqliterepo.ManualWindowUsage{
		CostSubcents: 500_000, EventCount: 2, UnpricedCount: 1,
	}}
	candidate := automaticCandidate("partial", dynamicruntime.UsageManual, dynamicruntime.CostSubscription)
	candidate.Selection.Model.Windows = []dynamicruntime.UsageWindow{{
		Period: agentusage.WindowPeriodMonth, Unit: agentusage.WindowUnitMoney,
		Limit: "100.00", ResetAnchor: "00:00 day 1", Timezone: "UTC",
	}}
	scores, _ := snapshotAt(now, manual).UsageSnapshot(context.Background(), dynamicruntime.Profile{
		Candidates: []dynamicruntime.Candidate{candidate},
	})
	score := scores["partial"]
	if score.Known {
		t.Fatalf("score = %#v, want an unpriced money total to stay unknown for ranking", score)
	}
	if !score.HasRecord {
		t.Fatalf("score = %#v, want the recorded lower bound to stay visible", score)
	}
	if score.Complete {
		t.Fatalf("score = %#v, want an incomplete total to report itself incomplete", score)
	}
	if score.UsageFraction != 0.5 {
		t.Fatalf("lower bound = %v, want the recorded 0.5 of the month", score.UsageFraction)
	}
}

// TestSnapshotManualPartialTotalsDoNotWinRanking pins that a visible lower bound
// never becomes a pace. Ranking on an undercount would overstate the candidate's
// remaining capacity, which is the exact failure the unknown state prevents.
func TestSnapshotManualPartialTotalsDoNotWinRanking(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	busy := &fakeManualTotals{totals: sqliterepo.ManualWindowUsage{
		CostSubcents: 900_000, EventCount: 3,
	}}
	partial := &fakeManualTotals{totals: sqliterepo.ManualWindowUsage{
		CostSubcents: 0, EventCount: 1, UnpricedCount: 1,
	}}
	busyCandidate := automaticCandidate("busy", dynamicruntime.UsageManual, dynamicruntime.CostSubscription)
	busyCandidate.Selection.Model.Windows = []dynamicruntime.UsageWindow{{
		Period: agentusage.WindowPeriodMonth, Unit: agentusage.WindowUnitMoney,
		Limit: "100.00", ResetAnchor: "00:00 day 1", Timezone: "UTC",
	}}
	partialCandidate := automaticCandidate("partial", dynamicruntime.UsageManual, dynamicruntime.CostSubscription)
	partialCandidate.Selection.Model.Windows = busyCandidate.Selection.Model.Windows

	adapter := &splitManualTotals{busy: busy, partial: partial}
	scores, err := adapter.snapshot(now).UsageSnapshot(context.Background(), dynamicruntime.Profile{
		Candidates: []dynamicruntime.Candidate{busyCandidate, partialCandidate},
	})
	if err != nil {
		t.Fatalf("UsageSnapshot: %v", err)
	}
	if !scores["busy"].Known {
		t.Fatalf("busy score = %#v, want a known pace", scores["busy"])
	}
	if scores["partial"].Known {
		t.Fatalf("partial score = %#v, want an unknown pace so it cannot outrank the busy candidate", scores["partial"])
	}

	ranked := dynamicruntime.RankTier(dynamicruntime.Tier{
		Index:      1,
		Candidates: []dynamicruntime.Candidate{busyCandidate, partialCandidate},
	}, dynamicruntime.RankOptions{Now: now, Scores: scores})
	winner, ok := dynamicruntime.FirstEligible(ranked)
	if !ok || winner.Candidate.ID != "busy" {
		t.Fatalf("winner = %#v, want the candidate with complete evidence", winner)
	}
}

// splitManualTotals answers per execution profile so one snapshot can compare a
// complete total against a partial one.
type splitManualTotals struct {
	busy    *fakeManualTotals
	partial *fakeManualTotals
}

func (s *splitManualTotals) GetManualWindowUsage(
	_ context.Context, executionProfileID string, _, _ time.Time,
) (sqliterepo.ManualWindowUsage, error) {
	if executionProfileID == "partial" {
		return s.partial.totals, s.partial.err
	}
	return s.busy.totals, s.busy.err
}

func (s *splitManualTotals) snapshot(now time.Time) *dynamicUsageSnapshot {
	return &dynamicUsageSnapshot{manual: s, now: func() time.Time { return now }}
}

func TestSnapshotManualUnknownTimezoneOrAnchorStaysUnknown(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	manual := &fakeManualTotals{totals: sqliterepo.ManualWindowUsage{CostSubcents: 1, EventCount: 1}}
	build := func(anchor, timezone string) dynamicruntime.Candidate {
		candidate := automaticCandidate("bad", dynamicruntime.UsageManual, dynamicruntime.CostSubscription)
		candidate.Selection.Model.Windows = []dynamicruntime.UsageWindow{{
			Period: agentusage.WindowPeriodMonth, Unit: agentusage.WindowUnitMoney,
			Limit: "100.00", ResetAnchor: anchor, Timezone: timezone,
		}}
		return candidate
	}
	for name, candidate := range map[string]dynamicruntime.Candidate{
		"unknown timezone": build("00:00", "Mars/Olympus"),
		"invalid anchor":   build("ninety", "UTC"),
		"unknown unit":     buildWithUnit(build("00:00", "UTC"), "credits"),
	} {
		t.Run(name, func(t *testing.T) {
			scores, _ := snapshotAt(now, manual).UsageSnapshot(context.Background(), dynamicruntime.Profile{
				Candidates: []dynamicruntime.Candidate{candidate},
			})
			if scores["bad"].Known {
				t.Fatalf("score = %#v, want unknown rather than a guessed window", scores["bad"])
			}
		})
	}
}

func buildWithUnit(candidate dynamicruntime.Candidate, unit string) dynamicruntime.Candidate {
	candidate.Selection.Model.Windows[0].Unit = unit
	return candidate
}

func TestSnapshotLedgerFailureStaysUnknown(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	manual := &fakeManualTotals{err: context.DeadlineExceeded}
	candidate := automaticCandidate("failing", dynamicruntime.UsageManual, dynamicruntime.CostSubscription)
	candidate.Selection.Model.Windows = []dynamicruntime.UsageWindow{{
		Period: agentusage.WindowPeriodMonth, Unit: agentusage.WindowUnitMoney,
		Limit: "100.00", ResetAnchor: "00:00 day 1", Timezone: "UTC",
	}}
	scores, _ := snapshotAt(now, manual).UsageSnapshot(context.Background(), dynamicruntime.Profile{
		Candidates: []dynamicruntime.Candidate{candidate},
	})
	if scores["failing"].Known {
		t.Fatalf("score = %#v, want a ledger read failure to be unknown", scores["failing"])
	}
}

func TestSnapshotWithoutManualReaderIsUnknown(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	candidate := automaticCandidate("unwired", dynamicruntime.UsageManual, dynamicruntime.CostSubscription)
	candidate.Selection.Model.Windows = []dynamicruntime.UsageWindow{{
		Period: agentusage.WindowPeriodMonth, Unit: agentusage.WindowUnitMoney,
		Limit: "100.00", ResetAnchor: "00:00 day 1", Timezone: "UTC",
	}}
	scores, _ := snapshotAt(now, nil).UsageSnapshot(context.Background(), dynamicruntime.Profile{
		Candidates: []dynamicruntime.Candidate{candidate},
	})
	if scores["unwired"].Known {
		t.Fatalf("score = %#v, want unknown with no ledger reader", scores["unwired"])
	}
}

// TestRecordedFractionRejectsUnknownUnit pins that an unrecognised unit is
// never treated as a confirmed zero charge.
func TestRecordedFractionRejectsUnknownUnit(t *testing.T) {
	// 100,000 subcents is $10.00, so against a $100 allowance the money fraction
	// is 0.1 while the 100 tokens against the same number is 1.
	totals := sqliterepo.ManualWindowUsage{CostSubcents: 100_000, TokensTotal: 100, EventCount: 1}
	limit := mustRat(t, "100")
	if _, _, ok := recordedFraction(totals, "credits", limit); ok {
		t.Fatal("unknown unit accepted, want rejection")
	}
	if _, _, ok := recordedFraction(totals, "0", limit); ok {
		t.Fatal("nonpositive limit accepted, want rejection")
	}
	money, complete, ok := recordedFraction(totals, agentusage.WindowUnitMoney, limit)
	if !ok || !complete || money != 0.1 {
		t.Fatalf("money fraction = %v (ok=%v complete=%v), want a complete 0.1", money, ok, complete)
	}
	tokens, complete, ok := recordedFraction(totals, agentusage.WindowUnitTokens, limit)
	if !ok || !complete || tokens != 1 {
		t.Fatalf("token fraction = %v (ok=%v complete=%v), want a complete 1", tokens, ok, complete)
	}
}

// TestRecordedFractionUsesTheLedgerSubcentUnit pins the money unit against the
// ledger's own contract. task_usage_events.cost_subcents is hundredths of a cent,
// so one dollar is 10,000 of them; scaling the allowance by anything else reports
// every money fraction at the wrong magnitude while still looking plausible.
func TestRecordedFractionUsesTheLedgerSubcentUnit(t *testing.T) {
	oneDollar := sqliterepo.ManualWindowUsage{CostSubcents: subcentsPerDollar, EventCount: 1}
	fraction, complete, ok := recordedFraction(oneDollar, agentusage.WindowUnitMoney, mustRat(t, "1"))
	if !ok || !complete || fraction != 1 {
		t.Fatalf("one dollar against a one dollar allowance = %v (ok=%v complete=%v), want 1", fraction, ok, complete)
	}
	oneCent, _, ok := recordedFraction(
		sqliterepo.ManualWindowUsage{CostSubcents: subcentsPerDollar / 100, EventCount: 1},
		agentusage.WindowUnitMoney,
		mustRat(t, "0.01"),
	)
	if !ok || oneCent != 1 {
		t.Fatalf("one cent against a one cent allowance = %v (ok=%v), want 1", oneCent, ok)
	}
}

// TestRecordedFractionKeepsAPartialTotalAsALowerBound pins AC-003.4's
// partial-accounting rule: an unpriced or incompletely measured total still
// yields its recorded figure, marked incomplete, so the operator can see "at
// least this much was used" instead of the window vanishing. Discarding it
// would report the same state as no measurement at all.
func TestRecordedFractionKeepsAPartialTotalAsALowerBound(t *testing.T) {
	limit := mustRat(t, "100")
	for name, totals := range map[string]sqliterepo.ManualWindowUsage{
		"unpriced":   {CostSubcents: 100_000, EventCount: 2, UnpricedCount: 1},
		"incomplete": {CostSubcents: 100_000, EventCount: 2, IncompleteCount: 1},
	} {
		t.Run(name, func(t *testing.T) {
			value, complete, ok := recordedFraction(totals, agentusage.WindowUnitMoney, limit)
			if !ok {
				t.Fatal("a partial money total must still produce a recorded figure")
			}
			if complete {
				t.Fatal("a partial money total must not report itself complete")
			}
			if value != 0.1 {
				t.Fatalf("lower bound = %v, want the recorded 0.1", value)
			}
		})
	}
	tokens, complete, ok := recordedFraction(
		sqliterepo.ManualWindowUsage{TokensTotal: 1000, EventCount: 2, IncompleteCount: 1},
		agentusage.WindowUnitTokens, limit,
	)
	if !ok || complete || tokens != 10 {
		t.Fatalf("token lower bound = %v (ok=%v complete=%v), want an incomplete 10", tokens, ok, complete)
	}
}

// TestApplicableAutomaticWindowsExcludesAnotherModelsUsage pins AC-003.3 at the
// filter itself. A model-scoped window belongs to that model, so it must not
// drive a candidate running a different one, and it must not be applied to a
// candidate whose own model is unknown.
func TestApplicableAutomaticWindowsExcludesAnotherModelsUsage(t *testing.T) {
	reset := time.Date(2026, 10, 2, 17, 0, 0, 0, time.UTC)
	window := func(modelID string, pct float64) agentusage.UtilizationWindow {
		return agentusage.UtilizationWindow{
			Label: "5-hour", UtilizationPct: pct, ResetAt: reset,
			DurationSeconds: 18000, StartAt: reset.Add(-5 * time.Hour), ModelID: modelID,
		}
	}
	observed := func(windows ...agentusage.UtilizationWindow) *agentusage.ProviderUsage {
		return &agentusage.ProviderUsage{Provider: "anthropic", Windows: windows, FetchedAt: reset}
	}

	if got := applicableAutomaticWindows("claude-sonnet-5", observed(window("claude-opus-5", 80))); len(got) != 0 {
		t.Fatalf("windows = %#v, want a sibling model's window excluded", got)
	}
	if got := applicableAutomaticWindows("", observed(window("claude-opus-5", 80))); len(got) != 0 {
		t.Fatalf("windows = %#v, want a model-scoped window excluded for an unidentified candidate", got)
	}
	if got := applicableAutomaticWindows("claude-opus-5", observed(window("claude-opus-5", 80))); len(got) != 1 {
		t.Fatalf("windows = %#v, want the candidate's own window kept", got)
	}
	if got := applicableAutomaticWindows("claude-sonnet-5", observed(window("", 80))); len(got) != 1 {
		t.Fatalf("windows = %#v, want an account-wide window to apply to any model", got)
	}
}

// stubHealth answers a fixed set of paused candidates.
type stubHealth struct {
	open    map[string]bool
	unknown map[string]bool
}

func (s stubHealth) OpenCircuit(_ context.Context, executionProfileID string, _ time.Time) (bool, bool) {
	if s.unknown[executionProfileID] {
		return false, false
	}
	return s.open[executionProfileID], true
}

// TestPreviewHonorsRouteHealth pins AC-005.2's agreement clause. Route health is
// one of the inputs a real selection consults, so a preview that ignored it
// could name a candidate the engine would refuse. The paused row must be
// reported as ineligible, and the engine's own ranking must then agree with the
// preview's choice.
func TestPreviewHonorsRouteHealth(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	paused := automaticCandidate("paused", dynamicruntime.UsageNone, dynamicruntime.CostFree)
	paused.BindingKey = "credential:paused"
	fallback := automaticCandidate("fallback", dynamicruntime.UsageNone, dynamicruntime.CostSubscription)
	paused.Selection.Tier = &dynamicruntime.TierPolicy{Mode: dynamicruntime.TierModeCost, OnFailure: dynamicruntime.FailureSameTierNext}
	fallback.Selection.Tier = paused.Selection.Tier
	profile := dynamicruntime.Profile{ID: "p", Version: 1, Candidates: []dynamicruntime.Candidate{paused, fallback}}

	snapshot := newDynamicUsageSnapshot(nil, nil, func() time.Time { return now })
	snapshot.WithPreviewHealth(stubHealth{open: map[string]bool{"paused": true}})

	preview := snapshot.PreviewDynamicSelection(context.Background(), profile, nil, now)
	if preview.State() != dynamicruntime.PreviewReady {
		t.Fatalf("state = %q, want a ready prediction naming the healthy row", preview.State())
	}
	if preview.CandidateID != "fallback" {
		t.Fatalf("preview chose %q, want the candidate whose binding is not paused", preview.CandidateID)
	}
	for _, considered := range preview.Considered {
		if considered.CandidateID != "paused" {
			continue
		}
		if considered.Eligible {
			t.Fatal("the paused candidate is reported eligible, want the circuit reason")
		}
		if considered.IneligibleReason != dynamicruntime.IneligibleCircuit {
			t.Fatalf("reason = %q, want %q", considered.IneligibleReason, dynamicruntime.IneligibleCircuit)
		}
	}

	// The engine's ranking over the same eligibility must reach the same choice.
	ranked := dynamicruntime.RankTier(dynamicruntime.Tier{
		Index: 1, Policy: *paused.Selection.Tier, Candidates: profile.Candidates,
	}, dynamicruntime.RankOptions{
		Now: now, Eligible: map[string]string{"paused": dynamicruntime.IneligibleCircuit},
	})
	winner, ok := dynamicruntime.FirstEligible(ranked)
	if !ok || winner.Candidate.ID != preview.CandidateID {
		t.Fatalf("ranking chose %q but the preview chose %q, want agreement", winner.Candidate.ID, preview.CandidateID)
	}
}

// TestPreviewWithoutHealthReportsNothingIneligible pins the degraded case. With
// no health reader the preview must not invent a verdict, and it must not mark a
// candidate ineligible on a health claim it cannot support either.
func TestPreviewWithoutHealthReportsNothingIneligible(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	candidate := automaticCandidate("only", dynamicruntime.UsageNone, dynamicruntime.CostFree)
	candidate.Selection.Tier = &dynamicruntime.TierPolicy{Mode: dynamicruntime.TierModeOrder, OnFailure: dynamicruntime.FailureSameTierNext}
	profile := dynamicruntime.Profile{ID: "p", Version: 1, Candidates: []dynamicruntime.Candidate{candidate}}

	preview := snapshotAt(now, nil).PreviewDynamicSelection(context.Background(), profile, nil, now)
	if preview.State() != dynamicruntime.PreviewReady || preview.CandidateID != "only" {
		t.Fatalf("preview = %#v, want the candidate reported as the choice without a health claim", preview)
	}
	for _, considered := range preview.Considered {
		if considered.IneligibleReason != "" {
			t.Fatalf("reason = %q, want no health claim without a reader", considered.IneligibleReason)
		}
	}
}

func mustRat(t *testing.T, value string) *big.Rat {
	t.Helper()
	parsed, ok := new(big.Rat).SetString(value)
	if !ok {
		t.Fatalf("parse %q", value)
	}
	return parsed
}
