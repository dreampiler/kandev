package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
)

// TestValidateCeilingEntry_BoundPromptEnsureOnRunningDestinationIsUnavailable
// covers KANDEV-W-086: a prompt_ensure (or workflow_step_ensure) deferral that
// carries the workflow-entry binding for the task's current entry must not be
// terminally dropped while its destination session is still starting or
// running. A record waiting for capacity has no observable completion yet, so
// discarding it as superseded would lose the step-entry prompt forever — the
// step would then advance on a later signal without its own turn ever running.
func TestValidateCeilingEntry_BoundPromptEnsureOnRunningDestinationIsUnavailable(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "w086-running", "w086-running-session", models.TaskSessionStateRunning)
	task, err := repo.GetTask(ctx, "w086-running")
	require.NoError(t, err)
	require.NotNil(t, task)

	stepGetter := newMockStepGetter()
	stepGetter.steps["w086-step"] = &wfmodels.WorkflowStep{ID: "w086-step", WorkflowID: "wf1", Name: "Implementation"}
	svc := createTestService(repo, stepGetter, newMockTaskRepo())
	task.WorkflowID = "wf1"
	task.WorkflowStepID = "w086-step"
	require.NoError(t, repo.UpdateTaskPreservingDeferredLaunch(ctx, task))
	task, err = repo.GetTask(ctx, task.ID)
	require.NoError(t, err)

	entryIdentity := svc.workflowEntryIdentity(ctx, task.ID)
	binding := models.CeilingWorkflowEntryBinding{
		WorkflowID:           "wf1",
		DestinationStepID:    "w086-step",
		RouteOperationID:     workflowSessionRouteID(task.ID, "w086-step", entryIdentity, nil, models.WorkflowProfileSessionStartPolicyReuse),
		EntryIdentity:        entryIdentity,
		DestinationSessionID: "w086-running-session",
	}

	deferral := models.CeilingDeferral{
		Kind: models.CeilingLaunchPromptEnsure,
		Payload: map[string]interface{}{
			metaKeySessionID:                    "w086-running-session",
			metaKeyPrompt:                       "implementation entry prompt",
			models.CeilingLaunchEntryBindingKey: ceilingEntryBindingValue(binding),
		},
	}

	disposition, _, validationErr := svc.validateCeilingEntry(ctx, task, deferral)
	require.NoError(t, validationErr)
	require.Equal(t, ceilingEntryUnavailable, disposition,
		"a bound entry prompt whose destination is still running must be kept for a later pass, not dropped as superseded")
}

// TestValidateCeilingEntry_UnboundPromptOnRunningDestinationKeepsLegacyDisposition
// pins the unchanged legacy behavior: without a workflow-entry binding there is
// no entry identity to compare, so a record whose destination is already
// running keeps today's superseded disposition.
func TestValidateCeilingEntry_UnboundPromptOnRunningDestinationKeepsLegacyDisposition(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "w086-legacy", "w086-legacy-session", models.TaskSessionStateRunning)
	task, err := repo.GetTask(ctx, "w086-legacy")
	require.NoError(t, err)
	require.NotNil(t, task)

	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	deferral := models.CeilingDeferral{
		Kind: models.CeilingLaunchPromptEnsure,
		Payload: map[string]interface{}{
			metaKeySessionID: "w086-legacy-session",
			metaKeyPrompt:    "legacy prompt",
		},
	}

	disposition, _, validationErr := svc.validateCeilingEntry(ctx, task, deferral)
	require.NoError(t, validationErr)
	require.Equal(t, ceilingEntrySuperseded, disposition,
		"an unbound record on a running destination keeps the legacy superseded disposition")
}

// TestPrepareEngineTurnCompletion_BlocksAdvanceWhileBoundEntryPromptPending
// covers KANDEV-W-086's second half: a signal-gated step must not advance on
// its completion signal while the workflow entry that brought the task here
// still has a valid queued prompt. Losing the entry prompt and advancing on
// the signal is exactly the "entered the step but the step's turn never ran"
// skip this fix closes.
func TestPrepareEngineTurnCompletion_BlocksAdvanceWhileBoundEntryPromptPending(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "w086-guard", "w086-guard-session", models.TaskSessionStateWaitingForInput)
	require.NoError(t, repo.SetSessionMetadataKey(ctx, "w086-guard-session", models.SessionMetaKeyPendingStepCompletion, models.PendingStepCompletionSignal{
		StepID: "w086-guard-step",
		Source: models.StepCompletionSourceAgent,
	}))

	stepGetter := newMockStepGetter()
	stepGetter.steps["w086-guard-step"] = &wfmodels.WorkflowStep{
		ID:                        "w086-guard-step",
		WorkflowID:                "wf1",
		Name:                      "Implementation",
		AutoAdvanceRequiresSignal: true,
		Events: wfmodels.StepEvents{
			OnTurnComplete: []wfmodels.OnTurnCompleteAction{{Type: wfmodels.OnTurnCompleteMoveToNext}},
		},
	}
	svc := createTestService(repo, stepGetter, newMockTaskRepo())
	task, err := repo.GetTask(ctx, "w086-guard")
	require.NoError(t, err)
	task.WorkflowID = "wf1"
	task.WorkflowStepID = "w086-guard-step"
	require.NoError(t, repo.UpdateTaskPreservingDeferredLaunch(ctx, task))
	task, err = repo.GetTask(ctx, "w086-guard")
	require.NoError(t, err)
	session, err := repo.GetTaskSession(ctx, "w086-guard-session")
	require.NoError(t, err)

	require.True(t, svc.prepareEngineTurnCompletion(ctx, task.ID, task, session, turnCompletionCauseAgentTurn),
		"sanity: a signaled gate with no pending entry prompt must still advance")

	entryIdentity := svc.workflowEntryIdentity(ctx, task.ID)
	binding := models.CeilingWorkflowEntryBinding{
		WorkflowID:           "wf1",
		DestinationStepID:    "w086-guard-step",
		RouteOperationID:     workflowSessionRouteID(task.ID, "w086-guard-step", entryIdentity, nil, models.WorkflowProfileSessionStartPolicyReuse),
		EntryIdentity:        entryIdentity,
		DestinationSessionID: "w086-guard-session",
	}
	deferral := models.CeilingDeferral{
		Kind: models.CeilingLaunchPromptEnsure,
		Payload: map[string]interface{}{
			metaKeySessionID:                    "w086-guard-session",
			metaKeyPrompt:                       "implementation entry prompt",
			models.CeilingLaunchEntryBindingKey: ceilingEntryBindingValue(binding),
		},
	}
	require.NoError(t, repo.SetTaskMetadataKey(ctx, task.ID, models.MetaKeyDeferredLaunch, models.CeilingRecordKeys(deferral)))

	require.False(t, svc.prepareEngineTurnCompletion(ctx, task.ID, task, session, turnCompletionCauseAgentTurn),
		"a valid queued entry prompt for this step must block the signal-gated advance until the prompt is delivered")
	sessionAfter, err := repo.GetTaskSession(ctx, "w086-guard-session")
	require.NoError(t, err)
	require.Equal(t, models.TaskSessionStateWaitingForInput, sessionAfter.State)
}
