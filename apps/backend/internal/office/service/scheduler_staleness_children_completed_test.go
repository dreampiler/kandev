package service_test

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/office/models"
	"github.com/kandev/kandev/internal/office/repository/sqlite"
	"github.com/kandev/kandev/internal/office/service"
	runsmodels "github.com/kandev/kandev/internal/runs/models"
)

// consumedWakeFixture queues one task_children_completed run for a parent in
// the given state, so each case only varies what would make the wake live.
type consumedWakeFixture struct {
	svc   *service.Service
	mock  *mockTaskStarter
	agent *models.AgentInstance
	runID string
}

func newConsumedWakeFixture(t *testing.T, parentState string) *consumedWakeFixture {
	t.Helper()
	mock := &mockTaskStarter{}
	svc := newTestService(t, service.ServiceOptions{TaskStarter: mock})
	ctx := context.Background()

	agent := makeAgent("parent-agent-consumed-wake", models.AgentRoleWorker)
	agent.ExecutorPreference = `{"type":"local_pc"}`
	if err := svc.CreateAgentInstance(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	svc.ExecSQL(t, `INSERT INTO tasks (id, workspace_id, title, state, created_at, updated_at)
		VALUES ('parent-consumed', 'ws-1', 'Parent', ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, parentState)
	if _, err := svc.QueueRun(ctx, agent.ID, service.RunReasonTaskChildrenCompleted,
		`{"task_id":"parent-consumed"}`, ""); err != nil {
		t.Fatalf("queue run: %v", err)
	}
	runs, err := svc.ListRuns(ctx, "ws-1")
	if err != nil || len(runs) != 1 {
		t.Fatalf("list runs: %v (%d)", err, len(runs))
	}
	return &consumedWakeFixture{svc: svc, mock: mock, agent: agent, runID: runs[0].ID}
}

func (f *consumedWakeFixture) coalesceWakeup(t *testing.T, id, reason string) {
	t.Helper()
	ctx := context.Background()
	repo := f.svc.RepoForTest()
	if err := repo.CreateWakeupRequest(ctx, &sqlite.WakeupRequest{
		ID: id, AgentProfileID: f.agent.ID, Source: "routine", Reason: reason, Payload: `{}`,
	}); err != nil {
		t.Fatalf("create wakeup request: %v", err)
	}
	if err := repo.MarkWakeupRequestCoalesced(ctx, id, f.runID); err != nil {
		t.Fatalf("coalesce wakeup request: %v", err)
	}
}

func (f *consumedWakeFixture) run(t *testing.T) *runsmodels.Run {
	t.Helper()
	runs, err := f.svc.ListRuns(context.Background(), "ws-1")
	if err != nil || len(runs) != 1 {
		t.Fatalf("list runs: %v (%d)", err, len(runs))
	}
	return runs[0]
}

func (f *consumedWakeFixture) requireCancelledAsConsumed(t *testing.T) {
	t.Helper()
	if f.mock.callCount() != 0 {
		t.Fatalf("StartTask calls = %d, want 0 for a consumed wake", f.mock.callCount())
	}
	got := f.run(t)
	if got.Status != service.RunStatusCancelled {
		t.Fatalf("run status = %q, want %q", got.Status, service.RunStatusCancelled)
	}
	if got.CancelReason == nil || *got.CancelReason != "parent_task_terminal" {
		t.Fatalf("cancel reason = %v, want parent_task_terminal", got.CancelReason)
	}
}

func (f *consumedWakeFixture) requireLaunched(t *testing.T) {
	t.Helper()
	if f.mock.callCount() != 1 {
		t.Fatalf("StartTask calls = %d, want 1 for a live wake", f.mock.callCount())
	}
	if got := f.run(t); got.Status == service.RunStatusCancelled {
		t.Fatalf("run was cancelled (%v); a live wake must launch", got.CancelReason)
	}
}

func TestSchedulerTick_ChildrenCompletedForTerminalParentIsCancelledWithoutLaunch(t *testing.T) {
	for _, state := range []string{"COMPLETED", "CANCELLED"} {
		t.Run(state, func(t *testing.T) {
			f := newConsumedWakeFixture(t, state)
			service.RunSchedulerTick(f.svc, context.Background())
			f.requireCancelledAsConsumed(t)
		})
	}
}

func TestSchedulerTick_ChildrenCompletedForLiveParentStillLaunches(t *testing.T) {
	f := newConsumedWakeFixture(t, "IN_PROGRESS")
	service.RunSchedulerTick(f.svc, context.Background())
	f.requireLaunched(t)
}

// Periodic routine wakes merged into a consumed run are released exactly as an
// idle skip would release them; the next cron tick queues a fresh run.
func TestSchedulerTick_ConsumedWakeCarryingOnlyPeriodicWakesIsCancelled(t *testing.T) {
	f := newConsumedWakeFixture(t, "COMPLETED")
	f.coalesceWakeup(t, "wake-cron-1", "routine_dispatch_cron")
	f.coalesceWakeup(t, "wake-cron-2", "routine_dispatch_cron")
	service.RunSchedulerTick(f.svc, context.Background())
	f.requireCancelledAsConsumed(t)
}

// An event-triggered wake (a manual or webhook routine fire) coalesced into the
// run must still be delivered, so the run launches even though its parent is done.
func TestSchedulerTick_ConsumedWakeCarryingAnEventWakeStillLaunches(t *testing.T) {
	f := newConsumedWakeFixture(t, "COMPLETED")
	f.coalesceWakeup(t, "wake-cron", "routine_dispatch_cron")
	f.coalesceWakeup(t, "wake-event", "routine_dispatch_event")
	service.RunSchedulerTick(f.svc, context.Background())
	f.requireLaunched(t)
}

// A run parked by the session ceiling re-enters processRun when it is lifted;
// a parent that reached Done while it waited is caught then, before launch.
func TestSchedulerTick_LiftedCeilingParkedWakeIsCancelledOnceParentIsDone(t *testing.T) {
	f := newConsumedWakeFixture(t, "IN_PROGRESS")
	f.mock.err = service.ErrLaunchDeferredByCapacity
	service.RunSchedulerTick(f.svc, context.Background())
	if f.mock.callCount() != 1 {
		t.Fatalf("StartTask calls = %d, want 1 deferred attempt", f.mock.callCount())
	}

	f.svc.ExecSQL(t, `UPDATE tasks SET state = 'COMPLETED' WHERE id = 'parent-consumed'`)
	f.svc.ExecSQL(t, `UPDATE runs SET routing_blocked_status = NULL, earliest_retry_at = NULL,
		scheduled_retry_at = NULL WHERE id = ?`, f.runID)
	f.mock.err = nil
	service.RunSchedulerTick(f.svc, context.Background())

	if f.mock.callCount() != 1 {
		t.Fatalf("StartTask calls = %d, want no launch after the parent finished", f.mock.callCount())
	}
	got := f.run(t)
	if got.Status != service.RunStatusCancelled || got.CancelReason == nil ||
		*got.CancelReason != "parent_task_terminal" {
		t.Fatalf("run = %s/%v, want cancelled/parent_task_terminal", got.Status, got.CancelReason)
	}
}
