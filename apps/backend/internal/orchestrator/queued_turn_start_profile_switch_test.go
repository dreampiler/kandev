package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	"github.com/stretchr/testify/require"
)

// TestQueuedTurnStartProfileSwitchParksSourceUnderClaimGuard drains a queued
// prompt whose on_turn_start moves the task to a step owned by another agent
// profile. The prompt claim holds the source session's dispatch guard while
// the after-claim callback runs on_turn_start, so parking the source session
// for the profile switch must reuse that ownership instead of acquiring the
// non-reentrant guard again.
func TestQueuedTurnStartProfileSwitchParksSourceUnderClaimGuard(t *testing.T) {
	for _, useEngine := range []bool{false, true} {
		name := "legacy"
		if useEngine {
			name = "engine"
		}
		t.Run(name, func(t *testing.T) {
			testQueuedTurnStartProfileSwitchParksSourceUnderClaimGuard(t, useEngine)
		})
	}
}

func testQueuedTurnStartProfileSwitchParksSourceUnderClaimGuard(t *testing.T, useEngine bool) {
	t.Helper()
	ctx := context.Background()
	fixture := newProfileSwitchFixture(
		t,
		models.WorkflowProfileSessionStartPolicyReuse,
		models.WorkflowProfileSessionEndPolicyPark,
	)
	source, err := fixture.repo.GetTaskSession(ctx, fixture.current.ID)
	require.NoError(t, err)
	source.State = models.TaskSessionStateWaitingForInput
	require.NoError(t, fixture.repo.UpdateTaskSession(ctx, source))

	fixture.stepGetter.steps["step-a"] = &wfmodels.WorkflowStep{
		ID: "step-a", WorkflowID: "wf1", Name: "Implement", Position: 0,
		AgentProfileID:            "profile-a",
		ProfileSessionStartPolicy: fixture.startPolicy,
		ProfileSessionEndPolicy:   fixture.endPolicy,
		Events: wfmodels.StepEvents{
			OnTurnStart: []wfmodels.OnTurnStartAction{{Type: wfmodels.OnTurnStartMoveToNext}},
		},
	}
	fixture.stepGetter.steps["step-b"] = &wfmodels.WorkflowStep{
		ID: "step-b", WorkflowID: "wf1", Name: "Review", Position: 1,
		AgentProfileID:            "profile-b",
		ProfileSessionStartPolicy: fixture.startPolicy,
		ProfileSessionEndPolicy:   fixture.endPolicy,
	}
	if useEngine {
		fixture.svc.SetWorkflowStepGetter(fixture.stepGetter)
	} else {
		fixture.svc.workflowEngine = nil
	}
	fixture.agentMgr.isAgentRunning = true

	_, err = fixture.svc.messageQueue.QueueMessageWithMetadata(
		ctx, source.ID, "t1", "follow-up after the previous turn", "",
		messagequeue.QueuedByUser, false, nil, nil,
	)
	require.NoError(t, err)

	require.True(t, fixture.svc.drainQueuedMessageForPromptableSession(ctx, source.ID),
		"the waiting source session must accept the queued prompt")
	// The drain hands the claimed prompt to an asynchronous worker. Wait for
	// that worker to apply on_turn_start and release its dispatch marker.
	require.Eventually(t, func() bool {
		task, err := fixture.repo.GetTask(ctx, "t1")
		return err == nil && task.WorkflowStepID == "step-b" &&
			!fixture.svc.isQueuedDispatchInFlight(source.ID)
	}, 10*time.Second, 10*time.Millisecond,
		"queued dispatch never moved the task to the destination step: profile-switch parking blocked on the claim guard it already owns")

	parked, err := fixture.repo.GetTaskSession(ctx, source.ID)
	require.NoError(t, err)
	require.False(t, parked.IsPrimary, "the source profile session must be parked behind the destination session")
}
