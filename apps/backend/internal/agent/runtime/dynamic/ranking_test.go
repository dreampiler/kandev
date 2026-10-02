package dynamic

import (
	"testing"
	"time"
)

func windowUsage(value float64) *float64 { return &value }

func paceTestNow() time.Time {
	return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
}

func TestPaceFromWindowsUsesElapsedAndFloor(t *testing.T) {
	now := paceTestNow()
	start := now.Add(-2 * time.Hour) // 25% of a 5-hour window
	tests := []struct {
		name        string
		window      WindowObservation
		wantKnown   bool
		wantPace    float64
		wantFloor   bool
		wantElapsed float64
	}{
		{
			name: "25 percent used at 40 percent elapsed yields 0.625",
			window: WindowObservation{
				Label: "five_hour", UsageFraction: windowUsage(0.25),
				StartAt: start, ResetAt: start.Add(5 * time.Hour),
			},
			wantKnown: true, wantPace: 0.625, wantElapsed: 0.4,
		},
		{
			name: "a just reset window is floored rather than dividing by zero",
			window: WindowObservation{
				Label: "week", UsageFraction: windowUsage(0.10),
				StartAt: now, ResetAt: now.Add(7 * 24 * time.Hour),
			},
			wantKnown: true, wantPace: 0.10 / minPaceElapsed, wantFloor: true, wantElapsed: 0,
		},
		{
			name: "usage above one hundred percent is not capped before dividing",
			window: WindowObservation{
				Label: "day", UsageFraction: windowUsage(1.5),
				StartAt: now.Add(-12 * time.Hour), ResetAt: now.Add(12 * time.Hour),
			},
			wantKnown: true, wantPace: 1.5 / 0.5, wantElapsed: 0.5,
		},
		{
			name: "unknown usage is not a zero",
			window: WindowObservation{
				Label: "week", UsageFraction: nil,
				StartAt: start, ResetAt: start.Add(7 * 24 * time.Hour),
			},
			wantKnown: false,
		},
		{
			name: "an expired window requires refreshed evidence",
			window: WindowObservation{
				Label: "day", UsageFraction: windowUsage(0.5),
				StartAt: start.Add(-24 * time.Hour), ResetAt: start.Add(-time.Hour),
			},
			wantKnown: false,
		},
		{
			name: "a future start is invalid",
			window: WindowObservation{
				Label: "day", UsageFraction: windowUsage(0.5),
				StartAt: now.Add(time.Hour), ResetAt: now.Add(25 * time.Hour),
			},
			wantKnown: false,
		},
		{
			name: "a zero length span is invalid",
			window: WindowObservation{
				Label: "day", UsageFraction: windowUsage(0.5),
				StartAt: now, ResetAt: now,
			},
			wantKnown: false,
		},
		{
			name: "free with an explicit no window is a known zero",
			window: WindowObservation{
				Label: "free", ExplicitNoWindow: true, ObservedAt: now,
			},
			wantKnown: true, wantPace: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := PaceFromWindows(now, []WindowObservation{tt.window})
			if score.Known != tt.wantKnown {
				t.Fatalf("known = %v, want %v", score.Known, tt.wantKnown)
			}
			if !tt.wantKnown {
				return
			}
			if diff := score.Pace - tt.wantPace; diff > 1e-9 || diff < -1e-9 {
				t.Fatalf("pace = %v, want %v", score.Pace, tt.wantPace)
			}
			if score.FloorApplied != tt.wantFloor {
				t.Fatalf("floor applied = %v, want %v", score.FloorApplied, tt.wantFloor)
			}
			if diff := score.ElapsedFraction - tt.wantElapsed; diff > 1e-9 || diff < -1e-9 {
				t.Fatalf("elapsed = %v, want %v", score.ElapsedFraction, tt.wantElapsed)
			}
		})
	}
}

func TestPaceFromWindowsTakesTheWorstApplicableWindow(t *testing.T) {
	now := paceTestNow()
	half := now.Add(-2 * time.Hour)
	score := PaceFromWindows(now, []WindowObservation{
		{Label: "five_hour", UsageFraction: windowUsage(0.25), StartAt: half, ResetAt: half.Add(5 * time.Hour)},
		{Label: "week", UsageFraction: windowUsage(0.30), StartAt: half, ResetAt: half.Add(7 * 24 * time.Hour)},
		{Label: "unusable", UsageFraction: nil, StartAt: half, ResetAt: half},
	})
	// 0.25/0.4 = 0.625 against 0.30/0.0119 = about 25.2 for the week.
	if !score.Known || score.Controlling != "week" {
		t.Fatalf("score = %#v, want the week window to control", score)
	}
}

func TestRankTierKnownPaceSortsFirstAndKeepsRowOrderOnTies(t *testing.T) {
	tier := Tier{Index: 1, Policy: TierPolicy{Mode: TierModePace}, Candidates: []Candidate{
		{ID: "unknown", Selection: Selection{Model: ModelOptions{Cost: CostFree}}},
		{ID: "slow", Selection: Selection{Model: ModelOptions{Cost: CostMetered}}},
		{ID: "fast", Selection: Selection{Model: ModelOptions{Cost: CostFree}}},
	}}
	ranked := RankTier(tier, RankOptions{Now: paceTestNow(), Scores: map[string]PaceScore{
		"slow": {Known: true, Pace: 0.9},
		"fast": {Known: true, Pace: 0.1},
	}})
	winner, ok := FirstEligible(ranked)
	if !ok || winner.Candidate.ID != "fast" {
		t.Fatalf("winner = %#v, want fast", winner)
	}
	// Equal pace must keep saved row order rather than reshuffle.
	tied := RankTier(tier, RankOptions{Now: paceTestNow(), Scores: map[string]PaceScore{
		"slow": {Known: true, Pace: 0.5},
		"fast": {Known: true, Pace: 0.5},
	}})
	if tied[0].Candidate.ID != "slow" || tied[1].Candidate.ID != "fast" {
		t.Fatalf("tied order = %s,%s, want the saved row order", tied[0].Candidate.ID, tied[1].Candidate.ID)
	}
}

func TestRankTierCostUsesConfiguredClassAndLegacyUnknownSortsLast(t *testing.T) {
	tier := Tier{Index: 1, Policy: TierPolicy{Mode: TierModeCost}, Candidates: []Candidate{
		{ID: "legacy", Selection: Selection{Model: ModelOptions{}}},
		{ID: "metered", Selection: Selection{Model: ModelOptions{Cost: CostMetered}}},
		{ID: "free-b", Selection: Selection{Model: ModelOptions{Cost: CostFree}}},
		{ID: "free-a", Selection: Selection{Model: ModelOptions{Cost: CostFree}}},
	}}
	ranked := RankTier(tier, RankOptions{Now: paceTestNow()})
	got := make([]string, 0, len(ranked))
	for _, entry := range ranked {
		got = append(got, entry.Candidate.ID)
	}
	// free-b before free-a proves the row-order tie-break; legacy last proves an
	// unclassified row never wins on a guess.
	want := []string{"free-b", "free-a", "metered", "legacy"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestRankTierOrderModeKeepsListOrder(t *testing.T) {
	tier := Tier{Index: 1, Policy: TierPolicy{Mode: TierModeOrder}, Candidates: []Candidate{
		{ID: "metered", Selection: Selection{Model: ModelOptions{Cost: CostMetered}}},
		{ID: "free", Selection: Selection{Model: ModelOptions{Cost: CostFree}}},
	}}
	ranked := RankTier(tier, RankOptions{Now: paceTestNow(), Scores: map[string]PaceScore{
		"metered": {Known: true, Pace: 0.1},
		"free":    {Known: true, Pace: 9.0},
	}})
	if ranked[0].Candidate.ID != "metered" {
		t.Fatalf("order = %s, want the saved row order", ranked[0].Candidate.ID)
	}
}

func TestRankTierMarksIneligibleCandidatesWithoutReordering(t *testing.T) {
	tier := Tier{Index: 1, Policy: TierPolicy{Mode: TierModePace}, Candidates: []Candidate{
		{ID: "tried", Selection: Selection{Model: ModelOptions{Cost: CostFree}}},
		{ID: "open", Selection: Selection{Model: ModelOptions{Cost: CostFree}}},
		{ID: "circuit", Selection: Selection{Model: ModelOptions{Cost: CostFree}}},
	}}
	ranked := RankTier(tier, RankOptions{
		Now:      paceTestNow(),
		Excluded: map[string]bool{"tried": true},
		Eligible: map[string]string{"circuit": IneligibleCircuit},
	})
	reasons := map[string]string{}
	for _, entry := range ranked {
		reasons[entry.Candidate.ID] = entry.IneligibleReason
	}
	if reasons["tried"] != IneligibleTried || reasons["circuit"] != IneligibleCircuit {
		t.Fatalf("reasons = %#v", reasons)
	}
	winner, ok := FirstEligible(ranked)
	if !ok || winner.Candidate.ID != "open" {
		t.Fatalf("winner = %#v, want the only eligible candidate", winner)
	}
}

func TestReservationExclusion(t *testing.T) {
	used := windowUsage(0.90)
	tests := []struct {
		name    string
		options ModelOptions
		score   PaceScore
		want    bool
	}{
		{
			name:    "zero reserve adds no gate",
			options: ModelOptions{ReservedUserSharePct: 0},
			score:   PaceScore{Known: true, UsageFraction: 0.99},
		},
		{
			name:    "below the reserved threshold stays eligible",
			options: ModelOptions{ReservedUserSharePct: 20},
			score:   PaceScore{Known: true, UsageFraction: 0.79},
		},
		{
			name:    "at the reserved threshold is excluded",
			options: ModelOptions{ReservedUserSharePct: 20},
			score:   PaceScore{Known: true, UsageFraction: 0.80},
			want:    true,
		},
		{
			name:    "a full reserve excludes every known candidate",
			options: ModelOptions{ReservedUserSharePct: 100},
			score:   PaceScore{Known: true, UsageFraction: 0.0},
			want:    true,
		},
		{
			name:    "a positive reserve excludes unknown usage until evidence returns",
			options: ModelOptions{ReservedUserSharePct: 10},
			score:   PaceScore{Known: false},
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReservationExclusion(tt.options, tt.score); got != tt.want {
				t.Fatalf("excluded = %v, want %v", got, tt.want)
			}
		})
	}
	// A reservation is an admission gate, so it must not rewrite raw pace.
	reserved := ModelOptions{ReservedUserSharePct: 50}
	score := PaceFromWindows(paceTestNow(), []WindowObservation{{
		Label: "week", UsageFraction: used,
		StartAt: paceTestNow().Add(-time.Hour), ResetAt: paceTestNow().Add(6 * 24 * time.Hour),
	}})
	before := score.Pace
	ReservationExclusion(reserved, score)
	if score.Pace != before {
		t.Fatalf("pace changed from %v to %v, want the raw pace untouched", before, score.Pace)
	}
}

func TestRankTierExcludesReservedCandidate(t *testing.T) {
	tier := Tier{Index: 1, Policy: TierPolicy{Mode: TierModePace}, Candidates: []Candidate{
		{ID: "reserved", Selection: Selection{Model: ModelOptions{
			Cost: CostFree, UsageSource: UsageAutomatic, ReservedUserSharePct: 20}}},
		{ID: "open", Selection: Selection{Model: ModelOptions{Cost: CostFree}}},
	}}
	ranked := RankTier(tier, RankOptions{Now: paceTestNow(), Scores: map[string]PaceScore{
		"reserved": {Known: true, Pace: 0.1, UsageFraction: 0.9},
		"open":     {Known: true, Pace: 0.2, UsageFraction: 0.2},
	}})
	if ranked[0].IneligibleReason != IneligibleReserved {
		t.Fatalf("reason = %q, want %q", ranked[0].IneligibleReason, IneligibleReserved)
	}
	winner, ok := FirstEligible(ranked)
	if !ok || winner.Candidate.ID != "open" {
		t.Fatalf("winner = %#v, want the unreserved candidate", winner)
	}
}
