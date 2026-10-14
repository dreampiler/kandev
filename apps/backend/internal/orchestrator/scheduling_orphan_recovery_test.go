package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

const (
	orphanAutoStep = "orphan-step-auto"
	orphanHoldStep = "orphan-step-hold"
)

// schedulingOrphanFixture is a service whose startup sweep runs against a real
// SQLite repository, with launches counted at the agent manager.
type schedulingOrphanFixture struct {
	repo     *sqliterepo.Repository
	taskRepo *mockTaskRepo
	agentMgr *mockAgentManager
	launches *launchCounter
	svc      *Service
}

func newSchedulingOrphanFixture(t *testing.T) *schedulingOrphanFixture {
	t.Helper()
	ctx := context.Background()
	repo := setupTestRepo(t)
	now := time.Now().UTC()
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws1", Name: "Test", CreatedAt: now, UpdatedAt: now}))
	require.NoError(t, repo.CreateWorkflow(ctx, &models.Workflow{ID: "wf1", WorkspaceID: "ws1", Name: "WF", CreatedAt: now, UpdatedAt: now}))

	steps := newMockStepGetter()
	steps.steps[orphanAutoStep] = &wfmodels.WorkflowStep{
		ID: orphanAutoStep, WorkflowID: "wf1", Name: "Auto",
		Events: wfmodels.StepEvents{OnEnter: []wfmodels.OnEnterAction{{Type: wfmodels.OnEnterAutoStartAgent}}},
	}
	steps.steps[orphanHoldStep] = &wfmodels.WorkflowStep{ID: orphanHoldStep, WorkflowID: "wf1", Name: "Hold"}

	launches := newLaunchCounter()
	agentMgr := &mockAgentManager{
		repoForExecutionLookup: repo,
		launchAgentFunc: func(context.Context, *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
			launches.record()
			return &executor.LaunchAgentResponse{AgentExecutionID: "exec-orphan"}, nil
		},
	}
	taskRepo := newMockTaskRepo()
	svc := createTestServiceWithScheduler(repo, steps, taskRepo, agentMgr)
	svc.SetTaskDependencyReader(&launchDependencyReader{})
	return &schedulingOrphanFixture{repo: repo, taskRepo: taskRepo, agentMgr: agentMgr, launches: launches, svc: svc}
}

// seedTask stores a SCHEDULING task on stepID in both repositories.
func (f *schedulingOrphanFixture) seedTask(t *testing.T, taskID, stepID string, mutate func(*models.Task)) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	task := &models.Task{
		ID: taskID, WorkspaceID: "ws1", WorkflowID: "wf1", WorkflowStepID: stepID,
		Title: "Orphan", Description: "desc", State: v1.TaskStateScheduling,
		Metadata:  map[string]interface{}{},
		CreatedAt: now, UpdatedAt: now,
	}
	if mutate != nil {
		mutate(task)
	}
	if task.Metadata == nil {
		task.Metadata = map[string]interface{}{}
	}
	task.Metadata[models.MetaKeyAgentProfileID] = testAgentProfileID
	require.NoError(t, f.repo.CreateTask(ctx, task))
	require.NoError(t, f.repo.UpdateTaskState(ctx, taskID, v1.TaskStateScheduling))
	f.taskRepo.tasks[taskID] = &v1.Task{
		ID: taskID, WorkflowID: "wf1", Title: "Orphan", Description: "desc", State: v1.TaskStateScheduling,
		Metadata: map[string]interface{}{models.MetaKeyAgentProfileID: testAgentProfileID},
	}
}

func (f *schedulingOrphanFixture) seedSession(t *testing.T, taskID, sessionID string, state models.TaskSessionState) {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, f.repo.CreateTaskSession(context.Background(), &models.TaskSession{
		ID: sessionID, TaskID: taskID, State: state, AgentProfileID: testAgentProfileID,
		QueueIncarnationID: sessionID + "-incarnation", StartedAt: now, UpdatedAt: now,
	}))
}

// runStartupSweep runs the background startup lifecycle sweep to completion.
func (f *schedulingOrphanFixture) runStartupSweep(t *testing.T) {
	t.Helper()
	require.True(t, f.svc.startLifecycleSweepAsync(context.Background()))
	f.svc.lifecycleSweepWorkers.Wait()
}

func (f *schedulingOrphanFixture) taskState(taskID string) v1.TaskState {
	f.taskRepo.mu.Lock()
	defer f.taskRepo.mu.Unlock()
	return f.taskRepo.tasks[taskID].State
}

// AC-TASKS-RESTART-ORPHAN-SESSIONS-003.1, 003.4: an interrupted automatic start
// with no session starts once, and a second sweep does not start it again.
func TestSchedulingOrphanWithoutSessionStartsOnceAfterRestart(t *testing.T) {
	f := newSchedulingOrphanFixture(t)
	f.seedTask(t, "orphan-no-session", orphanAutoStep, nil)

	f.runStartupSweep(t)
	require.True(t, f.launches.awaitLaunch(0), "the interrupted automatic start was not restarted")
	awaitLaunchedSession(t, f.repo, "orphan-no-session")

	f.runStartupSweep(t)
	require.False(t, f.launches.awaitLaunch(1), "a second startup sweep started the task again")
	require.Equal(t, 1, sessionCount(t, f.repo, "orphan-no-session"))
}

// AC-TASKS-RESTART-ORPHAN-SESSIONS-003.2: every task with another owner, or
// without an automatic start, keeps its state and starts nothing.
func TestSchedulingOrphanLeavesOwnedTasksAlone(t *testing.T) {
	cases := map[string]struct {
		stepID string
		mutate func(*models.Task)
		setup  func(*schedulingOrphanFixture)
	}{
		"workflow queue destination": {stepID: orphanAutoStep, mutate: func(task *models.Task) {
			task.QueuedForStepID = orphanAutoStep
		}},
		"deferred launch record": {stepID: orphanAutoStep, mutate: func(task *models.Task) {
			task.Metadata = map[string]interface{}{models.MetaKeyDeferredLaunch: map[string]interface{}{
				"intent": "start", "agent_profile_id": testAgentProfileID, models.DeferredLaunchStartWhenUnblockedKey: true,
			}}
		}},
		"recorded launch error": {stepID: orphanAutoStep, mutate: func(task *models.Task) {
			task.Metadata = map[string]interface{}{}
			models.SetTaskLaunchError(task.Metadata, models.TaskLaunchError{Message: "model unavailable", OccurredAt: time.Now().UTC()})
		}},
		"step without automatic start": {stepID: orphanHoldStep},
		"unresolved dependency": {stepID: orphanAutoStep, setup: func(f *schedulingOrphanFixture) {
			f.svc.SetTaskDependencyReader(&launchDependencyReader{blocked: true})
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newSchedulingOrphanFixture(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			f.seedTask(t, "orphan-owned", tc.stepID, tc.mutate)

			f.runStartupSweep(t)
			require.False(t, f.launches.awaitLaunch(0), "an owned or agentless task was started")
			require.Equal(t, 0, sessionCount(t, f.repo, "orphan-owned"))
			require.Equal(t, v1.TaskStateScheduling, f.taskState("orphan-owned"))
		})
	}
}

// AC-TASKS-RESTART-ORPHAN-SESSIONS-003.2: any present deferred_launch value,
// including an empty or non-object one, keeps its existing owner, as the
// ordinary launchDeferredTask reader does. CreateTask drops such a record from
// a fresh row, so eligibility is checked on the loaded task directly.
func TestSchedulingOrphanKeepsMalformedDeferredRecordWithItsOwner(t *testing.T) {
	f := newSchedulingOrphanFixture(t)
	f.seedTask(t, "orphan-malformed", orphanAutoStep, nil)
	task, err := f.repo.GetTask(context.Background(), "orphan-malformed")
	require.NoError(t, err)
	require.True(t, f.svc.schedulingOrphanRecoverable(context.Background(), task))
	for name, record := range map[string]interface{}{
		"empty": map[string]interface{}{}, "non-object": "invalid", "null": nil,
	} {
		task.Metadata[models.MetaKeyDeferredLaunch] = record
		require.Falsef(t, f.svc.schedulingOrphanRecoverable(context.Background(), task), "%s record was treated as absent", name)
	}
}

// AC-TASKS-RESTART-ORPHAN-SESSIONS-003.1: the restarted start passes session
// ceiling admission. With the only slot taken, it is deferred, not launched.
func TestSchedulingOrphanRestartRespectsSessionCeiling(t *testing.T) {
	f := newSchedulingOrphanFixture(t)
	f.svc.sessionCeiling = newSessionCeilingController(1, &fakeAdmittedLister{ids: []string{"other-running-session"}}, nil)
	f.seedTask(t, "orphan-at-ceiling", orphanAutoStep, nil)

	f.runStartupSweep(t)
	require.False(t, f.launches.awaitLaunch(0), "a start over the session ceiling was launched")
	require.Equal(t, 0, sessionCount(t, f.repo, "orphan-at-ceiling"))
	record := deferredLaunchOf(t, f.svc, "orphan-at-ceiling")
	require.NotNil(t, record, "the refused start was not handed to the ceiling queue")
	require.Equal(t, true, record[models.CeilingDeferredKey])
}

// AC-TASKS-RESTART-ORPHAN-SESSIONS-003.3: a session whose step entry was already
// delivered is not prompted again; only the stale task state is repaired.
func TestSchedulingOrphanWithPromptHistoryOnlyRepairsState(t *testing.T) {
	f := newSchedulingOrphanFixture(t)
	f.seedTask(t, "orphan-prompted", orphanAutoStep, nil)
	f.seedSession(t, "orphan-prompted", "orphan-prompted-session", models.TaskSessionStateWaitingForInput)
	// Create a turn for the current step to simulate the step entry prompt
	// having been delivered. The turn is stamped with the current step ID.
	turn := &models.Turn{
		ID:            "orphan-prompted-turn",
		TaskSessionID: "orphan-prompted-session",
		TaskID:        "orphan-prompted",
		StartedAt:     time.Now().UTC(),
		Metadata: map[string]interface{}{
			models.TurnMetaKeyWorkflowStepIDAtStart: orphanAutoStep,
		},
	}
	_, err := f.repo.CreateTurnWithStepStamp(context.Background(), turn)
	require.NoError(t, err)

	f.runStartupSweep(t)
	require.False(t, f.launches.awaitLaunch(0), "a session with a turn for the current step was started again")
	require.Equal(t, v1.TaskStateInProgress, f.taskState("orphan-prompted"))
	require.Empty(t, f.agentMgr.capturedPrompts)
	require.Equal(t, 1, sessionCount(t, f.repo, "orphan-prompted"))
}

// AC-TASKS-RESTART-ORPHAN-SESSIONS-003.3: a lifecycle-only turn carries the
// current step stamp but is not an accepted prompt, so it does not stop the
// step entry from being delivered.
func TestSchedulingOrphanLifecycleOnlyTurnIsNotPromptHistory(t *testing.T) {
	f := newSchedulingOrphanFixture(t)
	f.seedTask(t, "orphan-lifecycle", orphanAutoStep, nil)
	f.seedSession(t, "orphan-lifecycle", "orphan-lifecycle-session", models.TaskSessionStateWaitingForInput)
	_, err := f.repo.CreateTurnWithStepStamp(context.Background(), &models.Turn{
		ID: "orphan-lifecycle-turn", TaskSessionID: "orphan-lifecycle-session", TaskID: "orphan-lifecycle",
		StartedAt: time.Now().UTC(),
		Metadata: map[string]interface{}{
			models.TurnMetaKeyWorkflowStepIDAtStart: orphanAutoStep,
			models.TurnMetaKeyLifecycleOnly:         true,
		},
	})
	require.NoError(t, err)

	hasTurn, err := f.svc.sessionHasTurnForStep(context.Background(), "orphan-lifecycle-session", orphanAutoStep)
	require.NoError(t, err)
	require.False(t, hasTurn)
	f.runStartupSweep(t)
	require.Equal(t, v1.TaskStateScheduling, f.taskState("orphan-lifecycle"), "a lifecycle-only turn repaired the task as if a prompt was accepted")
}

// AC-TASKS-RESTART-ORPHAN-SESSIONS-003.1: a resumable session left by the prior
// step is re-entered with the accepted entry id and the step that was left.
func TestSchedulingOrphanEntryContextUsesLatestLedgerEntry(t *testing.T) {
	f := newSchedulingOrphanFixture(t)
	f.seedTask(t, "orphan-entry", orphanHoldStep, nil)
	ctx := context.Background()
	task, err := f.repo.GetTask(ctx, "orphan-entry")
	require.NoError(t, err)
	task.WorkflowStepID = orphanAutoStep
	require.NoError(t, f.repo.UpdateTask(ctx, task))
	task, err = f.repo.GetTask(ctx, "orphan-entry")
	require.NoError(t, err)
	f.seedSession(t, "orphan-entry", "orphan-entry-session", models.TaskSessionStateWaitingForInput)
	session, err := f.repo.GetTaskSession(ctx, "orphan-entry-session")
	require.NoError(t, err)

	id, source := f.svc.schedulingOrphanEntryContext(ctx, task, session)
	require.Positive(t, id)
	require.NotNil(t, source)
	require.Equal(t, orphanHoldStep, source.ID)

	// A route that already names the current step makes the session a
	// same-step recipient: no entry context is recovered.
	task.Metadata[models.MetaKeyWorkflowSessionRoute] = models.WorkflowSessionRoute{
		OperationID: "op", DestinationStepID: orphanAutoStep, TargetKind: "step", Phase: "committed",
	}
	id, source = f.svc.schedulingOrphanEntryContext(ctx, task, session)
	require.Zero(t, id)
	require.Nil(t, source)
}

// AC-TASKS-RESTART-ORPHAN-SESSIONS-003.3: CREATED, STARTING, and RUNNING
// sessions belong to a live launch and are not touched.
func TestSchedulingOrphanLeavesLiveSessionsAlone(t *testing.T) {
	for _, state := range []models.TaskSessionState{
		models.TaskSessionStateCreated, models.TaskSessionStateStarting, models.TaskSessionStateRunning,
	} {
		t.Run(string(state), func(t *testing.T) {
			f := newSchedulingOrphanFixture(t)
			f.seedTask(t, "orphan-live", orphanAutoStep, nil)
			f.seedSession(t, "orphan-live", "orphan-live-session", state)

			f.runStartupSweep(t)
			require.False(t, f.launches.awaitLaunch(0), "a live session was started again")
			require.Equal(t, v1.TaskStateScheduling, f.taskState("orphan-live"))
			require.Equal(t, 1, sessionCount(t, f.repo, "orphan-live"))
		})
	}
}

// AC-TASKS-RESTART-ORPHAN-SESSIONS-003.3: a resumable session whose step entry
// never delivered a prompt receives the entry in place, without a second session.
func TestSchedulingOrphanReentersWaitingForInputSessionWithoutPromptHistory(t *testing.T) {
	f := newSchedulingOrphanFixture(t)
	f.seedTask(t, "orphan-waiting", orphanAutoStep, nil)
	f.seedSession(t, "orphan-waiting", "orphan-waiting-session", models.TaskSessionStateWaitingForInput)
	now := time.Now().UTC()
	require.NoError(t, f.repo.UpsertExecutorRunning(context.Background(), &models.ExecutorRunning{
		ID: "orphan-waiting-running", SessionID: "orphan-waiting-session", TaskID: "orphan-waiting",
		AgentExecutionID: "exec-before-restart", ResumeToken: "acp-orphan-waiting", Resumable: true,
		CreatedAt: now, UpdatedAt: now,
	}))
	f.agentMgr.isAgentReadyFn = func(context.Context, string) bool { return true }

	// The sweep must return before the resumed agent reports readiness, so a
	// slow resume cannot hold the rest of the startup sweep.
	f.runStartupSweep(t)
	require.True(t, f.launches.awaitLaunch(0), "the waiting session was not resumed for the step entry")
	require.Eventually(t, func() bool {
		session, err := f.repo.GetTaskSession(context.Background(), "orphan-waiting-session")
		return err == nil && session.State == models.TaskSessionStateStarting
	}, 2*time.Second, 10*time.Millisecond, "the resumed session never reached STARTING")
	// Stand in for the agent's boot-ready event.
	require.NoError(t, f.repo.UpdateTaskSessionState(context.Background(), "orphan-waiting-session", models.TaskSessionStateWaitingForInput, ""))
	require.Eventually(t, func() bool {
		f.agentMgr.mu.Lock()
		defer f.agentMgr.mu.Unlock()
		return len(f.agentMgr.capturedPrompts) > 0
	}, 2*time.Second, 10*time.Millisecond, "the step entry prompt was not delivered")
	require.Equal(t, 1, sessionCount(t, f.repo, "orphan-waiting"), "re-entry created a second session")
}

// AC-TASKS-RESTART-ORPHAN-SESSIONS-003.3: the detached re-entry work is owned
// by the service, so shutdown joins it before returning instead of leaving the
// entry to touch repository and agent state after teardown begins.
func TestStopOrphanReentryWorkersJoinsTrackedWork(t *testing.T) {
	f := newSchedulingOrphanFixture(t)

	release := make(chan struct{})
	f.svc.orphanReentryWorkers.Add(1)
	go func() {
		defer f.svc.orphanReentryWorkers.Done()
		<-release
	}()

	stopped := make(chan struct{})
	go func() {
		f.svc.stopOrphanReentryWorkers()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("shutdown returned while a tracked re-entry worker was still running")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not join the tracked re-entry worker")
	}
}
