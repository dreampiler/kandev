package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/automation"
	"github.com/kandev/kandev/internal/task/models"
)

// The automation's configured parent must reach the review task request so the
// task service can nest the firing's task under it. Empty stays top-level.
func TestCreateAutomationTaskCarriesParentTaskID(t *testing.T) {
	repo := setupTestRepo(t)
	creator := &stubReviewTaskCreator{task: &models.Task{ID: "t-created"}}
	autoSvc := &stubAutomationService{automation: &automation.Automation{
		ID: "a-1", WorkspaceID: "ws-1", Name: "nightly sweep", Prompt: "sweep",
		WorkflowID: "wf-1", WorkflowStepID: "step-1", ParentTaskID: "parent-1", Enabled: true,
	}}

	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	svc.SetAutomationService(autoSvc)
	svc.reviewTaskCreator = creator
	seedAutomationWorkspaceRepo(t, repo, "ws-1")

	svc.createAutomationTask(context.Background(), &automation.AutomationTriggeredEvent{
		AutomationID: "a-1", TriggerID: "trg-1", TriggerType: automation.TriggerTypeScheduled,
	})

	require.NotNil(t, creator.got, "expected the automation to create its task; runs=%+v", autoSvc.runs)
	require.Equal(t, "parent-1", creator.got.ParentID)
}

// A firing with no configured parent must not sneak a parent onto the request.
func TestCreateAutomationTaskWithoutParentStaysTopLevel(t *testing.T) {
	repo := setupTestRepo(t)
	creator := &stubReviewTaskCreator{task: &models.Task{ID: "t-created"}}
	autoSvc := &stubAutomationService{automation: &automation.Automation{
		ID: "a-1", WorkspaceID: "ws-1", Name: "nightly sweep", Prompt: "sweep", Enabled: true,
	}}

	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	svc.SetAutomationService(autoSvc)
	svc.reviewTaskCreator = creator
	seedAutomationWorkspaceRepo(t, repo, "ws-1")

	svc.createAutomationTask(context.Background(), &automation.AutomationTriggeredEvent{
		AutomationID: "a-1", TriggerID: "trg-1", TriggerType: automation.TriggerTypeScheduled,
	})

	require.NotNil(t, creator.got)
	require.Empty(t, creator.got.ParentID)
}

// Retried and continued firings rebuild their automation from the launch
// snapshot; the parent must survive that round trip.
func TestAutomationFromRetrySnapshotPreservesParentTaskID(t *testing.T) {
	restored := automationFromRetrySnapshot(automation.RetryLaunchConfigSnapshot{
		AutomationID: "a-1", WorkspaceID: "ws-1", Name: "nightly",
		WorkflowID: "wf-1", WorkflowStepID: "step-1", ParentTaskID: "parent-1",
	})

	require.Equal(t, "parent-1", restored.ParentTaskID)
}
