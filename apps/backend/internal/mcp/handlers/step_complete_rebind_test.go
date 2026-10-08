package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	"github.com/kandev/kandev/internal/task/service"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	ws "github.com/kandev/kandev/pkg/websocket"
)

type rebindSessionSeed struct {
	id      string
	state   models.TaskSessionState
	primary bool
	profile string
}

// seedStepCompleteRebindTask seeds a workspace and task plus an explicit set of
// sessions, so a rebind test can arrange a broken primary (dead or wrong
// profile) next to a live current-step caller. Unlike seedStepCompleteTarget it
// does not assume a single primary session.
func seedStepCompleteRebindTask(t *testing.T, repo *sqliterepo.Repository, taskID, stepID string, seeds ...rebindSessionSeed) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{
		ID: "ws-step-complete", Name: "Step Complete", CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: taskID, WorkspaceID: "ws-step-complete", WorkflowStepID: stepID,
		Title: "Rebind Task", State: v1.TaskStateInProgress, CreatedAt: now, UpdatedAt: now,
	}))
	for _, seed := range seeds {
		require.NoError(t, repo.CreateTaskSession(ctx, &models.TaskSession{
			ID: seed.id, TaskID: taskID, IsPrimary: seed.primary, State: seed.state,
			AgentProfileID: seed.profile, StartedAt: now, UpdatedAt: now,
		}))
	}
}

// newStepCompleteRebindHandler wires the handler with one workflow step row per
// entry in stepProfiles (a step profile of "" exercises the no-constraint case).
func newStepCompleteRebindHandler(
	t *testing.T,
	taskSvc *service.Service,
	repo *sqliterepo.Repository,
	bus *mcpRecordingEventBus,
	stepProfiles map[string]string,
) *Handlers {
	t.Helper()
	ctrl, wfRepo := newTestWorkflowController(t)
	ctx := context.Background()
	for stepID, profile := range stepProfiles {
		require.NoError(t, repo.CreateWorkflow(ctx, &models.Workflow{
			ID: "wf-" + stepID, WorkspaceID: "ws-step-complete", Name: "Workflow " + stepID,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}))
		require.NoError(t, wfRepo.CreateStep(ctx, &wfmodels.WorkflowStep{
			ID: stepID, WorkflowID: "wf-" + stepID, Name: stepID, AgentProfileID: profile,
		}))
	}
	return &Handlers{
		taskSvc: taskSvc, sessionRepo: repo, eventBus: bus, workflowCtrl: ctrl,
		logger: testLogger(t).WithFields(),
	}
}

// recordingStepCompleteRebinder writes the promotion through the real repository
// so a test asserts the caller actually became primary, and can simulate a seam
// failure without touching the repository.
type recordingStepCompleteRebinder struct {
	repo     *sqliterepo.Repository
	promoted []string
	fail     error
}

var _ StepCompleteSessionRebinder = (*recordingStepCompleteRebinder)(nil)

func (r *recordingStepCompleteRebinder) SetPrimarySession(ctx context.Context, sessionID string) error {
	if r.fail != nil {
		return r.fail
	}
	if err := r.repo.SetSessionPrimary(ctx, sessionID); err != nil {
		return err
	}
	r.promoted = append(r.promoted, sessionID)
	return nil
}

func rebindStepCompleteMessage(t *testing.T, taskID, sessionID string) *ws.Message {
	t.Helper()
	return makeWSMessage(t, ws.ActionMCPStepComplete, map[string]interface{}{
		"task_id": taskID, "session_id": sessionID, "summary": "work done",
	})
}

func acceptedStepComplete(t *testing.T, resp *ws.Message) map[string]interface{} {
	t.Helper()
	require.NotNil(t, resp)
	require.Equal(t, ws.MessageTypeResponse, resp.Type)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Payload, &payload))
	return payload
}

// TestHandleStepComplete_RebindsToLiveSessionWhenPrimaryIsDead covers the
// reported failure: the session the task still labels primary is terminal, so a
// live current-step caller must be rebound and its signal accepted.
func TestHandleStepComplete_RebindsToLiveSessionWhenPrimaryIsDead(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestTaskService(t)
	seedStepCompleteRebindTask(t, repo, "task-rebind-dead", "step-rebind",
		rebindSessionSeed{id: "session-dead", state: models.TaskSessionStateFailed, primary: true, profile: "profile-step"},
		rebindSessionSeed{id: "session-live", state: models.TaskSessionStateRunning, primary: false, profile: "profile-step"},
	)
	createStepCompleteRecoveryTurn(t, repo, "turn-live-dead", "task-rebind-dead", "session-live")

	bus := &mcpRecordingEventBus{}
	h := newStepCompleteRebindHandler(t, svc, repo, bus, map[string]string{"step-rebind": "profile-step"})
	rebinder := &recordingStepCompleteRebinder{repo: repo}
	h.SetStepCompleteSessionRebinder(rebinder)

	resp, err := h.handleStepComplete(ctx, rebindStepCompleteMessage(t, "task-rebind-dead", "session-live"))
	require.NoError(t, err)
	payload := acceptedStepComplete(t, resp)
	assert.Equal(t, true, payload["accepted"])
	assert.Equal(t, []string{"session-live"}, rebinder.promoted, "the live caller must be promoted")
	require.Len(t, bus.events, 1, "the rebound signal must publish one event")
	data, ok := bus.events[0].Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "session-live", data["session_id"])

	live, err := repo.GetTaskSession(ctx, "session-live")
	require.NoError(t, err)
	assert.True(t, live.IsPrimary, "the live caller must become the task's primary session")
	_, hasBag := models.LoadPendingStepSignal(live.Metadata)
	assert.True(t, hasBag, "the signal must persist on the rebound session")
}

// TestHandleStepComplete_RebindsWhenPrimaryProfileChanged covers a step whose
// effective profile changed while its designated session kept the old one.
func TestHandleStepComplete_RebindsWhenPrimaryProfileChanged(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestTaskService(t)
	seedStepCompleteRebindTask(t, repo, "task-rebind-profile", "step-rebind",
		rebindSessionSeed{id: "session-old", state: models.TaskSessionStateRunning, primary: true, profile: "profile-old"},
		rebindSessionSeed{id: "session-new", state: models.TaskSessionStateRunning, primary: false, profile: "profile-step"},
	)
	createStepCompleteRecoveryTurn(t, repo, "turn-new-profile", "task-rebind-profile", "session-new")

	bus := &mcpRecordingEventBus{}
	h := newStepCompleteRebindHandler(t, svc, repo, bus, map[string]string{"step-rebind": "profile-step"})
	rebinder := &recordingStepCompleteRebinder{repo: repo}
	h.SetStepCompleteSessionRebinder(rebinder)

	resp, err := h.handleStepComplete(ctx, rebindStepCompleteMessage(t, "task-rebind-profile", "session-new"))
	require.NoError(t, err)
	payload := acceptedStepComplete(t, resp)
	assert.Equal(t, true, payload["accepted"])
	assert.Equal(t, []string{"session-new"}, rebinder.promoted)
	require.Len(t, bus.events, 1)
}

// TestHandleStepComplete_RebindsWhenNoPrimarySession covers a task with no
// primary at all.
func TestHandleStepComplete_RebindsWhenNoPrimarySession(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestTaskService(t)
	seedStepCompleteRebindTask(t, repo, "task-rebind-none", "step-rebind",
		rebindSessionSeed{id: "session-only", state: models.TaskSessionStateRunning, primary: false, profile: "profile-step"},
	)
	createStepCompleteRecoveryTurn(t, repo, "turn-none", "task-rebind-none", "session-only")

	bus := &mcpRecordingEventBus{}
	h := newStepCompleteRebindHandler(t, svc, repo, bus, map[string]string{"step-rebind": "profile-step"})
	rebinder := &recordingStepCompleteRebinder{repo: repo}
	h.SetStepCompleteSessionRebinder(rebinder)

	resp, err := h.handleStepComplete(ctx, rebindStepCompleteMessage(t, "task-rebind-none", "session-only"))
	require.NoError(t, err)
	payload := acceptedStepComplete(t, resp)
	assert.Equal(t, true, payload["accepted"])
	assert.Equal(t, []string{"session-only"}, rebinder.promoted)
}

// TestHandleStepComplete_RejectsSideConversationWithViablePrimary preserves the
// fork guard from PR #137: a same-profile session that is not the task's current
// session must not advance the workflow while the designated session is viable.
func TestHandleStepComplete_RejectsSideConversationWithViablePrimary(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestTaskService(t)
	seedStepCompleteRebindTask(t, repo, "task-rebind-side", "step-rebind",
		rebindSessionSeed{id: "session-primary", state: models.TaskSessionStateRunning, primary: true, profile: "profile-step"},
		rebindSessionSeed{id: "session-side", state: models.TaskSessionStateRunning, primary: false, profile: "profile-step"},
	)
	createStepCompleteRecoveryTurn(t, repo, "turn-side", "task-rebind-side", "session-side")

	bus := &mcpRecordingEventBus{}
	h := newStepCompleteRebindHandler(t, svc, repo, bus, map[string]string{"step-rebind": "profile-step"})
	rebinder := &recordingStepCompleteRebinder{repo: repo}
	h.SetStepCompleteSessionRebinder(rebinder)

	resp, err := h.handleStepComplete(ctx, rebindStepCompleteMessage(t, "task-rebind-side", "session-side"))
	require.NoError(t, err)
	assertWSError(t, resp, ws.ErrorCodeValidation)
	assert.Empty(t, rebinder.promoted, "a viable primary must block the rebind")
	assert.Empty(t, bus.events, "a rejected signal must not publish")
	side, err := repo.GetTaskSession(ctx, "session-side")
	require.NoError(t, err)
	_, hasBag := models.LoadPendingStepSignal(side.Metadata)
	assert.False(t, hasBag)
}

// TestHandleStepComplete_RejectsPrimaryWithMismatchedProfile preserves the
// profile gate for a leftover primary from a failed profile switch: the caller
// is already primary, so it is not rebound to itself.
func TestHandleStepComplete_RejectsPrimaryWithMismatchedProfile(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestTaskService(t)
	seedStepCompleteRebindTask(t, repo, "task-rebind-leftover", "step-rebind",
		rebindSessionSeed{id: "session-leftover", state: models.TaskSessionStateRunning, primary: true, profile: "profile-old"},
	)
	createStepCompleteRecoveryTurn(t, repo, "turn-leftover", "task-rebind-leftover", "session-leftover")

	bus := &mcpRecordingEventBus{}
	h := newStepCompleteRebindHandler(t, svc, repo, bus, map[string]string{"step-rebind": "profile-step"})
	rebinder := &recordingStepCompleteRebinder{repo: repo}
	h.SetStepCompleteSessionRebinder(rebinder)

	resp, err := h.handleStepComplete(ctx, rebindStepCompleteMessage(t, "task-rebind-leftover", "session-leftover"))
	require.NoError(t, err)
	assertWSError(t, resp, ws.ErrorCodeValidation)
	assert.Empty(t, rebinder.promoted)
	assert.Empty(t, bus.events)
}

// TestHandleStepComplete_RejectsWhenRebinderUnavailable covers the fail-closed
// path: with no rebind seam wired, a broken binding is refused with a clear
// reason instead of being silently accepted or left waiting.
func TestHandleStepComplete_RejectsWhenRebinderUnavailable(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestTaskService(t)
	seedStepCompleteRebindTask(t, repo, "task-rebind-noseam", "step-rebind",
		rebindSessionSeed{id: "session-dead", state: models.TaskSessionStateFailed, primary: true, profile: "profile-step"},
		rebindSessionSeed{id: "session-live", state: models.TaskSessionStateRunning, primary: false, profile: "profile-step"},
	)
	createStepCompleteRecoveryTurn(t, repo, "turn-noseam", "task-rebind-noseam", "session-live")

	bus := &mcpRecordingEventBus{}
	h := newStepCompleteRebindHandler(t, svc, repo, bus, map[string]string{"step-rebind": "profile-step"})

	resp, err := h.handleStepComplete(ctx, rebindStepCompleteMessage(t, "task-rebind-noseam", "session-live"))
	require.NoError(t, err)
	assertWSError(t, resp, ws.ErrorCodeValidation)
	var payload ws.ErrorPayload
	require.NoError(t, json.Unmarshal(resp.Payload, &payload))
	assert.Contains(t, payload.Message, "Automatic rebinding is not available")
	assert.Empty(t, bus.events)
}

// TestHandleStepComplete_RejectsWhenRebindFails covers a promotion error: the
// signal must not be recorded and the caller gets an internal error.
func TestHandleStepComplete_RejectsWhenRebindFails(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestTaskService(t)
	seedStepCompleteRebindTask(t, repo, "task-rebind-fail", "step-rebind",
		rebindSessionSeed{id: "session-dead", state: models.TaskSessionStateFailed, primary: true, profile: "profile-step"},
		rebindSessionSeed{id: "session-live", state: models.TaskSessionStateRunning, primary: false, profile: "profile-step"},
	)
	createStepCompleteRecoveryTurn(t, repo, "turn-fail", "task-rebind-fail", "session-live")

	bus := &mcpRecordingEventBus{}
	h := newStepCompleteRebindHandler(t, svc, repo, bus, map[string]string{"step-rebind": "profile-step"})
	h.SetStepCompleteSessionRebinder(&recordingStepCompleteRebinder{repo: repo, fail: errors.New("promotion refused")})

	resp, err := h.handleStepComplete(ctx, rebindStepCompleteMessage(t, "task-rebind-fail", "session-live"))
	require.NoError(t, err)
	assertWSError(t, resp, ws.ErrorCodeInternalError)
	assert.Empty(t, bus.events)
	live, err := repo.GetTaskSession(ctx, "session-live")
	require.NoError(t, err)
	_, hasBag := models.LoadPendingStepSignal(live.Metadata)
	assert.False(t, hasBag, "a failed rebind must not persist the signal")
}

// TestHandleStepComplete_RejectsInactiveCaller covers a non-active (idle) caller
// that must not be rebound.
func TestHandleStepComplete_RejectsInactiveCaller(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestTaskService(t)
	seedStepCompleteRebindTask(t, repo, "task-rebind-idle", "step-rebind",
		rebindSessionSeed{id: "session-dead", state: models.TaskSessionStateFailed, primary: true, profile: "profile-step"},
		rebindSessionSeed{id: "session-idle", state: models.TaskSessionStateIdle, primary: false, profile: "profile-step"},
	)
	createStepCompleteRecoveryTurn(t, repo, "turn-idle", "task-rebind-idle", "session-idle")

	bus := &mcpRecordingEventBus{}
	h := newStepCompleteRebindHandler(t, svc, repo, bus, map[string]string{"step-rebind": "profile-step"})
	rebinder := &recordingStepCompleteRebinder{repo: repo}
	h.SetStepCompleteSessionRebinder(rebinder)

	resp, err := h.handleStepComplete(ctx, rebindStepCompleteMessage(t, "task-rebind-idle", "session-idle"))
	require.NoError(t, err)
	assertWSError(t, resp, ws.ErrorCodeValidation)
	assert.Empty(t, rebinder.promoted)
}

// TestHandleStepComplete_DoesNotRebindStaleTurn covers a caller whose turn
// started in an older step: the stale-turn guard rejects before any rebind is
// attempted.
func TestHandleStepComplete_DoesNotRebindStaleTurn(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestTaskService(t)
	seedStepCompleteRebindTask(t, repo, "task-rebind-stale", "step-old",
		rebindSessionSeed{id: "session-dead", state: models.TaskSessionStateFailed, primary: true, profile: "profile-step"},
		rebindSessionSeed{id: "session-live", state: models.TaskSessionStateRunning, primary: false, profile: "profile-step"},
	)
	createStepCompleteRecoveryTurn(t, repo, "turn-stale", "task-rebind-stale", "session-live")

	task, err := repo.GetTask(ctx, "task-rebind-stale")
	require.NoError(t, err)
	task.WorkflowStepID = "step-new"
	require.NoError(t, repo.UpdateTask(ctx, task))

	bus := &mcpRecordingEventBus{}
	h := newStepCompleteRebindHandler(t, svc, repo, bus, map[string]string{
		"step-old": "profile-step", "step-new": "profile-step",
	})
	rebinder := &recordingStepCompleteRebinder{repo: repo}
	h.SetStepCompleteSessionRebinder(rebinder)

	resp, err := h.handleStepComplete(ctx, rebindStepCompleteMessage(t, "task-rebind-stale", "session-live"))
	require.NoError(t, err)
	assertWSError(t, resp, ws.ErrorCodeValidation)
	var payload ws.ErrorPayload
	require.NoError(t, json.Unmarshal(resp.Payload, &payload))
	assert.Contains(t, payload.Message, "workflow step changed before signal was recorded")
	assert.Empty(t, rebinder.promoted, "a stale turn must never be rebound")
}

// TestHandleStepComplete_AcceptsOwningPrimaryWithoutRebind covers the idempotent
// case: a caller that already owns the step is accepted through the ownership
// check, so the primary binding is never touched.
func TestHandleStepComplete_AcceptsOwningPrimaryWithoutRebind(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestTaskService(t)
	seedStepCompleteRebindTask(t, repo, "task-rebind-owner", "step-rebind",
		rebindSessionSeed{id: "session-owner", state: models.TaskSessionStateRunning, primary: true, profile: "profile-step"},
	)
	createStepCompleteRecoveryTurn(t, repo, "turn-owner", "task-rebind-owner", "session-owner")

	bus := &mcpRecordingEventBus{}
	h := newStepCompleteRebindHandler(t, svc, repo, bus, map[string]string{"step-rebind": "profile-step"})
	rebinder := &recordingStepCompleteRebinder{repo: repo}
	h.SetStepCompleteSessionRebinder(rebinder)

	resp, err := h.handleStepComplete(ctx, rebindStepCompleteMessage(t, "task-rebind-owner", "session-owner"))
	require.NoError(t, err)
	payload := acceptedStepComplete(t, resp)
	assert.Equal(t, true, payload["accepted"])
	assert.Empty(t, rebinder.promoted, "an owning session must not be re-promoted")
	require.Len(t, bus.events, 1)
}
