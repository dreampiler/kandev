package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/automation"
	"github.com/kandev/kandev/internal/task/models"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// TestDiscardDeferredCeilingLaunch_ReleasesTheHoldAndFailsTheRun proves the
// escape hatch: a task whose automatic start is stuck deferred by the session
// ceiling can have that record discarded on demand. Discarding fails the
// automation run the launch served (nothing will ever launch for it) and clears
// the durable record so later automatic launches stop being refused.
func TestDiscardDeferredCeilingLaunch_ReleasesTheHoldAndFailsTheRun(t *testing.T) {
	ctx := context.Background()
	f := deferAutomationStartAtCeiling(t, "t-manual-release")

	discarded, err := f.svc.DiscardDeferredCeilingLaunch(ctx, f.taskID)
	require.NoError(t, err)
	require.True(t, discarded, "a present record must be reported as cleared")
	require.False(t, f.ceilingDeferred(t), "the durable record must be gone")
	require.Equal(t, automation.RunStatusFailed, f.run(t).Status,
		"the release fails the run whose start can no longer launch")
	require.Zero(t, f.activeRuns(t), "the failed run releases its max_concurrent_runs slot")
	require.Zero(t, f.launches, "the release must not launch")
}

// TestDiscardDeferredCeilingLaunch_IsIdempotent proves a retried release of a
// task that holds no ceiling record is a no-op, so an operator can safely repeat
// the call.
func TestDiscardDeferredCeilingLaunch_IsIdempotent(t *testing.T) {
	ctx := context.Background()
	f := deferAutomationStartAtCeiling(t, "t-manual-release-idempotent")

	first, err := f.svc.DiscardDeferredCeilingLaunch(ctx, f.taskID)
	require.NoError(t, err)
	require.True(t, first)

	second, err := f.svc.DiscardDeferredCeilingLaunch(ctx, f.taskID)
	require.NoError(t, err)
	require.False(t, second, "a task with no ceiling record is a no-op")
}

// TestDiscardDeferredCeilingLaunch_NoRecordIsNoOp covers the ordinary task that
// has never deferred a launch.
func TestDiscardDeferredCeilingLaunch_NoRecordIsNoOp(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "t-manual-release-none", "s-manual-release-none", models.TaskSessionStateWaitingForInput)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, "t-manual-release-none", v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, &mockAgentManager{})

	discarded, err := svc.DiscardDeferredCeilingLaunch(ctx, "t-manual-release-none")
	require.NoError(t, err)
	require.False(t, discarded)
}

// TestDropCeilingStartOnNonAutoStartMove_ClearsQueuedStart proves the move-time
// cleanup: a task moved into a step that cannot run an agent has its queued
// ceiling "start" cleared immediately, without waiting for the next sweep, so
// the stale record cannot rank as the queue head when the task returns to an
// agent step.
func TestDropCeilingStartOnNonAutoStartMove_ClearsQueuedStart(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskWithoutSession(t, repo, "t-move-hold", "step-hold")
	deferral := models.CeilingDeferral{
		Kind:       models.CeilingLaunchStart,
		Payload:    map[string]interface{}{metaKeyPrompt: "queued start"},
		Origin:     string(launchOriginAutomatic),
		ReasonCode: ceilingReasonRefused,
		QueuedAt:   time.Now().UTC(),
	}
	require.NoError(t, repo.SetTaskMetadataKey(ctx, "t-move-hold", models.MetaKeyDeferredLaunch, models.CeilingRecordKeys(deferral)))
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	task, err := repo.GetTask(ctx, "t-move-hold")
	require.NoError(t, err)
	hold := &wfmodels.WorkflowStep{ID: "step-hold", WorkflowID: "wf1"}

	svc.dropCeilingStartOnNonAutoStartMove(ctx, "t-move-hold", task, hold)

	updated, err := repo.GetTask(ctx, "t-move-hold")
	require.NoError(t, err)
	require.False(t, models.HasCeilingDeferredIntent(updated), "the queued start must be gone after the move")
}

// TestDropCeilingStartOnNonAutoStartMove_PreservesWIPIntent proves the cleanup
// removes only the ceiling half: a coexisting WIP-overflow intent survives the
// move-time clear.
func TestDropCeilingStartOnNonAutoStartMove_PreservesWIPIntent(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskWithoutSession(t, repo, "t-move-hold-wip", "step-hold")
	deferral := models.CeilingDeferral{
		Kind:       models.CeilingLaunchStart,
		Payload:    map[string]interface{}{metaKeyPrompt: "queued start"},
		Origin:     string(launchOriginAutomatic),
		ReasonCode: ceilingReasonRefused,
		QueuedAt:   time.Now().UTC(),
	}
	record := models.CeilingRecordKeys(deferral)
	record[models.DeferredLaunchStartWhenUnblockedKey] = true
	require.NoError(t, repo.SetTaskMetadataKey(ctx, "t-move-hold-wip", models.MetaKeyDeferredLaunch, record))
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	task, err := repo.GetTask(ctx, "t-move-hold-wip")
	require.NoError(t, err)
	hold := &wfmodels.WorkflowStep{ID: "step-hold", WorkflowID: "wf1"}

	svc.dropCeilingStartOnNonAutoStartMove(ctx, "t-move-hold-wip", task, hold)

	updated, err := repo.GetTask(ctx, "t-move-hold-wip")
	require.NoError(t, err)
	require.False(t, models.HasCeilingDeferredIntent(updated))
	raw, _ := updated.Metadata[models.MetaKeyDeferredLaunch].(map[string]interface{})
	require.Equal(t, true, raw[models.DeferredLaunchStartWhenUnblockedKey], "the WIP half must survive the ceiling clear")
}

// TestDropCeilingStartOnNonAutoStartMove_LeavesNonStartKinds proves a queued
// resume (a continuation for an existing session) is not cleared by the move:
// only the sessionless "start" kind is terminal from a non-agent step.
func TestDropCeilingStartOnNonAutoStartMove_LeavesNonStartKinds(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "t-move-hold-resume", "resume-session", models.TaskSessionStateCreated)
	deferral := models.CeilingDeferral{
		Kind:       models.CeilingLaunchResume,
		Payload:    map[string]interface{}{metaKeySessionID: "resume-session"},
		Origin:     string(launchOriginAutomatic),
		ReasonCode: ceilingReasonRefused,
		QueuedAt:   time.Now().UTC(),
	}
	require.NoError(t, repo.SetTaskMetadataKey(ctx, "t-move-hold-resume", models.MetaKeyDeferredLaunch, models.CeilingRecordKeys(deferral)))
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	task, err := repo.GetTask(ctx, "t-move-hold-resume")
	require.NoError(t, err)
	hold := &wfmodels.WorkflowStep{ID: "step-hold", WorkflowID: "wf1"}

	svc.dropCeilingStartOnNonAutoStartMove(ctx, "t-move-hold-resume", task, hold)

	updated, err := repo.GetTask(ctx, "t-move-hold-resume")
	require.NoError(t, err)
	require.True(t, models.HasCeilingDeferredIntent(updated), "a non-start record must survive the move-time clear")
}

// TestDropCeilingStartOnNonAutoStartMove_LeavesAutoStartSteps proves an
// auto-start destination is untouched: the sweep still owns the replay.
func TestDropCeilingStartOnNonAutoStartMove_LeavesAutoStartSteps(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskWithoutSession(t, repo, "t-move-agent", "step-agent")
	deferral := models.CeilingDeferral{
		Kind:       models.CeilingLaunchStart,
		Payload:    map[string]interface{}{metaKeyPrompt: "queued start"},
		Origin:     string(launchOriginAutomatic),
		ReasonCode: ceilingReasonRefused,
		QueuedAt:   time.Now().UTC(),
	}
	require.NoError(t, repo.SetTaskMetadataKey(ctx, "t-move-agent", models.MetaKeyDeferredLaunch, models.CeilingRecordKeys(deferral)))
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	task, err := repo.GetTask(ctx, "t-move-agent")
	require.NoError(t, err)
	agent := &wfmodels.WorkflowStep{ID: "step-agent", WorkflowID: "wf1",
		Events: wfmodels.StepEvents{OnEnter: []wfmodels.OnEnterAction{{Type: wfmodels.OnEnterAutoStartAgent}}}}

	svc.dropCeilingStartOnNonAutoStartMove(ctx, "t-move-agent", task, agent)

	updated, err := repo.GetTask(ctx, "t-move-agent")
	require.NoError(t, err)
	require.True(t, models.HasCeilingDeferredIntent(updated), "an auto-start destination must keep the queued start")
}
