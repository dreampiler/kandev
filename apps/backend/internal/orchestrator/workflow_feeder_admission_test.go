package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/events/bus"
	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/orchestrator/watcher"
	"github.com/kandev/kandev/internal/task/models"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	"github.com/stretchr/testify/require"
)

// @covers AC-TASKS-WIP-LIMIT-PULL-SYSTEM-001.2
// @covers AC-TASKS-WIP-LIMIT-PULL-SYSTEM-001.4
func TestWorkflowFeederPromotionAdmitsQueuedTaskBeforeStarting(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "occupant", "occupant-session", "work")
	queuedAt := time.Now().UTC()
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "queued", WorkspaceID: "ws1", WorkflowID: "wf1", WorkflowStepID: "feeder",
		Title: "Queued work", State: v1.TaskStateCreated, WIPAdmitted: false,
		QueuedForStepID: "work", QueuedAt: &queuedAt,
		Metadata: map[string]interface{}{
			models.MetaKeyQueuedMoveExitCompleted: true,
			models.MetaKeyAgentProfileID:          "profile1",
			models.MetaKeyDeferredLaunch:          map[string]interface{}{"intent": "start", "agent_profile_id": "profile1", "prompt": "Launch this queued work"},
		},
	}))
	getter := newMockStepGetter()
	getter.steps["feeder"] = &wfmodels.WorkflowStep{ID: "feeder", WorkflowID: "wf1", Position: 0}
	getter.steps["work"] = &wfmodels.WorkflowStep{
		ID: "work", WorkflowID: "wf1", AgentProfileID: "profile1", Position: 1,
		WIPLimit: 1, PullFromStepID: "feeder",
		Events: wfmodels.StepEvents{OnEnter: []wfmodels.OnEnterAction{{Type: wfmodels.OnEnterAutoStartAgent}}},
	}
	getter.steps["next"] = &wfmodels.WorkflowStep{ID: "next", WorkflowID: "wf1", Position: 2}
	tasks := newMockTaskRepo()
	tasks.tasks["queued"] = &v1.Task{ID: "queued", WorkspaceID: "ws1", WorkflowID: "wf1", Title: "Queued work", State: v1.TaskStateCreated}
	launched := make(chan string, 1)
	manager := &mockAgentManager{repoForExecutionLookup: repo,
		launchAgentFunc: func(_ context.Context, request *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
			launched <- request.SessionID
			return &executor.LaunchAgentResponse{AgentExecutionID: "queued-execution"}, nil
		},
	}
	svc := createTestServiceWithScheduler(repo, getter, tasks, manager)
	svc.eventBus = bus.NewMemoryEventBus(testLogger())
	t.Cleanup(svc.eventBus.Close)
	eventWatcher := watcher.NewWatcher(svc.eventBus, watcher.EventHandlers{OnTaskMoved: svc.handleTaskMoved}, "queued-feeder-regression", testLogger())
	require.NoError(t, eventWatcher.Start(ctx))
	t.Cleanup(func() { require.NoError(t, eventWatcher.Stop()) })
	store := newWorkflowStore(repo, getter, manager, svc.publishTaskUpdated, testLogger(), &operationLedger{}, svc.publishTaskMoved)
	require.NoError(t, store.ApplyTransition(ctx, "occupant", "occupant-session", "work", "next", "on_turn_complete"))
	promoted, err := repo.GetTask(ctx, "queued")
	require.NoError(t, err)
	require.True(t, promoted.WIPAdmitted, "feeder arrival must leave the queued band before entry claims its launch")
	require.Empty(t, promoted.QueuedForStepID)
	require.Nil(t, promoted.QueuedAt)
	select {
	case sessionID := <-launched:
		session, err := repo.GetTaskSession(ctx, sessionID)
		require.NoError(t, err)
		require.Equal(t, "queued", session.TaskID)
	case <-time.After(30 * time.Second):
		t.Fatal("admitted feeder entry did not start its deferred launch")
	}
}
