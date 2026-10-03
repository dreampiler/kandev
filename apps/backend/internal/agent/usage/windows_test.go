package usage

import (
	"testing"
	"time"
)

func mustAnchor(t *testing.T, raw string) ResetAnchor {
	t.Helper()
	anchor, err := ParseResetAnchor(raw)
	if err != nil {
		t.Fatalf("ParseResetAnchor(%q): %v", raw, err)
	}
	return anchor
}

func TestParseResetAnchorRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "missing time", raw: "mon"},
		{name: "hour out of range", raw: "24:00"},
		{name: "minute out of range", raw: "09:60"},
		{name: "unparsable time", raw: "nine"},
		{name: "day without a value", raw: "09:00 day"},
		{name: "day out of range", raw: "09:00 day 0"},
		{name: "phase out of range", raw: "09:00 phase 99"},
		{name: "unknown field", raw: "09:00 fortnight"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseResetAnchor(tt.raw); err == nil {
				t.Fatalf("ParseResetAnchor(%q) succeeded, want an error", tt.raw)
			}
		})
	}
}

func TestParseResetAnchorAcceptsDocumentedForms(t *testing.T) {
	timeOnly := mustAnchor(t, "09:00")
	if timeOnly.Hour != 9 || timeOnly.Minute != 0 || timeOnly.HasWeekday {
		t.Fatalf("anchor = %#v", timeOnly)
	}
	weekly := mustAnchor(t, "09:00 mon day 3 phase 2")
	if !weekly.HasWeekday || weekly.Weekday != time.Monday ||
		weekly.DayOfMonth != 3 || weekly.PhaseHours != 2 {
		t.Fatalf("anchor = %#v", weekly)
	}
}

func TestResolveFiveHourWindowIsFixedLengthAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("timezone unavailable: %v", err)
	}
	anchor := mustAnchor(t, "09:00")
	// A spring-forward day: 2026-03-08 02:00 local does not exist.
	now := time.Date(2026, 3, 8, 14, 0, 0, 0, loc)
	window, ok := ResolveResetWindow(now, WindowPeriodFiveHour, anchor, loc)
	if !ok {
		t.Fatal("window not resolved")
	}
	if window.Reset.Sub(window.Start) != fiveHourDuration {
		t.Fatalf("window length = %v, want exactly five hours", window.Reset.Sub(window.Start))
	}
	if !window.Reset.After(now) {
		t.Fatalf("reset = %v, want it after now", window.Reset)
	}
	if window.Start.After(now) {
		t.Fatalf("start = %v, want the containing window", window.Start)
	}
}

func TestResolveFiveHourWindowPhaseOffsetsTheBlockBoundary(t *testing.T) {
	loc := time.UTC
	plain := mustAnchor(t, "00:00")
	now := time.Date(2026, 10, 2, 7, 30, 0, 0, loc)
	plainWindow, _ := ResolveResetWindow(now, WindowPeriodFiveHour, plain, loc)
	if plainWindow.Reset.Hour() != 10 {
		t.Fatalf("reset hour = %d, want 10 for a midnight-anchored window", plainWindow.Reset.Hour())
	}
	// The phase shifts the whole grid, so it must move the boundary rather than
	// being ignored: a two-hour phase moves the midnight grid to a 02:00 grid,
	// whose containing block for 07:30 ends at 12:00.
	shifted := mustAnchor(t, "00:00 phase 2")
	shiftedWindow, ok := ResolveResetWindow(now, WindowPeriodFiveHour, shifted, loc)
	if !ok {
		t.Fatal("shifted window not resolved")
	}
	if shiftedWindow.Reset.Hour() != 12 {
		t.Fatalf("reset hour = %d, want the phase-shifted grid boundary", shiftedWindow.Reset.Hour())
	}
	if shiftedWindow.Reset.Sub(shiftedWindow.Start) != fiveHourDuration {
		t.Fatalf("shifted window length = %v, want exactly five hours", shiftedWindow.Reset.Sub(shiftedWindow.Start))
	}
	if !shiftedWindow.Start.Before(now) || !shiftedWindow.Reset.After(now) {
		t.Fatalf("shifted window = %#v, want it to contain now", shiftedWindow)
	}
}

func TestResolveDayWindowUsesCalendarBoundaries(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Skipf("timezone unavailable: %v", err)
	}
	anchor := mustAnchor(t, "09:00")
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, loc)
	window, ok := ResolveResetWindow(now, WindowPeriodDay, anchor, loc)
	if !ok {
		t.Fatal("window not resolved")
	}
	if window.Reset.Hour() != 9 || window.Reset.Day() != 3 {
		t.Fatalf("reset = %v, want 09:00 on the next day", window.Reset)
	}
	if window.Start.Hour() != 9 || window.Start.Day() != 2 {
		t.Fatalf("start = %v, want 09:00 on the current day", window.Start)
	}
	// Exactly at the reset instant is already inside the next window.
	atReset := window.Reset
	next, ok := ResolveResetWindow(atReset, WindowPeriodDay, anchor, loc)
	if !ok || !next.Start.Equal(atReset) {
		t.Fatalf("window at reset = %#v, want a fresh window starting at the reset", next)
	}
}

func TestResolveWeeklyWindowAnchorsOnWeekday(t *testing.T) {
	loc := time.UTC
	// 2026-10-02 is a Friday.
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, loc)
	anchor := mustAnchor(t, "00:00 mon")
	window, ok := ResolveResetWindow(now, WindowPeriodWeek, anchor, loc)
	if !ok {
		t.Fatal("window not resolved")
	}
	if window.Reset.Weekday() != time.Monday || window.Start.Weekday() != time.Monday {
		t.Fatalf("window = %#v, want Monday boundaries", window)
	}
	if window.Start.Day() != 28 || window.Reset.Day() != 5 {
		t.Fatalf("window = %v..%v, want 28 Sep to 5 Oct", window.Start, window.Reset)
	}
}

func TestResolveMonthWindowUsesCalendarMonthAndClampsDay(t *testing.T) {
	loc := time.UTC
	// A day-31 anchor must clamp to the last valid day of each month.
	anchor := mustAnchor(t, "00:00 day 31")
	now := time.Date(2026, 2, 10, 12, 0, 0, 0, loc)
	window, ok := ResolveResetWindow(now, WindowPeriodMonth, anchor, loc)
	if !ok {
		t.Fatal("window not resolved")
	}
	if window.Start.Month() != time.January || window.Start.Day() != 31 {
		t.Fatalf("start = %v, want 31 January", window.Start)
	}
	if window.Reset.Month() != time.February || window.Reset.Day() != 28 {
		t.Fatalf("reset = %v, want 28 February for a non-leap year", window.Reset)
	}
	if window.Reset.Sub(window.Start) >= 29*24*time.Hour {
		t.Fatalf("window = %v, want a calendar month rather than a fixed 30 days", window.Reset.Sub(window.Start))
	}
}

func TestResolveMonthWindowClampsToLeapDay(t *testing.T) {
	loc := time.UTC
	anchor := mustAnchor(t, "00:00 day 31")
	now := time.Date(2028, 2, 10, 12, 0, 0, 0, loc)
	window, ok := ResolveResetWindow(now, WindowPeriodMonth, anchor, loc)
	if !ok {
		t.Fatal("window not resolved")
	}
	if window.Reset.Day() != 29 {
		t.Fatalf("reset = %v, want 29 February in a leap year", window.Reset)
	}
}

func TestResolveMonthWindowHandlesDay31AnchoredInA31DayMonth(t *testing.T) {
	loc := time.UTC
	// A day-31 anchor resets on the 31st, or on a shorter month's last day. The
	// window is [previous anchor, next anchor), so a November window starts on
	// 30 November and runs to 31 December.
	anchor := mustAnchor(t, "00:00 day 31")
	now := time.Date(2026, 12, 15, 12, 0, 0, 0, loc)
	window, ok := ResolveResetWindow(now, WindowPeriodMonth, anchor, loc)
	if !ok {
		t.Fatal("window not resolved")
	}
	if window.Reset.Day() != 31 || window.Reset.Month() != time.December {
		t.Fatalf("reset = %v, want 31 December", window.Reset)
	}
	if window.Start.Month() != time.November || window.Start.Day() != 30 {
		t.Fatalf("start = %v, want 30 November (clamped from 31)", window.Start)
	}
}

func TestResolveResetWindowRejectsInvalidInput(t *testing.T) {
	loc := time.UTC
	anchor := mustAnchor(t, "09:00")
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, loc)
	if _, ok := ResolveResetWindow(now, "fortnight", anchor, loc); ok {
		t.Fatal("unknown period resolved, want rejection")
	}
	if _, ok := ResolveResetWindow(now, WindowPeriodDay, anchor, nil); ok {
		t.Fatal("nil location resolved, want rejection")
	}
}

func TestResolveResetWindowSurvivesDSTTransitionDay(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("timezone unavailable: %v", err)
	}
	// 2026-11-01 is the US fall-back day.
	anchor := mustAnchor(t, "00:00")
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, loc)
	window, ok := ResolveResetWindow(now, WindowPeriodDay, anchor, loc)
	if !ok {
		t.Fatal("window not resolved")
	}
	if !window.Reset.After(now) || !window.Start.Before(now) {
		t.Fatalf("window = %#v, want the containing local day", window)
	}
	if window.Reset.Sub(window.Start) < 23*time.Hour || window.Reset.Sub(window.Start) > 25*time.Hour {
		t.Fatalf("window length = %v, want roughly a local day", window.Reset.Sub(window.Start))
	}
}
