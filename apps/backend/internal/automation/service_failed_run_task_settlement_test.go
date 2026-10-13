package automation

import (
	"context"
	"testing"

	taskmodels "github.com/kandev/kandev/internal/task/models"
)

// recordingTaskArchiver captures ArchiveTaskIfIdle calls so a test can assert
// the settlement's guard decisions without a real task service.
type recordingTaskArchiver struct {
	calls []string
	err   error
}

func (a *recordingTaskArchiver) ArchiveTaskIfIdle(_ context.Context, id string) error {
	a.calls = append(a.calls, id)
	return a.err
}

// newSettlementService builds a service over an in-memory store with the two
// side tables the settlement reads (task_sessions for the live-session
// pre-check), a recording archiver, and an origin lookup that knows one hidden
// and one visible task.
func newSettlementService(t *testing.T) (*Service, *recordingTaskArchiver) {
	t.Helper()
	svc := newTestService(t)
	if _, err := svc.store.db.Exec(`CREATE TABLE IF NOT EXISTS task_sessions (task_id TEXT, state TEXT)`); err != nil {
		t.Fatalf("create task_sessions: %v", err)
	}
	archiver := &recordingTaskArchiver{}
	svc.SetTaskArchiver(archiver)
	svc.SetTaskOriginLookup(&fakeTaskOriginLookup{results: map[string]fakeOriginResult{
		"task-hidden":  {workspaceID: "workspace", isAutomationRun: true, ok: true},
		"task-visible": {workspaceID: "workspace", isAutomationRun: false, ok: true},
	}})
	return svc, archiver
}

// seedSettlementAutomation creates the owning automation row so a run can be
// persisted.
func seedSettlementAutomation(t *testing.T, svc *Service) *Automation {
	t.Helper()
	a := &Automation{WorkspaceID: "workspace", Name: "automation", Enabled: true}
	if err := svc.store.CreateAutomation(context.Background(), a); err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	return a
}

// seedSettlementRun persists one run in the given status bound to taskID.
func seedSettlementRun(t *testing.T, svc *Service, status RunStatus, taskID string) *AutomationRun {
	t.Helper()
	a := seedSettlementAutomation(t, svc)
	run := &AutomationRun{
		AutomationID: a.ID,
		TriggerType:  TriggerTypeScheduled,
		Status:       status,
		TaskID:       taskID,
	}
	if err := svc.store.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	return run
}

func drainEnqueuedSettlement(t *testing.T, svc *Service) *AutomationRun {
	t.Helper()
	select {
	case run := <-svc.failedSettleCh:
		return run
	default:
		t.Fatal("expected a failed-run settlement to be enqueued")
		return nil
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestSettleFailedRunTask_ArchivesHiddenFailedTask(t *testing.T) {
	svc, archiver := newSettlementService(t)
	run := seedSettlementRun(t, svc, RunStatusFailed, "task-hidden")

	svc.settleFailedRunTask(context.Background(), run)

	if len(archiver.calls) != 1 || archiver.calls[0] != "task-hidden" {
		t.Fatalf("archive calls = %v, want [task-hidden]", archiver.calls)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestSettleFailedRunTask_SkipsSucceededRun(t *testing.T) {
	svc, archiver := newSettlementService(t)
	run := seedSettlementRun(t, svc, RunStatusSucceeded, "task-hidden")

	svc.settleFailedRunTask(context.Background(), run)

	if len(archiver.calls) != 0 {
		t.Fatalf("a succeeded run must not archive its task, got %v", archiver.calls)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestSettleFailedRunTask_SkipsOpenUncertainRun(t *testing.T) {
	svc, archiver := newSettlementService(t)
	run := seedSettlementRun(t, svc, RunStatusTriggered, "task-hidden")
	// An uncertain managed delivery leaves the run open. It must never be read
	// as "nothing ran", so its task is left untouched.
	run.DeliveryStatus = ManagedDeliveryUncertain

	svc.settleFailedRunTask(context.Background(), run)

	if len(archiver.calls) != 0 {
		t.Fatalf("an open uncertain-delivery run must not archive its task, got %v", archiver.calls)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestSettleFailedRunTask_SkipsVisibleTask(t *testing.T) {
	svc, archiver := newSettlementService(t)
	run := seedSettlementRun(t, svc, RunStatusFailed, "task-visible")

	svc.settleFailedRunTask(context.Background(), run)

	if len(archiver.calls) != 0 {
		t.Fatalf("a visible automation_task is user work and must not be archived, got %v", archiver.calls)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestSettleFailedRunTask_SkipsWithoutOriginLookup(t *testing.T) {
	svc, archiver := newSettlementService(t)
	svc.SetTaskOriginLookup(nil)
	run := seedSettlementRun(t, svc, RunStatusFailed, "task-hidden")

	svc.settleFailedRunTask(context.Background(), run)

	if len(archiver.calls) != 0 {
		t.Fatalf("the settlement must fail closed without an origin lookup, got %v", archiver.calls)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestSettleFailedRunTask_SkipsTaskReferencedByAnotherRun(t *testing.T) {
	svc, archiver := newSettlementService(t)
	ctx := context.Background()
	a := seedSettlementAutomation(t, svc)
	first := &AutomationRun{AutomationID: a.ID, TriggerType: TriggerTypeScheduled, Status: RunStatusFailed, TaskID: "task-hidden"}
	if err := svc.store.CreateRun(ctx, first); err != nil {
		t.Fatalf("CreateRun first: %v", err)
	}
	second := &AutomationRun{AutomationID: a.ID, TriggerType: TriggerTypeScheduled, Status: RunStatusTriggered, TaskID: "task-hidden"}
	if err := svc.store.CreateRun(ctx, second); err != nil {
		t.Fatalf("CreateRun second: %v", err)
	}

	svc.settleFailedRunTask(ctx, first)

	if len(archiver.calls) != 0 {
		t.Fatalf("a task referenced by another run must not be archived, got %v", archiver.calls)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestSettleFailedRunTask_SkipsContinuationTask(t *testing.T) {
	svc, archiver := newSettlementService(t)
	ctx := context.Background()
	a := seedSettlementAutomation(t, svc)
	if err := svc.store.SetContinuationTaskID(ctx, a.ID, "task-hidden"); err != nil {
		t.Fatalf("SetContinuationTaskID: %v", err)
	}
	run := &AutomationRun{AutomationID: a.ID, TriggerType: TriggerTypeScheduled, Status: RunStatusFailed, TaskID: "task-hidden"}
	if err := svc.store.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	svc.settleFailedRunTask(ctx, run)

	if len(archiver.calls) != 0 {
		t.Fatalf("a continuation task must not be archived, got %v", archiver.calls)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestSettleFailedRunTask_SkipsLiveTask(t *testing.T) {
	svc, archiver := newSettlementService(t)
	run := seedSettlementRun(t, svc, RunStatusFailed, "task-hidden")
	if _, err := svc.store.db.Exec(
		`INSERT INTO task_sessions (task_id, state) VALUES (?, ?)`,
		"task-hidden", string(taskmodels.TaskSessionStateRunning),
	); err != nil {
		t.Fatalf("insert running session: %v", err)
	}

	svc.settleFailedRunTask(context.Background(), run)

	if len(archiver.calls) != 0 {
		t.Fatalf("a task with a live session must not be archived, got %v", archiver.calls)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestSettleFailedRunTask_RefusesActiveSessionFromArchiver(t *testing.T) {
	svc, archiver := newSettlementService(t)
	run := seedSettlementRun(t, svc, RunStatusFailed, "task-hidden")
	archiver.err = ErrTaskActiveSessions

	svc.settleFailedRunTask(context.Background(), run)

	if len(archiver.calls) != 1 {
		t.Fatalf("the archiver should be asked exactly once, got %v", archiver.calls)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestSettleFailedRunTask_AlreadyArchivedIsIdempotent(t *testing.T) {
	svc, archiver := newSettlementService(t)
	run := seedSettlementRun(t, svc, RunStatusFailed, "task-hidden")
	archiver.err = ErrTaskAlreadyArchived

	svc.settleFailedRunTask(context.Background(), run)
	svc.settleFailedRunTask(context.Background(), run)

	if len(archiver.calls) != 2 {
		t.Fatalf("a repeated settlement must stay a no-op, got %v", archiver.calls)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestMarkRunFailedByTaskID_EnqueuesSettlement(t *testing.T) {
	svc, archiver := newSettlementService(t)
	seedSettlementRun(t, svc, RunStatusTaskCreated, "task-hidden")

	if err := svc.MarkRunFailedByTaskID(context.Background(), "task-hidden", "boom"); err != nil {
		t.Fatalf("MarkRunFailedByTaskID: %v", err)
	}

	svc.settleFailedRunTask(context.Background(), drainEnqueuedSettlement(t, svc))
	if len(archiver.calls) != 1 || archiver.calls[0] != "task-hidden" {
		t.Fatalf("archive calls = %v, want [task-hidden]", archiver.calls)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestStopRun_EnqueuesSettlement(t *testing.T) {
	svc, archiver := newSettlementService(t)
	ctx := context.Background()
	a := seedSettlementAutomation(t, svc)
	run := &AutomationRun{
		AutomationID: a.ID, TriggerType: TriggerTypeScheduled, Status: RunStatusTaskCreated,
		TaskID: "task-hidden", SessionID: "session", TurnID: "turn",
	}
	if err := svc.store.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	svc.SetRunStopper(runStopperStub{stopped: false})

	if _, err := svc.StopRun(ctx, a.ID, run.ID); err != nil {
		t.Fatalf("StopRun: %v", err)
	}

	svc.settleFailedRunTask(ctx, drainEnqueuedSettlement(t, svc))
	if len(archiver.calls) != 1 || archiver.calls[0] != "task-hidden" {
		t.Fatalf("archive calls = %v, want [task-hidden]", archiver.calls)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestReconcileOpenRuns_ArchivesRecordedFailureHiddenTask(t *testing.T) {
	svc, archiver := newSettlementService(t)
	ctx := context.Background()
	a := seedSettlementAutomation(t, svc)
	run := &AutomationRun{
		AutomationID: a.ID, TriggerType: TriggerTypeScheduled, Status: RunStatusTaskCreated,
		TaskID: "task-hidden", SessionID: "session", TurnID: "turn",
	}
	if err := svc.store.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	svc.SetRunLivenessChecker(runLivenessStub{outcome: AutomationTurnOutcomeFailed})

	if err := svc.ReconcileOpenRuns(ctx); err != nil {
		t.Fatalf("ReconcileOpenRuns: %v", err)
	}

	if len(archiver.calls) != 1 || archiver.calls[0] != "task-hidden" {
		t.Fatalf("archive calls = %v, want [task-hidden]", archiver.calls)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.7
func TestReconcileOpenRuns_RecordedSuccessKeepsTask(t *testing.T) {
	svc, archiver := newSettlementService(t)
	ctx := context.Background()
	a := seedSettlementAutomation(t, svc)
	run := &AutomationRun{
		AutomationID: a.ID, TriggerType: TriggerTypeScheduled, Status: RunStatusTaskCreated,
		TaskID: "task-hidden", SessionID: "session", TurnID: "turn",
	}
	if err := svc.store.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	svc.SetRunLivenessChecker(runLivenessStub{outcome: AutomationTurnOutcomeSucceeded})

	if err := svc.ReconcileOpenRuns(ctx); err != nil {
		t.Fatalf("ReconcileOpenRuns: %v", err)
	}
	if len(archiver.calls) != 0 {
		t.Fatalf("a succeeded run must keep its generated task, got %v", archiver.calls)
	}
	stored, err := svc.store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if stored.Status != RunStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", stored.Status)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestReconcileFailedRunTasks_ArchivesUnarchivedTaskAndSkipsArchived(t *testing.T) {
	svc, archiver := newSettlementService(t)
	ctx := context.Background()
	if _, err := svc.store.db.Exec(`CREATE TABLE IF NOT EXISTS tasks (id TEXT PRIMARY KEY, archived_at TIMESTAMP)`); err != nil {
		t.Fatalf("create tasks: %v", err)
	}
	seedSettlementRun(t, svc, RunStatusFailed, "task-hidden")
	if _, err := svc.store.db.Exec(`INSERT INTO tasks (id, archived_at) VALUES (?, NULL)`, "task-hidden"); err != nil {
		t.Fatalf("insert open task: %v", err)
	}

	if err := svc.ReconcileFailedRunTasks(ctx); err != nil {
		t.Fatalf("ReconcileFailedRunTasks: %v", err)
	}
	if len(archiver.calls) != 1 || archiver.calls[0] != "task-hidden" {
		t.Fatalf("archive calls = %v, want [task-hidden]", archiver.calls)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestReconcileFailedRunTasks_SkipsTaskRowAlreadyArchived(t *testing.T) {
	svc, archiver := newSettlementService(t)
	ctx := context.Background()
	if _, err := svc.store.db.Exec(`CREATE TABLE IF NOT EXISTS tasks (id TEXT PRIMARY KEY, archived_at TIMESTAMP)`); err != nil {
		t.Fatalf("create tasks: %v", err)
	}
	seedSettlementRun(t, svc, RunStatusFailed, "task-hidden")
	if _, err := svc.store.db.Exec(`INSERT INTO tasks (id, archived_at) VALUES (?, CURRENT_TIMESTAMP)`, "task-hidden"); err != nil {
		t.Fatalf("insert archived task: %v", err)
	}

	if err := svc.ReconcileFailedRunTasks(ctx); err != nil {
		t.Fatalf("ReconcileFailedRunTasks: %v", err)
	}
	if len(archiver.calls) != 0 {
		t.Fatalf("an already-archived task must not be settled again, got %v", archiver.calls)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestReconcileFailedRunTasks_NoTasksTableIsNoOp(t *testing.T) {
	svc, archiver := newSettlementService(t)
	seedSettlementRun(t, svc, RunStatusFailed, "task-hidden")

	if err := svc.ReconcileFailedRunTasks(context.Background()); err != nil {
		t.Fatalf("ReconcileFailedRunTasks: %v", err)
	}
	if len(archiver.calls) != 0 {
		t.Fatalf("a missing tasks table must leave tasks untouched, got %v", archiver.calls)
	}
}

// @covers AC-OFFICE-AUTOMATION-CONTINUITY-003.6
func TestGetLatestFailedRunByTask(t *testing.T) {
	svc, _ := newSettlementService(t)
	ctx := context.Background()
	run := seedSettlementRun(t, svc, RunStatusFailed, "task-hidden")

	got, err := svc.store.GetLatestFailedRunByTask(ctx, "task-hidden")
	if err != nil {
		t.Fatalf("GetLatestFailedRunByTask: %v", err)
	}
	if got == nil || got.ID != run.ID {
		t.Fatalf("got %+v, want run %s", got, run.ID)
	}
	none, err := svc.store.GetLatestFailedRunByTask(ctx, "no-such-task")
	if err != nil {
		t.Fatalf("GetLatestFailedRunByTask miss: %v", err)
	}
	if none != nil {
		t.Fatalf("a task with no failed run must return nil, got %+v", none)
	}
}
