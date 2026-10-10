package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	ws "github.com/kandev/kandev/pkg/websocket"
)

// repoBindingResolver resolves the durable step binding straight from the
// repository, standing in for the orchestrator-owned resolver wired in
// production. It exercises the same binding table the handler reads.
type repoBindingResolver struct {
	repo *sqliterepo.Repository
}

var _ StepCompleteSessionBindingResolver = repoBindingResolver{}

func (r repoBindingResolver) BoundWorkflowStepSession(_ context.Context, taskID, workflowStepID string) (string, error) {
	binding, err := r.repo.GetWorkflowSessionBinding(context.Background(), taskID, "step:"+workflowStepID)
	if err != nil || binding == nil {
		return "", err
	}
	return binding.SessionID, nil
}

// seedStepCompleteStepBinding records the durable session bound to a step, the
// same row the orchestrator writes when a step's session is selected or moved.
func seedStepCompleteStepBinding(t *testing.T, repo *sqliterepo.Repository, taskID, stepID, sessionID, profileID string) {
	t.Helper()
	accepted, err := repo.UpsertWorkflowSessionBinding(context.Background(), &models.WorkflowSessionBinding{
		TaskID:         taskID,
		TargetKey:      "step:" + stepID,
		WorkflowID:     "wf-" + stepID,
		AgentProfileID: profileID,
		SessionID:      sessionID,
		OperationID:    "workflow-step-entry-v2:seed",
		UpdatedAt:      time.Now().UTC(),
	})
	require.NoError(t, err)
	require.True(t, accepted, "the seeded step binding must be committed")
}

// TestHandleStepComplete_AcceptsRelaunchedSessionByStepBinding covers the
// reported failure: a restart or substitution moved the task onto a live
// session that is no longer primary and runs a different profile than the step
// configured, but the durable step binding names it as the step's owner, so its
// completion signal must be accepted.
func TestHandleStepComplete_AcceptsRelaunchedSessionByStepBinding(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestTaskService(t)
	seedStepCompleteRebindTask(t, repo, "task-bind-relaunch", "step-bind",
		rebindSessionSeed{id: "session-old", state: models.TaskSessionStateFailed, primary: true, profile: "profile-step"},
		rebindSessionSeed{id: "session-quota", state: models.TaskSessionStateRunning, primary: false, profile: "profile-quota"},
	)
	createStepCompleteRecoveryTurn(t, repo, "turn-relaunch", "task-bind-relaunch", "session-quota")
	seedStepCompleteStepBinding(t, repo, "task-bind-relaunch", "step-bind", "session-quota", "profile-step")

	bus := &mcpRecordingEventBus{}
	h := newStepCompleteRebindHandler(t, svc, repo, bus, map[string]string{"step-bind": "profile-step"})
	h.SetStepCompleteSessionBindingResolver(repoBindingResolver{repo: repo})
	// A rebinder is wired too, so the test also proves the binding path accepts
	// without having to promote the caller.
	h.SetStepCompleteSessionRebinder(&recordingStepCompleteRebinder{repo: repo})

	resp, err := h.handleStepComplete(ctx, rebindStepCompleteMessage(t, "task-bind-relaunch", "session-quota"))
	require.NoError(t, err)
	payload := acceptedStepComplete(t, resp)
	assert.Equal(t, true, payload["accepted"])
	require.Len(t, bus.events, 1, "the bound session's signal must publish one event")
}

// TestHandleStepComplete_RejectsSessionNotBoundToAnotherStepsSession is the
// W-040 regression guard: an implementation session that is not the review
// step's bound session must not advance the review step, even when it is the
// task's primary session, and the rejected signal must not move the binding.
func TestHandleStepComplete_RejectsSessionNotBoundToAnotherStepsSession(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestTaskService(t)
	seedStepCompleteRebindTask(t, repo, "task-bind-other", "step-review",
		rebindSessionSeed{id: "session-impl", state: models.TaskSessionStateRunning, primary: true, profile: "profile-implement"},
		rebindSessionSeed{id: "session-review", state: models.TaskSessionStateRunning, primary: false, profile: "profile-review"},
	)
	createStepCompleteRecoveryTurn(t, repo, "turn-impl", "task-bind-other", "session-impl")
	seedStepCompleteStepBinding(t, repo, "task-bind-other", "step-review", "session-review", "profile-review")

	bus := &mcpRecordingEventBus{}
	h := newStepCompleteRebindHandler(t, svc, repo, bus, map[string]string{"step-review": "profile-review"})
	h.SetStepCompleteSessionBindingResolver(repoBindingResolver{repo: repo})
	h.SetStepCompleteSessionRebinder(&recordingStepCompleteRebinder{repo: repo})

	resp, err := h.handleStepComplete(ctx, rebindStepCompleteMessage(t, "task-bind-other", "session-impl"))
	require.NoError(t, err)
	assertWSError(t, resp, ws.ErrorCodeValidation)
	assert.Empty(t, bus.events, "a rejected signal must not publish")

	binding, err := repo.GetWorkflowSessionBinding(ctx, "task-bind-other", "step:step-review")
	require.NoError(t, err)
	require.NotNil(t, binding)
	assert.Equal(t, "session-review", binding.SessionID, "a rejected signal must not move the binding")
}

// TestHandleStepComplete_AcceptsReuseSessionByStepBinding covers the reuse case:
// a session that continues into the next step and is recorded as that step's
// binding keeps advancing the workflow.
func TestHandleStepComplete_AcceptsReuseSessionByStepBinding(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestTaskService(t)
	seedStepCompleteRebindTask(t, repo, "task-bind-reuse", "step-impl",
		rebindSessionSeed{id: "session-reuse", state: models.TaskSessionStateRunning, primary: true, profile: "profile-step"},
	)
	createStepCompleteRecoveryTurn(t, repo, "turn-reuse", "task-bind-reuse", "session-reuse")
	seedStepCompleteStepBinding(t, repo, "task-bind-reuse", "step-impl", "session-reuse", "profile-step")

	bus := &mcpRecordingEventBus{}
	h := newStepCompleteRebindHandler(t, svc, repo, bus, map[string]string{"step-impl": "profile-step"})
	h.SetStepCompleteSessionBindingResolver(repoBindingResolver{repo: repo})

	resp, err := h.handleStepComplete(ctx, rebindStepCompleteMessage(t, "task-bind-reuse", "session-reuse"))
	require.NoError(t, err)
	payload := acceptedStepComplete(t, resp)
	assert.Equal(t, true, payload["accepted"])
	require.Len(t, bus.events, 1)
}
