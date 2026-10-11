package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"

	settingsstore "github.com/kandev/kandev/internal/agent/settings/store"
	"github.com/kandev/kandev/internal/office/repository/sqlite"
	runsmodels "github.com/kandev/kandev/internal/runs/models"
)

// openSessionCeilingParkDB opens a single-connection in-memory database so
// a second NewWithDB call replays the migrations against the same schema
// and rows, exactly as a restart would.
func openSessionCeilingParkDB(t *testing.T) (*sqlite.Repository, *sqlx.DB) {
	t.Helper()
	db, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, _, err := settingsstore.Provide(db, db, nil); err != nil {
		t.Fatalf("settings store init: %v", err)
	}
	repo, err := sqlite.NewWithDB(db, db, nil)
	if err != nil {
		t.Fatalf("first init: %v", err)
	}
	return repo, db
}

// seedParkedRun creates a queued run, appends the given run events in
// order, then parks it under blocked_provider_action_required with no retry
// deadline — the state a pre-waiting_for_session_capacity build left behind
// after the orchestrator's session ceiling deferred the launch.
func seedParkedRun(
	t *testing.T,
	repo *sqlite.Repository,
	agentProfileID, runID string,
	events [][2]string,
) {
	t.Helper()
	ctx := context.Background()
	run := &runsmodels.Run{
		ID:             runID,
		AgentProfileID: agentProfileID,
		Reason:         "task_assigned",
		Payload:        `{}`,
		Status:         "queued",
		CoalescedCount: 1,
	}
	if err := repo.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run %s: %v", runID, err)
	}
	for _, event := range events {
		if _, err := repo.AppendRunEvent(ctx, runID, event[0], "info", event[1]); err != nil {
			t.Fatalf("append event for %s: %v", runID, err)
		}
	}
	if err := repo.ParkRunForProviderCapacity(ctx, runID,
		string(runsmodels.RoutingBlockedActionRequired), time.Time{}); err != nil {
		t.Fatalf("park %s: %v", runID, err)
	}
}

// TestBackfillSessionCeilingParkedRuns_MigratesLegacyDeferredRunToTimedLift
// reproduces a run parked under blocked_provider_action_required by a build
// from before the waiting_for_session_capacity status existed, and asserts
// the boot-time backfill moves it onto that status with a retry deadline so
// the scheduler's timed lift returns it to dispatch.
func TestBackfillSessionCeilingParkedRuns_MigratesLegacyDeferredRunToTimedLift(t *testing.T) {
	repo, db := openSessionCeilingParkDB(t)
	ctx := context.Background()

	seedAgentProfile(t, db, "agent-ceiling", "ws-ceiling")
	seedParkedRun(t, repo, "agent-ceiling", "run-legacy-deferred", [][2]string{
		{"adapter.invoke", `{"agent":"cto","task_id":"t1","executor_type":"local_pc"}`},
		{"adapter.invoke", `{"phase":"deferred","reason":"session_ceiling"}`},
	})

	before, err := repo.GetRunByID(ctx, "run-legacy-deferred")
	if err != nil {
		t.Fatalf("read pre-backfill run: %v", err)
	}
	if before.EarliestRetryAt != nil {
		t.Fatalf("pre-backfill earliest_retry_at = %v, want NULL", before.EarliestRetryAt)
	}

	// Replay migrations on the same DB handle, as a restart would.
	if _, err := sqlite.NewWithDB(db, db, nil); err != nil {
		t.Fatalf("replay migrations: %v", err)
	}

	got, err := repo.GetRunByID(ctx, "run-legacy-deferred")
	if err != nil {
		t.Fatalf("read post-backfill run: %v", err)
	}
	if got.RoutingBlockedStatus == nil ||
		*got.RoutingBlockedStatus != runsmodels.RoutingBlockedWaitingForSessionCapacity {
		t.Fatalf("routing_blocked_status = %v, want waiting_for_session_capacity", got.RoutingBlockedStatus)
	}
	if got.Status != "queued" {
		t.Fatalf("status = %q, want queued", got.Status)
	}
	if got.EarliestRetryAt == nil {
		t.Fatal("earliest_retry_at = NULL, want the retry deadline the new park status carries")
	}
	deadline := *got.EarliestRetryAt
	now := time.Now().UTC()
	if deadline.Before(now.Add(10*time.Second)) || deadline.After(now.Add(40*time.Second)) {
		t.Fatalf("earliest_retry_at = %v, want about 20s after boot (%v)", deadline, now)
	}
	if got.ScheduledRetryAt == nil || !got.ScheduledRetryAt.Equal(deadline) {
		t.Fatalf("scheduled_retry_at = %v, want %v", got.ScheduledRetryAt, deadline)
	}

	// The recovered goal: the run is on the timed-lift path once its
	// deadline passes, and not before it.
	notYetPending, err := repo.ListPendingProviderCapacityRuns(ctx, now)
	if err != nil {
		t.Fatalf("list pending before deadline: %v", err)
	}
	for _, candidate := range notYetPending {
		if candidate.ID == got.ID {
			t.Fatal("run listed as timed pending before its deadline passed")
		}
	}
	afterDeadline, err := repo.ListPendingProviderCapacityRuns(ctx, deadline.Add(time.Second))
	if err != nil {
		t.Fatalf("list pending after deadline: %v", err)
	}
	found := false
	for _, candidate := range afterDeadline {
		if candidate.ID == got.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("recovered run is not on the timed-lift list after its deadline")
	}

	// A second replay must not re-stamp the deadline: the migrated row no
	// longer matches the candidate filter.
	if _, err := sqlite.NewWithDB(db, db, nil); err != nil {
		t.Fatalf("second replay: %v", err)
	}
	replayed, err := repo.GetRunByID(ctx, "run-legacy-deferred")
	if err != nil {
		t.Fatalf("read after second replay: %v", err)
	}
	if replayed.EarliestRetryAt == nil || !replayed.EarliestRetryAt.Equal(deadline) {
		t.Fatalf("earliest_retry_at after replay = %v, want unchanged %v", replayed.EarliestRetryAt, deadline)
	}
}

// TestBackfillSessionCeilingParkedRuns_LeavesNonDeferredRuns pins the
// discriminator: a run parked under blocked_provider_action_required whose
// latest event is not the session-ceiling deferral marker — a real launch
// event, no events at all, or anything that happened after the deferral —
// stays parked for an operator and is never given an automatic deadline.
func TestBackfillSessionCeilingParkedRuns_LeavesNonDeferredRuns(t *testing.T) {
	repo, db := openSessionCeilingParkDB(t)
	ctx := context.Background()

	seedAgentProfile(t, db, "agent-action", "ws-action")
	seedParkedRun(t, repo, "agent-action", "run-launched", [][2]string{
		{"adapter.invoke", `{"agent":"cto","task_id":"t2","executor_type":"local_pc"}`},
	})
	seedParkedRun(t, repo, "agent-action", "run-no-events", nil)
	seedParkedRun(t, repo, "agent-action", "run-events-after-deferral", [][2]string{
		{"adapter.invoke", `{"phase":"deferred","reason":"session_ceiling"}`},
		{"complete", `{"summary":"done"}`},
	})

	if _, err := sqlite.NewWithDB(db, db, nil); err != nil {
		t.Fatalf("replay migrations: %v", err)
	}

	for _, runID := range []string{"run-launched", "run-no-events", "run-events-after-deferral"} {
		got, err := repo.GetRunByID(ctx, runID)
		if err != nil {
			t.Fatalf("read %s: %v", runID, err)
		}
		if got.RoutingBlockedStatus == nil ||
			*got.RoutingBlockedStatus != runsmodels.RoutingBlockedActionRequired {
			t.Errorf("%s routing_blocked_status = %v, want blocked_provider_action_required", runID, got.RoutingBlockedStatus)
		}
		if got.EarliestRetryAt != nil || got.ScheduledRetryAt != nil {
			t.Errorf("%s retry schedule = %v/%v, want NULL/NULL",
				runID, got.EarliestRetryAt, got.ScheduledRetryAt)
		}
	}
}
