package sqlite

import (
	"context"
	"testing"
	"time"
)

// TestManualWindowUsageForProfilesSumsTheAccount pins account-scoped counting:
// a provider quota shared by several concrete profiles sums all of their
// recorded usage, each event once, and leaves profiles outside the list out.
func TestManualWindowUsageForProfilesSumsTheAccount(t *testing.T) {
	repo := newUsageEventsTestRepo(t)
	ctx := context.Background()
	createUsageEventsTestTask(t, repo, "task-account")
	createUsageEventsTestSession(t, repo, "session-account", "task-account")

	base := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	insertUsageTurn(t, repo, "turn-a", "session-account", "task-account", "zen-a", base)
	insertUsageTurn(t, repo, "turn-b", "session-account", "task-account", "zen-b", base)
	insertUsageTurn(t, repo, "turn-other", "session-account", "task-account", "openrouter-a", base)
	insertManualUsageEvent(t, repo, "evt-a", "task-account", "session-account", "turn-a", "dynamic-1",
		base.Add(time.Hour), 1000, 0, "actual", "complete")
	insertManualUsageEvent(t, repo, "evt-b", "task-account", "session-account", "turn-b", "dynamic-1",
		base.Add(2*time.Hour), 2000, 0, "actual", "complete")
	insertManualUsageEvent(t, repo, "evt-other", "task-account", "session-account", "turn-other", "dynamic-1",
		base.Add(3*time.Hour), 9999, 0, "actual", "complete")

	usage, err := repo.GetManualWindowUsageForProfiles(ctx, []string{"zen-a", "zen-b", "zen-a"}, base, base.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("GetManualWindowUsageForProfiles: %v", err)
	}
	if usage.EventCount != 2 || usage.TokensTotal != 3000 {
		t.Fatalf("usage = %#v, want both account profiles counted once each", usage)
	}

	if _, err := repo.GetManualWindowUsageForProfiles(ctx, []string{""}, base, base.Add(time.Hour)); err == nil {
		t.Fatal("an empty profile list must be rejected rather than matching everything")
	}
}
