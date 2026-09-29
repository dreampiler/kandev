package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/automation"
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
