package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
)

// TestDynamicManualWindowUsage pins the recorded-only manual accounting
// contract: the concrete profile comes from the stored turn, the interval is
// half-open, and unpriced or incomplete events keep the total visible as a
// lower bound instead of collapsing it to a wrong number.

func insertUsageTurn(t *testing.T, repo *Repository, turnID, sessionID, taskID, executionProfileID string, at time.Time) {
	t.Helper()
	if _, err := repo.db.Exec(repo.db.Rebind(`
		INSERT INTO task_session_turns
			(id, task_session_id, task_id, started_at, completed_at, execution_profile_id, route_generation, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, '{}', ?, ?)
	`), turnID, sessionID, taskID, at, at, executionProfileID, 1, at, at); err != nil {
		t.Fatalf("insert turn %s: %v", turnID, err)
	}
}

func insertManualUsageEvent(
	t *testing.T,
	repo *Repository,
	id, taskID, sessionID, turnID, agentProfileID string,
	at time.Time,
	tokens, costSubcents int64,
	costSource, completeness string,
) {
	t.Helper()
	event := newTestUsageEvent(id, taskID, sessionID)
	event.TurnID = turnID
	event.AgentProfileID = agentProfileID
	event.OccurredAt = at
	event.TokensTotal = tokens
	event.CostSubcents = costSubcents
	event.CostSource = costSource
	event.UsageCompleteness = completeness
	if err := repo.CreateTaskUsageEvent(context.Background(), event); err != nil {
		t.Fatalf("create usage event %s: %v", id, err)
	}
}

func TestDynamicManualWindowUsageAggregatesByStoredConcreteTurnProfile(t *testing.T) {
	repo := newUsageEventsTestRepo(t)
	ctx := context.Background()
	createUsageEventsTestTask(t, repo, "task-tier")
	createUsageEventsTestSession(t, repo, "session-tier", "task-tier")

	base := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	// Two logical sessions on the same concrete candidate must both count.
	insertUsageTurn(t, repo, "turn-1", "session-tier", "task-tier", "concrete-a", base)
	insertManualUsageEvent(t, repo, "evt-1", "task-tier", "session-tier", "turn-1", "dynamic-1",
		base.Add(10*time.Minute), 1000, 250, "actual", "complete")
	insertManualUsageEvent(t, repo, "evt-2", "task-tier", "session-tier", "turn-1", "dynamic-1",
		base.Add(20*time.Minute), 500, 100, "actual", "complete")

	// A sibling concrete candidate in the same task must not be counted.
	insertUsageTurn(t, repo, "turn-2", "session-tier", "task-tier", "concrete-b", base)
	insertManualUsageEvent(t, repo, "evt-3", "task-tier", "session-tier", "turn-2", "dynamic-1",
		base.Add(30*time.Minute), 9999, 9999, "actual", "complete")

	start := base
	end := base.Add(time.Hour)
	usage, err := repo.GetManualWindowUsage(ctx, "concrete-a", start, end)
	if err != nil {
		t.Fatalf("GetManualWindowUsage: %v", err)
	}
	if usage.EventCount != 2 || usage.TokensTotal != 1500 || usage.CostSubcents != 350 {
		t.Fatalf("usage = %#v, want only the two concrete-a events", usage)
	}
	if usage.TurnsAttributed != 2 || usage.ProfileAttributed != 0 {
		t.Fatalf("attribution = (turns %d, profile %d), want both via the turn",
			usage.TurnsAttributed, usage.ProfileAttributed)
	}
	if !usage.Complete() {
		t.Fatalf("complete = false, want fully priced and measured")
	}
}

func TestDynamicManualWindowUsageExcludesTheResetBoundary(t *testing.T) {
	repo := newUsageEventsTestRepo(t)
	ctx := context.Background()
	createUsageEventsTestTask(t, repo, "task-edge")
	createUsageEventsTestSession(t, repo, "session-edge", "task-edge")

	base := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	insertUsageTurn(t, repo, "turn-edge", "session-edge", "task-edge", "concrete-a", base)
	insertManualUsageEvent(t, repo, "evt-before", "task-edge", "session-edge", "turn-edge", "concrete-a",
		base, 100, 10, "actual", "complete")
	// Exactly at the reset instant: excluded, because the interval is half-open.
	insertManualUsageEvent(t, repo, "evt-at-reset", "task-edge", "session-edge", "turn-edge", "concrete-a",
		base.Add(time.Hour), 700, 70, "actual", "complete")

	usage, err := repo.GetManualWindowUsage(ctx, "concrete-a", base, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetManualWindowUsage: %v", err)
	}
	if usage.EventCount != 1 || usage.TokensTotal != 100 || usage.CostSubcents != 10 {
		t.Fatalf("usage = %#v, want the reset-instant event excluded", usage)
	}
}

func TestDynamicManualWindowUsageKeepsPartialAccountingVisible(t *testing.T) {
	repo := newUsageEventsTestRepo(t)
	ctx := context.Background()
	createUsageEventsTestTask(t, repo, "task-partial")
	createUsageEventsTestSession(t, repo, "session-partial", "task-partial")

	base := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	insertUsageTurn(t, repo, "turn-partial", "session-partial", "task-partial", "concrete-a", base)
	insertManualUsageEvent(t, repo, "evt-unpriced", "task-partial", "session-partial", "turn-partial", "dynamic-1",
		base, 100, 0, costSourceUnpriced, "complete")
	insertManualUsageEvent(t, repo, "evt-incomplete", "task-partial", "session-partial", "turn-partial", "dynamic-1",
		base, 100, 50, "actual", "tokens_only")

	usage, err := repo.GetManualWindowUsage(ctx, "concrete-a", base, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetManualWindowUsage: %v", err)
	}
	if usage.UnpricedCount != 1 || usage.IncompleteCount != 1 {
		t.Fatalf("usage = %#v, want both gaps counted", usage)
	}
	if usage.Complete() {
		t.Fatal("complete = true, want an incomplete recorded lower bound")
	}
	// The recorded total is still returned so a preview can show it.
	if usage.TokensTotal != 200 || usage.CostSubcents != 50 {
		t.Fatalf("usage = %#v, want the recorded lower bound retained", usage)
	}
}

func TestDynamicManualWindowUsageLeavesDeletedTurnsUnattributed(t *testing.T) {
	repo := newUsageEventsTestRepo(t)
	ctx := context.Background()
	createUsageEventsTestTask(t, repo, "task-orphan")
	createUsageEventsTestSession(t, repo, "session-orphan", "task-orphan")

	base := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	// A missing turn is not attributed through the turn join. The event's own
	// profile is the logical dynamic profile, which must never be used as a
	// concrete candidate.
	insertManualUsageEvent(t, repo, "evt-orphan", "task-orphan", "session-orphan", "turn-deleted", "dynamic-1",
		base, 5000, 5000, "actual", "complete")

	usage, err := repo.GetManualWindowUsage(ctx, "concrete-a", base, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetManualWindowUsage: %v", err)
	}
	if usage.EventCount != 0 || usage.TokensTotal != 0 {
		t.Fatalf("usage = %#v, want an unattributed event to stay out of a concrete total", usage)
	}
	// The same event must not be reachable by treating the dynamic profile as
	// the concrete candidate either.
	asDynamic, err := repo.GetManualWindowUsage(ctx, "dynamic-1", base, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetManualWindowUsage for dynamic: %v", err)
	}
	if asDynamic.TokensTotal != 5000 {
		t.Fatalf("dynamic total = %d, want the proven-event-profile fallback to apply", asDynamic.TokensTotal)
	}
}

func TestDynamicManualWindowUsageEmptyQueryIsRecordedZeroNotProof(t *testing.T) {
	repo := newUsageEventsTestRepo(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	usage, err := repo.GetManualWindowUsage(ctx, "concrete-a", base, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetManualWindowUsage: %v", err)
	}
	if usage.EventCount != 0 || usage.TokensTotal != 0 || usage.CostSubcents != 0 {
		t.Fatalf("usage = %#v, want a zero recorded total", usage)
	}
	if !usage.Complete() {
		t.Fatal("complete = false, want a clean empty window")
	}
}

func TestDynamicManualWindowUsageRejectsDegenerateInput(t *testing.T) {
	repo := newUsageEventsTestRepo(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	if _, err := repo.GetManualWindowUsage(ctx, "", base, base.Add(time.Hour)); err == nil {
		t.Fatal("empty profile: want an error rather than a whole-ledger aggregate")
	}
	usage, err := repo.GetManualWindowUsage(ctx, "concrete-a", base, base)
	if err != nil {
		t.Fatalf("empty interval: %v", err)
	}
	if usage.EventCount != 0 {
		t.Fatalf("usage = %#v, want zero for a zero-length interval", usage)
	}
}

var _ = models.TaskUsageTotals{}
