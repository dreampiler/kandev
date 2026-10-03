package dynamic

import (
	"testing"
	"time"
)

func TestExhaustedWindowWithoutSpanIsKnownBusiest(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	full := 1.0
	score := PaceFromWindows(now, []WindowObservation{{Label: "monthly", UsageFraction: &full, Exhausted: true}})
	if !score.Known || !score.FloorApplied || score.Pace != 1/minPaceElapsed {
		t.Fatalf("score = %#v, want a known pace at the elapsed floor", score)
	}

	half := 0.5
	busy := PaceFromWindows(now, []WindowObservation{{
		Label: "7-day", UsageFraction: &half, StartAt: now.Add(-time.Hour), ResetAt: now.Add(time.Hour),
	}})
	if !(busy.Pace < score.Pace) {
		t.Fatalf("live window pace %v must rank ahead of an exhausted account %v", busy.Pace, score.Pace)
	}
}

func TestUnexhaustedWindowWithoutSpanStaysUnknown(t *testing.T) {
	now := time.Now()
	partial := 0.4
	if score := PaceFromWindows(now, []WindowObservation{{Label: "monthly", UsageFraction: &partial}}); score.Known {
		t.Fatalf("score = %#v, want unknown without a span", score)
	}
}

func TestExpiredExhaustedWindowIsNotBusy(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	full := 1.0
	score := PaceFromWindows(now, []WindowObservation{{
		Label: "7-day", UsageFraction: &full, Exhausted: true,
		StartAt: now.Add(-8 * 24 * time.Hour), ResetAt: now.Add(-time.Hour),
	}})
	if score.Known {
		t.Fatalf("score = %#v, want a window past its reset to need fresh evidence", score)
	}
}
