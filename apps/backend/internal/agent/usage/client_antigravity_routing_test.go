package usage

import (
	"testing"
	"time"
)

func TestAntigravityWindowsRetainNumericDuration(t *testing.T) {
	resetAt := time.Date(2026, 10, 5, 5, 0, 0, 0, time.UTC)
	body := []byte(`{
		"status": "SUCCESS",
		"command": {"data": {"groups": [{"name": "Gemini Models", "buckets": [
			{"id": "g5h", "window": "5h", "remaining_fraction": 0.57,
			 "reset_time": "` + resetAt.Format(time.RFC3339) + `"},
			{"id": "gw", "window": "weekly", "remaining_fraction": 0.39,
			 "reset_time": "` + resetAt.AddDate(0, 0, 3).Format(time.RFC3339) + `"}
		]}]}}
	}`)
	usage, err := parseAntigravityUsage(body, resetAt.Add(-time.Hour))
	if err != nil {
		t.Fatalf("parseAntigravityUsage: %v", err)
	}
	if len(usage.Windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(usage.Windows))
	}
	fiveHour := usage.Windows[0]
	if fiveHour.Label != claudeLabel5Hour {
		t.Fatalf("label = %q, want %q", fiveHour.Label, claudeLabel5Hour)
	}
	if fiveHour.DurationSeconds != 18000 {
		t.Fatalf("duration = %d, want 18000", fiveHour.DurationSeconds)
	}
	if !fiveHour.StartAt.Equal(resetAt.Add(-5 * time.Hour)) {
		t.Fatalf("start = %v, want reset minus five hours", fiveHour.StartAt)
	}
	if !fiveHour.UsableFor("") {
		t.Fatal("five-hour window unusable, want a usable numeric window")
	}
	weekly := usage.Windows[1]
	if weekly.DurationSeconds != 604800 {
		t.Fatalf("weekly duration = %d, want 604800", weekly.DurationSeconds)
	}
	if !weekly.UsableFor("") {
		t.Fatal("weekly window unusable, want a usable numeric window")
	}
}

func TestAntigravityUnknownWindowStaysUnusable(t *testing.T) {
	resetAt := time.Date(2026, 10, 5, 5, 0, 0, 0, time.UTC)
	body := []byte(`{
		"status": "SUCCESS",
		"command": {"data": {"groups": [{"name": "Gemini Models", "buckets": [
			{"id": "gmystery", "window": "opaque", "remaining_fraction": 0.5,
			 "reset_time": "` + resetAt.Format(time.RFC3339) + `"}
		]}]}}
	}`)
	usage, err := parseAntigravityUsage(body, resetAt.Add(-time.Hour))
	if err != nil {
		t.Fatalf("parseAntigravityUsage: %v", err)
	}
	if len(usage.Windows) != 1 {
		t.Fatalf("windows = %d, want 1", len(usage.Windows))
	}
	if usage.Windows[0].Label != "opaque" {
		t.Fatalf("label = %q, want the reported window name", usage.Windows[0].Label)
	}
	if usage.Windows[0].UsableFor("") {
		t.Fatal("unknown window usable without a known length, want unknown rather than a guess")
	}
}
