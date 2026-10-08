package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/automation"
	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// autoStartOwnedStep is the destination step a feeder pull promotes a task
// into: it carries on_enter auto_start_agent, which is exactly the trigger
// that used to race the automation's own start.
func autoStartOwnedStep() *wfmodels.WorkflowStep {
	return &wfmodels.WorkflowStep{
		ID: "step-work", WorkflowID: "wf1", Name: "Work", Position: 0,
		Events: wfmodels.StepEvents{
			OnEnter: []wfmodels.OnEnterAction{{Type: wfmodels.OnEnterAutoStartAgent}},
		},
	}
}

func seedAutoStartOwnedTask(t *testing.T, taskID string, metadata map[string]interface{}) *sqliterepo.Repository {
	t.Helper()
	ctx := context.Background()
	repo := setupTestRepo(t)
	now := time.Now().UTC()
	requireNoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws1", Name: "Test", CreatedAt: now, UpdatedAt: now}))
	requireNoError(t, repo.CreateWorkflow(ctx, &models.Workflow{ID: "wf1", WorkspaceID: "ws1", Name: "WF", CreatedAt: now, UpdatedAt: now}))
	requireNoError(t, repo.CreateTask(ctx, &models.Task{
		ID:             taskID,
		WorkspaceID:    "ws1",
		WorkflowID:     "wf1",
		WorkflowStepID: "step-work",
		Title:          "Automation run",
		Description:    "prompt",
		State:          v1.TaskStateCreated,
		Metadata:       metadata,
		CreatedAt:      now,
		UpdatedAt:      now,
	}))
	return repo
}

// TestAutoStartTaskForLoadedStepHonorsAutomationStartOwnership pins the
// single-start-owner fix for the feeder double-start: a task whose initial
// start is owned by the automation run that created it must not be launched by
// the workflow auto-start chokepoint, while an otherwise identical task
// without the ownership marker still launches.
//
// @covers AC-AGENTS-SESSION-CEILING-001.10
func TestAutoStartTaskForLoadedStepHonorsAutomationStartOwnership(t *testing.T) {
	ctx := context.Background()
	step := autoStartOwnedStep()

	t.Run("skips a task whose start is owned by its automation run", func(t *testing.T) {
		repo := seedAutoStartOwnedTask(t, "t-owned", map[string]interface{}{
			models.MetaKeyAutomationStartOwned: true,
			models.MetaKeyAgentProfileID:       "automation-profile",
		})
		stepGetter := newMockStepGetter()
		stepGetter.steps[step.ID] = step
		svc := createTestServiceWithScheduler(repo, stepGetter, newMockTaskRepo(), failIfLaunched(t))

		task, err := repo.GetTask(ctx, "t-owned")
		requireNoError(t, err)
		svc.autoStartTaskForLoadedStep(ctx, task, step, "task.moved", false, 0, false, false)

		sessions, err := repo.ListTaskSessions(ctx, "t-owned")
		requireNoError(t, err)
		if len(sessions) != 0 {
			t.Fatalf("automation-owned task auto-started %d session(s), want 0", len(sessions))
		}
	})

	t.Run("control: launches a task without the ownership marker", func(t *testing.T) {
		repo := seedAutoStartOwnedTask(t, "t-plain", map[string]interface{}{
			models.MetaKeyAgentProfileID: "automation-profile",
		})
		stepGetter := newMockStepGetter()
		stepGetter.steps[step.ID] = step
		taskRepo := newMockTaskRepo()
		taskRepo.tasks["t-plain"] = &v1.Task{
			ID: "t-plain", WorkspaceID: "ws1", WorkflowID: "wf1",
			Description: "prompt", State: v1.TaskStateCreated,
			Metadata: map[string]interface{}{models.MetaKeyAgentProfileID: "automation-profile"},
		}
		launched := make(chan struct{}, 1)
		agentMgr := &mockAgentManager{
			launchAgentFunc: func(context.Context, *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
				launched <- struct{}{}
				return &executor.LaunchAgentResponse{AgentExecutionID: "exec-1"}, nil
			},
		}
		svc := createTestServiceWithScheduler(repo, stepGetter, taskRepo, agentMgr)

		task, err := repo.GetTask(ctx, "t-plain")
		requireNoError(t, err)
		svc.autoStartTaskForLoadedStep(ctx, task, step, "task.moved", false, 0, false, false)

		select {
		case <-launched:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for the control task to auto-start")
		}
	})
}

// TestCreateAutomationTaskStampsStartOwnership pins the create-time half of
// the fix: the automation must mark the task it creates as automation-owned
// before CreateTask runs the feeder pull, so the pull's task.moved cannot
// launch it first.
//
// @covers AC-AGENTS-SESSION-CEILING-001.10
func TestCreateAutomationTaskStampsStartOwnership(t *testing.T) {
	repo := setupTestRepo(t)
	creator := &stubReviewTaskCreator{task: &models.Task{ID: "t-created"}}
	autoSvc := &stubAutomationService{automation: &automation.Automation{
		ID: "a-1", WorkspaceID: "ws-1", Name: "nightly sweep", Prompt: "sweep",
		WorkflowID: "wf-1", WorkflowStepID: "step-1", Enabled: true,
	}}

	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	svc.SetAutomationService(autoSvc)
	svc.reviewTaskCreator = creator
	seedAutomationWorkspaceRepo(t, repo, "ws-1")

	svc.createAutomationTask(context.Background(), &automation.AutomationTriggeredEvent{
		AutomationID: "a-1", TriggerID: "trg-1", TriggerType: automation.TriggerTypeScheduled,
	})

	requireNotNil(t, creator.got, "expected the automation to create its task")
	if creator.got.Metadata[models.MetaKeyAutomationStartOwned] != true {
		t.Fatalf("automation task metadata %v is missing %s=true",
			creator.got.Metadata, models.MetaKeyAutomationStartOwned)
	}
}

func requireNotNil(t *testing.T, value interface{}, msg string) {
	t.Helper()
	if value == nil {
		t.Fatal(msg)
	}
}
