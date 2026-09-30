package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/automation"
	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// deferredAutomationDispatchService models the service-owned dispatcher
// converting the orchestrator's ceiling sentinel into ErrRunDeferred: the run
// row stays open and the task must not be reclaimed. It never runs the
// dispatch callback, exactly as DispatchRun does not on a refused launch.
type deferredAutomationDispatchService struct {
	*stubAutomationService
}

func (s *deferredAutomationDispatchService) DispatchRun(
	context.Context,
	string,
	automation.ThreadAction,
	string,
	func() (automation.RunDispatch, error),
) error {
	return automation.ErrRunDeferred
}

// A ceiling refusal is not a failure: the task the deferred launch lives on
// must survive so the sweep can replay it, and no run may be marked failed.
func TestAutoStartAutomationTask_DeferredLaunchKeepsTheTaskAndRunOpen(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedAutomationTask(t, repo, "t-deferred", models.TaskOriginAutomationRun, false)

	autoSvc := &deferredAutomationDispatchService{stubAutomationService: &stubAutomationService{}}
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{})
	svc.SetTaskLifecycleDeleter(taskLifecycleDeleterFunc(func(ctx context.Context, id string) error {
		return repo.DeleteTask(ctx, id)
	}))
	svc.SetAutomationService(autoSvc)

	svc.autoStartAutomationTaskForRun(
		ctx,
		&automation.Automation{ID: "a-deferred", WorkspaceID: "ws-t-deferred"},
		&models.Task{ID: "t-deferred", Description: "sweep"},
		"",
		"run-deferred",
		automation.ThreadActionCreated,
		"",
	)

	surviving, err := repo.GetTask(ctx, "t-deferred")
	require.NoError(t, err)
	require.NotNil(t, surviving, "a ceiling-deferred launch must not delete its task")
	require.Empty(t, autoSvc.failed, "a deferred launch is not a failed run")
}

// The deferred branch must not swallow real dispatch failures: a launch that
// truly failed still reclaims its task, as it did before the ceiling fix.
func TestAutoStartAutomationTask_HardDispatchFailureStillReclaimsTheTask(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedAutomationTask(t, repo, "t-hardfail", models.TaskOriginAutomationRun, false)

	autoSvc := &rejectingAutomationService{
		stubAutomationService: &stubAutomationService{},
		dispatchErr:           errors.New("executor unavailable"),
	}
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{})
	svc.SetTaskLifecycleDeleter(taskLifecycleDeleterFunc(func(ctx context.Context, id string) error {
		return repo.DeleteTask(ctx, id)
	}))
	svc.SetAutomationService(autoSvc)

	svc.autoStartAutomationTaskForRun(
		ctx,
		&automation.Automation{ID: "a-hardfail", WorkspaceID: "ws-t-hardfail"},
		&models.Task{ID: "t-hardfail", Description: "sweep"},
		"",
		"run-hardfail",
		automation.ThreadActionCreated,
		"",
	)

	surviving, err := repo.GetTask(ctx, "t-hardfail")
	if err == nil {
		require.Nil(t, surviving, "an ordinary dispatch failure still reclaims its orphaned task")
	}
}

// An automation trigger is the start signal, so a deferred automation launch
// must survive the sweep's drop-reason evaluation even when its workflow step
// has no on_enter auto_start_agent action. Otherwise the record is dropped and
// the retained task is never retried.
func TestEvaluateCeilingDropReasons_AutomationStartIgnoresWorkflowAutoStartEligibility(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	stepGetter := newMockStepGetter()
	svc.workflowStepGetter = stepGetter
	ctx := context.Background()

	stepGetter.steps["automation-step-no-auto-start"] = &wfmodels.WorkflowStep{ID: "automation-step-no-auto-start"}
	task := &models.Task{
		ID: "keep-automation-start", Title: "Automation run", State: v1.TaskStateInProgress,
		WorkflowStepID: "automation-step-no-auto-start", Origin: models.TaskOriginAutomationRun,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	require.NoError(t, repo.CreateTask(ctx, task))

	reasonCode, detail, drop := svc.evaluateCeilingDropReasons(ctx, task, models.CeilingDeferral{
		Kind: models.CeilingLaunchStart, Payload: map[string]interface{}{},
	})
	require.False(t, drop, "an automation start is not governed by workflow auto-start eligibility")
	require.Empty(t, reasonCode)
	require.Empty(t, detail)
}

// deferredAutomationStartFixture drives a real automation service and store
// through a ceiling-deferred start: the run row is admitted, another launch
// holds the ceiling's only slot, and the automation start is refused.
type deferredAutomationStartFixture struct {
	svc       *Service
	autoStore *automation.Store
	taskID    string
	runID     string
	autoID    string
	launchErr error
}

const deferredAutomationOccupierSession = "occupier-session"

func deferAutomationStartAtCeiling(t *testing.T, taskID string) *deferredAutomationStartFixture {
	t.Helper()
	ctx := context.Background()
	base := setupAutomationRetentionFixture(t)
	seedAutomationTask(t, base.repo, taskID, models.TaskOriginAutomationRun, false)

	f := &deferredAutomationStartFixture{autoStore: base.autoStore, taskID: taskID}
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)
	agentMgr := &mockAgentManager{
		launchAgentFunc: func(context.Context, *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
			if f.launchErr != nil {
				return nil, f.launchErr
			}
			return &executor.LaunchAgentResponse{AgentExecutionID: "exec-" + taskID}, nil
		},
	}
	f.svc = createTestServiceWithScheduler(base.repo, newMockStepGetter(), taskRepo, agentMgr)
	f.svc.sessionCeiling = newSessionCeilingController(1, nil, nil)
	f.svc.turnService = &repoTurnService{repo: base.repo}
	f.svc.SetAutomationService(base.autoSvc)

	a := &automation.Automation{
		WorkspaceID: "ws-" + taskID, Name: "watch", Enabled: true,
		AgentProfileID: "profile-1", MaxConcurrentRuns: 1,
	}
	require.NoError(t, base.autoStore.CreateAutomation(ctx, a))
	run := &automation.AutomationRun{
		AutomationID: a.ID, TriggerType: automation.TriggerTypeScheduled,
		TaskID: taskID, Status: automation.RunStatusTriggered, TriggerData: json.RawMessage(`{}`),
	}
	require.NoError(t, base.autoStore.CreateRun(ctx, run))
	f.autoID, f.runID = a.ID, run.ID

	require.True(t, f.svc.sessionCeiling.admit(ctx, admissionRequest{
		taskID: "occupier", sessionID: deferredAutomationOccupierSession, origin: launchOriginAutomatic, seam: "test-setup",
	}).admitted)
	f.svc.autoStartAutomationTaskForRun(ctx, a, &models.Task{ID: taskID, Description: "sweep"}, "",
		run.ID, automation.ThreadActionCreated, "new task created for automation run")

	deferred := f.run(t)
	require.Equal(t, automation.RunStatusTaskCreated, deferred.Status, "a deferred launch leaves its run open")
	require.Empty(t, deferred.SessionID, "no session exists while the launch is deferred")
	require.True(t, f.ceilingDeferred(t), "the refused automation start must be recorded for replay")
	return f
}

func (f *deferredAutomationStartFixture) run(t *testing.T) *automation.AutomationRun {
	t.Helper()
	run, err := f.autoStore.GetRun(context.Background(), f.runID)
	require.NoError(t, err)
	require.NotNil(t, run)
	return run
}

func (f *deferredAutomationStartFixture) ceilingDeferred(t *testing.T) bool {
	t.Helper()
	return models.HasCeilingDeferredIntent(&models.Task{Metadata: map[string]interface{}{
		models.MetaKeyDeferredLaunch: deferredLaunchOf(t, f.svc, f.taskID),
	}})
}

func (f *deferredAutomationStartFixture) activeRuns(t *testing.T) int {
	t.Helper()
	active, err := f.autoStore.CountActiveRuns(context.Background(), f.autoID)
	require.NoError(t, err)
	return active
}

// A replayed automation start is bound to its run exactly as a direct start
// is, so the turn it launched settles the run and frees the
// max_concurrent_runs slot.
func TestDrainDeferredCeilingLaunches_BindsReplayedAutomationStartToItsRun(t *testing.T) {
	ctx := context.Background()
	f := deferAutomationStartAtCeiling(t, "t-replay-bind")

	f.svc.drainDeferredCeilingLaunches(ctx)
	require.True(t, f.ceilingDeferred(t), "a still-refused replay keeps the record, run identity included")
	require.Empty(t, f.run(t).SessionID)

	f.svc.sessionCeiling.release(deferredAutomationOccupierSession)
	f.svc.drainDeferredCeilingLaunches(ctx)

	bound := f.run(t)
	require.False(t, f.ceilingDeferred(t), "a dispatched replay clears the record")
	require.Equal(t, automation.RunStatusTaskCreated, bound.Status)
	require.NotEmpty(t, bound.SessionID, "the replay must bind the session it launched to the run")
	require.NotEmpty(t, bound.TurnID, "the replay must bind the turn it launched to the run")
	require.Equal(t, automation.ThreadActionCreated, bound.ThreadAction)
	require.Equal(t, "new task created for automation run", bound.ThreadReason)
	require.Equal(t, 1, f.activeRuns(t))

	session, err := f.svc.repo.GetTaskSession(ctx, bound.SessionID)
	require.NoError(t, err)
	require.Equal(t, f.taskID, session.TaskID)
	require.True(t, f.svc.handleAutomationTurnCompleteForTurn(
		ctx, f.taskID, bound.SessionID, session, bound.TurnID, "end_turn", false, ""))

	require.Equal(t, automation.RunStatusSucceeded, f.run(t).Status,
		"the bound turn's completion must settle the run")
	require.Zero(t, f.activeRuns(t), "a settled run must release its max_concurrent_runs slot")
}

// A replay whose launch fails for a non-ceiling reason fails the run and
// clears the record: a run left at task_created would hold its
// max_concurrent_runs slot with no completion event coming to free it.
func TestDrainDeferredCeilingLaunches_FailedAutomationReplayFailsItsRun(t *testing.T) {
	ctx := context.Background()
	f := deferAutomationStartAtCeiling(t, "t-replay-fail")
	f.launchErr = errors.New("executor unavailable")

	f.svc.sessionCeiling.release(deferredAutomationOccupierSession)
	f.svc.drainDeferredCeilingLaunches(ctx)

	failed := f.run(t)
	require.Equal(t, automation.RunStatusFailed, failed.Status)
	require.Contains(t, failed.ErrorMessage, "executor unavailable")
	require.False(t, f.ceilingDeferred(t), "a failed run's launch must not stay queued for another replay")
	require.Zero(t, f.activeRuns(t))
}

// Dropping a deferred automation start settles its run, since nothing else
// will: the launch that would have produced a completion event never happens.
func TestDropCeilingDeferral_FailsTheDeferredAutomationRun(t *testing.T) {
	ctx := context.Background()
	f := deferAutomationStartAtCeiling(t, "t-replay-drop")
	require.NoError(t, f.svc.repo.(interface {
		ArchiveTask(context.Context, string) error
	}).ArchiveTask(ctx, f.taskID))

	f.svc.drainDeferredCeilingLaunches(ctx)

	dropped := f.run(t)
	require.Equal(t, automation.RunStatusFailed, dropped.Status)
	require.Contains(t, dropped.ErrorMessage, "task is archived")
	require.False(t, f.ceilingDeferred(t))
}
