package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/testutil"
)

// GetManualWindowUsage is dialect-sensitive: it joins task_usage_events to
// task_session_turns through the event's turn_id, compares occurred_at against a
// half-open interval, and uses CASE aggregates over possibly-NULL columns. The
// SQLite-only cases in dynamic_manual_usage_test.go cannot exercise the
// PostgreSQL branch at all.
//
// Skips unless KANDEV_TEST_POSTGRES_DSN is set.

// TestPostgresGetManualWindowUsage_JoinsTurnProfileAndExcludesBoundary is the
// Postgres counterpart to the SQLite concrete-attribution and reset-boundary
// cases: it proves the LEFT JOIN, the interval comparison and the CASE
// aggregates all work against a real PostgreSQL instance.
//
// A skip here is not parity evidence; the test must actually run.
func TestPostgresGetManualWindowUsage_JoinsTurnProfileAndExcludesBoundary(t *testing.T) {
	db := testutil.OpenIsolatedPostgres(t, testutil.PostgresDSNFromEnv(t))
	repo, err := NewWithDB(db, db, nil)
	if err != nil {
		t.Fatalf("init postgres schema: %v", err)
	}
	seedPostgresTaskSession(t, repo, "task-window-pg", "session-window-pg")
	ctx := context.Background()

	base := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	seedManualTurnPostgres(t, repo, "turn-a", "session-window-pg", "task-window-pg", "concrete-a", base)
	seedManualTurnPostgres(t, repo, "turn-b", "session-window-pg", "task-window-pg", "concrete-b", base)

	inside := manualUsageEventPostgres("evt-in", "task-window-pg", "session-window-pg", "turn-a",
		"dynamic-1", base.Add(10*time.Minute))
	if err := repo.CreateTaskUsageEvent(ctx, inside); err != nil {
		t.Fatalf("CreateTaskUsageEvent(inside): %v", err)
	}
	// Exactly at the reset instant: the interval is half-open, so it is excluded.
	atReset := manualUsageEventPostgres("evt-reset", "task-window-pg", "session-window-pg", "turn-a",
		"dynamic-1", base.Add(time.Hour))
	if err := repo.CreateTaskUsageEvent(ctx, atReset); err != nil {
		t.Fatalf("CreateTaskUsageEvent(atReset): %v", err)
	}
	// A sibling concrete candidate must not be attributed to concrete-a.
	sibling := manualUsageEventPostgres("evt-sibling", "task-window-pg", "session-window-pg", "turn-b",
		"dynamic-1", base.Add(20*time.Minute))
	if err := repo.CreateTaskUsageEvent(ctx, sibling); err != nil {
		t.Fatalf("CreateTaskUsageEvent(sibling): %v", err)
	}

	usage, err := repo.GetManualWindowUsage(ctx, "concrete-a", base, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetManualWindowUsage: %v", err)
	}
	// The expected figures come from the seeded event rather than a literal, so
	// a fixture change cannot leave this case asserting a stale number.
	wantTokens := inside.TokensTotal
	wantCost := inside.CostSubcents
	if usage.EventCount != 1 || usage.TokensTotal != wantTokens || usage.CostSubcents != wantCost {
		t.Fatalf("usage = %#v, want only the in-window concrete-a event (%d tokens, %d subcents)",
			usage, wantTokens, wantCost)
	}
	if usage.TurnsAttributed != 1 || usage.ProfileAttributed != 0 {
		t.Fatalf("attribution = (turns %d, profile %d), want both via the turn",
			usage.TurnsAttributed, usage.ProfileAttributed)
	}
}

// TestPostgresGetManualWindowUsage_CountsUnpricedAndIncomplete proves the CASE
// aggregates report a partial total's gaps rather than collapsing it.
func TestPostgresGetManualWindowUsage_CountsUnpricedAndIncomplete(t *testing.T) {
	db := testutil.OpenIsolatedPostgres(t, testutil.PostgresDSNFromEnv(t))
	repo, err := NewWithDB(db, db, nil)
	if err != nil {
		t.Fatalf("init postgres schema: %v", err)
	}
	seedPostgresTaskSession(t, repo, "task-partial-pg", "session-partial-pg")
	ctx := context.Background()

	base := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	seedManualTurnPostgres(t, repo, "turn-partial-pg", "session-partial-pg", "task-partial-pg", "concrete-a", base)

	unpriced := manualUsageEventPostgres("evt-unpriced-pg", "task-partial-pg", "session-partial-pg",
		"turn-partial-pg", "dynamic-1", base.Add(5*time.Minute))
	unpriced.CostSource = costSourceUnpriced
	if err := repo.CreateTaskUsageEvent(ctx, unpriced); err != nil {
		t.Fatalf("CreateTaskUsageEvent(unpriced): %v", err)
	}
	incomplete := manualUsageEventPostgres("evt-incomplete-pg", "task-partial-pg", "session-partial-pg",
		"turn-partial-pg", "dynamic-1", base.Add(6*time.Minute))
	incomplete.UsageCompleteness = "tokens_only"
	if err := repo.CreateTaskUsageEvent(ctx, incomplete); err != nil {
		t.Fatalf("CreateTaskUsageEvent(incomplete): %v", err)
	}

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
}

func seedManualTurnPostgres(
	t *testing.T,
	repo *Repository,
	turnID, sessionID, taskID, executionProfileID string,
	at time.Time,
) {
	t.Helper()
	if _, err := repo.db.Exec(repo.db.Rebind(`
		INSERT INTO task_session_turns
			(id, task_session_id, task_id, started_at, completed_at, execution_profile_id,
			 route_generation, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, '{}', ?, ?)
	`), turnID, sessionID, taskID, at, at, executionProfileID, 1, at, at); err != nil {
		t.Fatalf("seed turn %s: %v", turnID, err)
	}
}

func manualUsageEventPostgres(
	id, taskID, sessionID, turnID, agentProfileID string,
	at time.Time,
) *models.TaskUsageEvent {
	event := newTestUsageEvent(id, taskID, sessionID)
	event.TurnID = turnID
	event.AgentProfileID = agentProfileID
	event.OccurredAt = at
	event.CreatedAt = at
	event.UsageCompleteness = "complete"
	return event
}
