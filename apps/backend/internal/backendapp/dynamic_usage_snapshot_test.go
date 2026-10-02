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
	manual := &fakeManualTotals{totals: sqliterepo.ManualWindowUsage{
		CostSubcents: 5000, TokensTotal: 900, EventCount: 2,
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
	// 5000 subcents against a 100.00 USD allowance is half of the month, and the
	// window has barely elapsed, so the pace must be well above the fraction.
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
// not presented as a usable pace, and not as zero usage either.
func TestSnapshotManualIncompleteTotalsStayUnknown(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	manual := &fakeManualTotals{totals: sqliterepo.ManualWindowUsage{
		CostSubcents: 5000, EventCount: 2, UnpricedCount: 1,
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
		t.Fatalf("score = %#v, want an unpriced money total to stay unknown", score)
	}
	if score.HasRecord {
		t.Fatalf("score = %#v, want no usable record when the total cannot be priced", score)
	}
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
	totals := sqliterepo.ManualWindowUsage{CostSubcents: 100, TokensTotal: 100, EventCount: 1}
	limit := mustRat(t, "100")
	if _, ok := recordedFraction(totals, "credits", limit); ok {
		t.Fatal("unknown unit accepted, want rejection")
	}
	if _, ok := recordedFraction(totals, "0", limit); ok {
		t.Fatal("nonpositive limit accepted, want rejection")
	}
	money, ok := recordedFraction(totals, agentusage.WindowUnitMoney, limit)
	if !ok || money != 0.01 {
		t.Fatalf("money fraction = %v (ok=%v), want 0.01", money, ok)
	}
	tokens, ok := recordedFraction(totals, agentusage.WindowUnitTokens, limit)
	if !ok || tokens != 1 {
		t.Fatalf("token fraction = %v (ok=%v), want 1", tokens, ok)
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
