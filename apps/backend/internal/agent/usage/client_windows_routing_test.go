package usage

import (
	"strconv"
	"testing"
	"time"
)

func TestCodexWindowsRetainNumericDuration(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	resetAt := now.Add(time.Hour)
	body := []byte(`{
		"plan_type": "plus",
		"rate_limit": {
			"primary_window": {"used_percent": 25, "limit_window_seconds": 18000,
				"reset_at": ` + itoa(resetAt.Unix()) + `, "reset_after_seconds": 3600},
			"secondary_window": {"used_percent": 40, "limit_window_seconds": 604800,
				"reset_at": ` + itoa(resetAt.AddDate(0, 0, 6).Unix()) + `}
		}
	}`)
	usage, err := parseCodexUsage(body, now)
	if err != nil {
		t.Fatalf("parseCodexUsage: %v", err)
	}
	if len(usage.Windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(usage.Windows))
	}
	primary := usage.Windows[0]
	if primary.DurationSeconds != 18000 {
		t.Fatalf("duration = %d, want the provider's numeric 18000", primary.DurationSeconds)
	}
	if !primary.StartAt.Equal(time.Unix(resetAt.Unix(), 0).Add(-5 * time.Hour)) {
		t.Fatalf("start = %v, want reset minus five hours", primary.StartAt)
	}
	if !primary.UsableFor("") {
		t.Fatal("primary window unusable, want a usable numeric window")
	}
	if usage.Windows[1].DurationSeconds != 604800 {
		t.Fatalf("secondary duration = %d, want 604800", usage.Windows[1].DurationSeconds)
	}
}

func TestCodexWindowWithoutDurationIsUnusable(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	usage, err := parseCodexUsage([]byte(`{
		"rate_limit": {"primary_window": {"used_percent": 10, "reset_after_seconds": 600}}
	}`), now)
	if err != nil {
		t.Fatalf("parseCodexUsage: %v", err)
	}
	if len(usage.Windows) != 1 {
		t.Fatalf("windows = %d, want 1", len(usage.Windows))
	}
	// The label says "current", but a label is not evidence of a length.
	if usage.Windows[0].UsableFor("") {
		t.Fatal("window usable without a duration, want unknown rather than a guess")
	}
}

func TestClaudeScopedLimitIsUnusableWithoutAModelIdentity(t *testing.T) {
	reset := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	windows := claudeLimitWindows([]claudeLimit{{
		Kind:     claudeLimitWeeklyScoped,
		Percent:  30,
		ResetsAt: reset.Format(time.RFC3339),
		Scope: &claudeLimitScope{Model: &struct {
			DisplayName string `json:"display_name"`
		}{DisplayName: "Claude Sonnet"}},
	}})
	if len(windows) != 1 {
		t.Fatalf("windows = %d, want 1", len(windows))
	}
	if !windows[0].AmbiguousModelScope {
		t.Fatal("scoped window is not marked ambiguous, want a display name treated as non-identity")
	}
	if windows[0].UsableFor("claude-sonnet-5") {
		t.Fatal("scoped window usable, want unknown rather than matched by display name")
	}
}

func TestClaudeKnownKindsCarryNumericDuration(t *testing.T) {
	reset := time.Date(2026, 10, 2, 17, 0, 0, 0, time.UTC)
	windows := claudeLimitWindows([]claudeLimit{
		{Kind: "session", Percent: 25, ResetsAt: reset.Format(time.RFC3339)},
		{Kind: "weekly_all", Percent: 40, ResetsAt: reset.Format(time.RFC3339)},
		{Kind: "unknown_future_kind", Percent: 10, ResetsAt: reset.Format(time.RFC3339)},
	})
	if len(windows) != 2 {
		t.Fatalf("windows = %d, want 2 (the unknown kind has no duration)", len(windows))
	}
	if windows[0].DurationSeconds != 5*3600 {
		t.Fatalf("session duration = %d, want 18000", windows[0].DurationSeconds)
	}
	if windows[1].DurationSeconds != 7*24*3600 {
		t.Fatalf("weekly duration = %d, want 604800", windows[1].DurationSeconds)
	}
	for index, window := range windows {
		if !window.StartAt.Before(window.ResetAt) {
			t.Fatalf("window %d start %v is not before reset %v", index, window.StartAt, window.ResetAt)
		}
		if !window.UsableFor("") {
			t.Fatalf("window %d unusable, want a usable known window", index)
		}
	}
}

func TestClaudeFallbackWindowsCarryNumericDuration(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	reset := now.Add(2 * time.Hour)
	windows := claudeWindows(claudeUsageResponse{
		FiveHour: &claudeUsageWindow{Utilization: 25, ResetsAt: reset.Format(time.RFC3339)},
	}, now)
	if len(windows) != 1 {
		t.Fatalf("windows = %d, want 1", len(windows))
	}
	if windows[0].DurationSeconds != 5*3600 {
		t.Fatalf("duration = %d, want 18000", windows[0].DurationSeconds)
	}
	if !windows[0].StartAt.Equal(reset.Add(-5 * time.Hour)) {
		t.Fatalf("start = %v, want reset minus five hours", windows[0].StartAt)
	}
}

func TestUtilizationWindowUsableForRejectsUnidentifiedScopeAndMismatch(t *testing.T) {
	reset := time.Date(2026, 10, 2, 17, 0, 0, 0, time.UTC)
	base := UtilizationWindow{
		DurationSeconds: 18000, ResetAt: reset, StartAt: reset.Add(-5 * time.Hour),
	}
	if !base.UsableFor("any-model") {
		t.Fatal("account-wide window rejected, want it usable for any model")
	}
	scoped := base
	scoped.ModelID = "claude-opus-5"
	if scoped.UsableFor("claude-sonnet-5") {
		t.Fatal("mismatched model accepted, want the window to not apply")
	}
	if !scoped.UsableFor("claude-opus-5") {
		t.Fatal("matching model rejected")
	}
	ambiguous := base
	ambiguous.AmbiguousModelScope = true
	if ambiguous.UsableFor("claude-sonnet-5") {
		t.Fatal("unidentified scope accepted, want it unusable")
	}
}

// TestUtilizationWindowModelScopeNeedsBothIdentities pins AC-003.3's
// attribution rule. A model-scoped window is that model's consumption, so it is
// only usable for that exact model, and it stays unusable when the candidate's
// own model is unknown. Accepting it because "we cannot tell" is exactly the
// substitution of another model's usage that the unknown state prevents.
func TestUtilizationWindowModelScopeNeedsBothIdentities(t *testing.T) {
	reset := time.Date(2026, 10, 2, 17, 0, 0, 0, time.UTC)
	scoped := UtilizationWindow{
		DurationSeconds: 18000, ResetAt: reset, StartAt: reset.Add(-5 * time.Hour),
		ModelID: "claude-opus-5",
	}
	if scoped.UsableFor("") {
		t.Fatal("a model-scoped window was accepted for a candidate with no known model")
	}
	if !scoped.UsableFor("claude-opus-5") {
		t.Fatal("a model-scoped window was rejected for its own model")
	}
	if scoped.UsableFor("claude-sonnet-5") {
		t.Fatal("a sibling model's window was accepted, want it excluded")
	}
	// An account-wide window still applies whatever model the candidate runs.
	accountWide := scoped
	accountWide.ModelID = ""
	if !accountWide.UsableFor("") || !accountWide.UsableFor("claude-sonnet-5") {
		t.Fatal("an account-wide window must remain usable for any model")
	}
}

func itoa(value int64) string { return strconv.FormatInt(value, 10) }
