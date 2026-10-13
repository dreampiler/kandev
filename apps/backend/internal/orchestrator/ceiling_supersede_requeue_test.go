package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

func supersedeRequeueOldBinding(entryIdentity, stepID string) models.CeilingWorkflowEntryBinding {
	return models.CeilingWorkflowEntryBinding{
		WorkflowID:           "wf1",
		DestinationStepID:    stepID,
		RouteOperationID:     "route-old",
		EntryIdentity:        entryIdentity,
		DestinationSessionID: "session-old",
	}
}

func seedSupersedeRoute(repo taskMetadataKeySetter, taskID, stepID, entryIdentity, operationID, destinationID string) {
	ctx := context.Background()
	_ = repo.SetTaskMetadataKey(ctx, taskID, models.MetaKeyWorkflowSessionRoute, models.WorkflowSessionRoute{
		OperationID:       operationID,
		DestinationStepID: stepID,
		EntryIdentity:     entryIdentity,
		TargetKind:        "new_session",
		DestinationID:     destinationID,
		Phase:             "committed",
	})
}

// A superseded bound record whose entry still owns the current route is
// re-queued against the new destination with its original queue time.
func TestSupersedeRequeue_RetargetsBoundRecordPreservingQueueTime(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSessionWithStep(t, repo, "requeue-task", "session-new", "step-new")
	oldSession := &models.TaskSession{ID: "session-old", TaskID: "requeue-task", State: models.TaskSessionStateCreated}
	require.NoError(t, repo.CreateTaskSession(ctx, oldSession))
	task, err := repo.GetTask(ctx, "requeue-task")
	require.NoError(t, err)
	entryIdentity := (&Service{repo: repo}).workflowEntryIdentity(ctx, task.ID)

	stepGetter := newMockStepGetter()
	stepGetter.steps["step-new"] = &wfmodels.WorkflowStep{ID: "step-new", WorkflowID: "wf1"}
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, task.ID, v1.TaskStateScheduling)
	svc := createTestService(repo, stepGetter, taskRepo)

	seedSupersedeRoute(repo, task.ID, "step-new", entryIdentity, "route-new", "session-new")
	task, err = repo.GetTask(ctx, task.ID)
	require.NoError(t, err)

	queuedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	deferral := models.CeilingDeferral{
		Kind: models.CeilingLaunchStartCreated,
		Payload: map[string]interface{}{
			metaKeySessionID:                    "session-old",
			metaKeyPrompt:                       "entry prompt",
			models.CeilingLaunchEntryBindingKey: ceilingEntryBindingValue(supersedeRequeueOldBinding(entryIdentity, "step-new")),
		},
		Origin:     string(launchOriginAutomatic),
		ReasonCode: ceilingReasonRefused,
		QueuedAt:   queuedAt,
	}
	require.NoError(t, repo.SetTaskMetadataKey(ctx, task.ID, models.MetaKeyDeferredLaunch, models.CeilingRecordKeys(deferral)))

	handled := svc.evaluateCeilingSupersedeRequeue(ctx, task, deferral, "workflow destination route changed")
	require.True(t, handled, "a same-entry destination change must re-queue instead of dropping")

	record := deferredLaunchOf(t, svc, task.ID)
	require.NotNil(t, record)
	stored, err := models.ReadCeilingDeferral(record)
	require.NoError(t, err)
	require.True(t, stored.QueuedAt.Equal(queuedAt), "re-queue must preserve the original queue time, not take a new place in line")
	require.Equal(t, ceilingReasonRetargetedRequeued, stored.ReasonCode)
	require.Equal(t, "session-new", sessionIDFromCeilingPayload(stored))
	binding, present, err := models.ReadCeilingWorkflowEntryBinding(stored.Payload)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, "route-new", binding.RouteOperationID)
	require.Equal(t, "session-new", binding.DestinationSessionID)
}

// A superseded record is a true duplicate when the current destination for the
// same entry is already STARTING: it must drop, not launch a second session.
func TestSupersedeRequeue_DropsTrueDuplicateDestinationRunning(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSessionWithStep(t, repo, "dup-task", "session-live", "step-new")
	live, err := repo.GetTaskSession(ctx, "session-live")
	require.NoError(t, err)
	live.State = models.TaskSessionStateStarting
	require.NoError(t, repo.UpdateTaskSession(ctx, live))
	oldSession := &models.TaskSession{ID: "session-old", TaskID: "dup-task", State: models.TaskSessionStateCreated}
	require.NoError(t, repo.CreateTaskSession(ctx, oldSession))
	task, err := repo.GetTask(ctx, "dup-task")
	require.NoError(t, err)
	entryIdentity := (&Service{repo: repo}).workflowEntryIdentity(ctx, task.ID)

	stepGetter := newMockStepGetter()
	stepGetter.steps["step-new"] = &wfmodels.WorkflowStep{ID: "step-new", WorkflowID: "wf1"}
	svc := createTestService(repo, stepGetter, newMockTaskRepo())

	seedSupersedeRoute(repo, task.ID, "step-new", entryIdentity, "route-new", "session-live")
	task, err = repo.GetTask(ctx, task.ID)
	require.NoError(t, err)

	oldBinding := supersedeRequeueOldBinding(entryIdentity, "step-new")
	oldBinding.RouteOperationID = "route-new"
	oldBinding.DestinationSessionID = "session-live"
	deferral := models.CeilingDeferral{
		Kind: models.CeilingLaunchStartCreated,
		Payload: map[string]interface{}{
			metaKeySessionID:                    "session-live",
			models.CeilingLaunchEntryBindingKey: ceilingEntryBindingValue(oldBinding),
		},
		Origin:     string(launchOriginAutomatic),
		ReasonCode: ceilingReasonRefused,
		QueuedAt:   time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
	}
	require.NoError(t, repo.SetTaskMetadataKey(ctx, task.ID, models.MetaKeyDeferredLaunch, models.CeilingRecordKeys(deferral)))

	handled := svc.evaluateCeilingSupersedeRequeue(ctx, task, deferral, "workflow destination route changed")
	require.False(t, handled, "a destination already STARTING for the same entry is a true duplicate and must drop")
}

// A superseded bound record whose old destination terminalized retargets onto
// the route's newer destination instead of dropping.
func TestSupersedeRequeue_RebindsTerminalDestinationToNewRoute(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSessionWithStep(t, repo, "terminal-task", "session-new", "step-new")
	oldSession := &models.TaskSession{ID: "session-old", TaskID: "terminal-task", State: models.TaskSessionStateCancelled}
	require.NoError(t, repo.CreateTaskSession(ctx, oldSession))
	task, err := repo.GetTask(ctx, "terminal-task")
	require.NoError(t, err)
	entryIdentity := (&Service{repo: repo}).workflowEntryIdentity(ctx, task.ID)

	stepGetter := newMockStepGetter()
	stepGetter.steps["step-new"] = &wfmodels.WorkflowStep{ID: "step-new", WorkflowID: "wf1"}
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, task.ID, v1.TaskStateScheduling)
	svc := createTestService(repo, stepGetter, taskRepo)

	seedSupersedeRoute(repo, task.ID, "step-new", entryIdentity, "route-new", "session-new")
	task, err = repo.GetTask(ctx, task.ID)
	require.NoError(t, err)

	deferral := models.CeilingDeferral{
		Kind: models.CeilingLaunchStartCreated,
		Payload: map[string]interface{}{
			metaKeySessionID:                    "session-old",
			models.CeilingLaunchEntryBindingKey: ceilingEntryBindingValue(supersedeRequeueOldBinding(entryIdentity, "step-new")),
		},
		Origin:     string(launchOriginAutomatic),
		ReasonCode: ceilingReasonRefused,
		QueuedAt:   time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second),
	}
	require.NoError(t, repo.SetTaskMetadataKey(ctx, task.ID, models.MetaKeyDeferredLaunch, models.CeilingRecordKeys(deferral)))

	handled := svc.evaluateCeilingSupersedeRequeue(ctx, task, deferral, "workflow destination changed")
	require.True(t, handled, "a terminalized destination must rebind to the route's new destination")

	record := deferredLaunchOf(t, svc, task.ID)
	stored, err := models.ReadCeilingDeferral(record)
	require.NoError(t, err)
	require.Equal(t, "session-new", sessionIDFromCeilingPayload(stored))
}

// A superseded record for a newer entry that replaced the route must not be
// retargeted onto the successor's destination.
func TestSupersedeRequeue_DropsEntryThatMovedOn(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSessionWithStep(t, repo, "moved-task", "session-new", "step-new")
	task, err := repo.GetTask(ctx, "moved-task")
	require.NoError(t, err)

	stepGetter := newMockStepGetter()
	stepGetter.steps["step-new"] = &wfmodels.WorkflowStep{ID: "step-new", WorkflowID: "wf1"}
	svc := createTestService(repo, stepGetter, newMockTaskRepo())

	require.NoError(t, repo.SetTaskMetadataKey(ctx, task.ID, models.MetaKeyWorkflowSessionRoute, models.WorkflowSessionRoute{
		OperationID:       "route-successor",
		DestinationStepID: "step-new",
		EntryIdentity:     "entry:00000000000000000099",
		TargetKind:        "new_session",
		DestinationID:     "session-new",
		Phase:             "committed",
	}))

	deferral := models.CeilingDeferral{
		Kind: models.CeilingLaunchStartCreated,
		Payload: map[string]interface{}{
			metaKeySessionID:                    "session-old",
			models.CeilingLaunchEntryBindingKey: ceilingEntryBindingValue(supersedeRequeueOldBinding("entry:00000000000000000001", "step-new")),
		},
		Origin:     string(launchOriginAutomatic),
		ReasonCode: ceilingReasonRefused,
		QueuedAt:   time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
	}
	require.NoError(t, repo.SetTaskMetadataKey(ctx, task.ID, models.MetaKeyDeferredLaunch, models.CeilingRecordKeys(deferral)))

	handled := svc.evaluateCeilingSupersedeRequeue(ctx, task, deferral, "workflow entry identity changed")
	require.False(t, handled, "an entry a successor replaced must drop, never retarget onto the successor")
}
