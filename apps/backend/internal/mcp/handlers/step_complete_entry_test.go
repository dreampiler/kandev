package handlers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	ws "github.com/kandev/kandev/pkg/websocket"
	"github.com/stretchr/testify/require"
)

func TestHandleStepCompletePreLedgerTurnIsAccepted(t *testing.T) {
	ctx := context.Background()
	h, repo, task, bus := stepCompleteEntryFixture(t)
	_, err := repo.DB().ExecContext(ctx, `DELETE FROM task_step_transitions WHERE task_id = ?`, task.ID)
	require.NoError(t, err)
	require.NoError(t, repo.SetSessionMetadataKey(ctx, "session-entry-handler", models.SessionMetaKeyPendingStepCompletion, nil))
	turn := &models.Turn{ID: "turn-pre-ledger", TaskID: task.ID, TaskSessionID: "session-entry-handler"}
	_, err = repo.CreateTurnWithStepStamp(ctx, turn)
	require.NoError(t, err)
	require.Zero(t, models.StepCompletionEntryIDFromAny(turn.Metadata[models.TurnMetaKeyWorkflowStepEntryAtStart]))
	msg := makeWSMessage(t, ws.ActionMCPStepComplete, map[string]interface{}{
		"task_id": task.ID, "session_id": "session-entry-handler", "summary": "legacy work complete",
	})
	resp, err := h.handleStepComplete(ctx, msg)
	require.NoError(t, err)
	require.Equal(t, ws.MessageTypeResponse, resp.Type)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Payload, &payload))
	require.Equal(t, true, payload["accepted"])
	require.Len(t, bus.events, 1)
	entry, err := repo.GetTaskStepCompletionEntry(ctx, task.ID)
	require.NoError(t, err)
	require.Positive(t, entry.TransitionID)
	session, err := repo.GetTaskSession(ctx, "session-entry-handler")
	require.NoError(t, err)
	signal, ok := models.LoadPendingStepSignal(session.Metadata)
	require.True(t, ok)
	require.Equal(t, entry.TransitionID, signal.EntryTransitionID)
}

type stepCompleteClaimReentryRepo struct {
	*sqliterepo.Repository
	beforeClaim func()
}

type stepCompleteUnknownEntryRepo struct{ *sqliterepo.Repository }

func (r *stepCompleteUnknownEntryRepo) GetTaskStepCompletionEntry(context.Context, string) (models.StepCompletionEntry, error) {
	return models.StepCompletionEntry{StepID: "step-a"}, nil
}

func TestHandleStepCompleteDoesNotGuessMissingEntry(t *testing.T) {
	h, repo, task, bus := stepCompleteEntryFixture(t)
	h.sessionRepo = &stepCompleteUnknownEntryRepo{Repository: repo}
	msg := makeWSMessage(t, ws.ActionMCPStepComplete, map[string]interface{}{
		"task_id": task.ID, "session_id": "session-entry-handler", "summary": "unknown entry",
	})
	resp, err := h.handleStepComplete(context.Background(), msg)
	require.NoError(t, err)
	require.Equal(t, ws.MessageTypeError, resp.Type)
	require.Contains(t, string(resp.Payload), "entry could not be established")
	require.Empty(t, bus.events)
}

func (r *stepCompleteClaimReentryRepo) SetSessionMetadataKeyIfAbsentOrDifferentStepIfTaskAtStep(
	ctx context.Context, taskID, sessionID, key, stepID string, value interface{},
) (bool, error) {
	r.beforeClaim()
	return r.Repository.SetSessionMetadataKeyIfAbsentOrDifferentStepIfTaskAtStep(ctx, taskID, sessionID, key, stepID, value)
}

func TestHandleStepCompleteRejectsEntryChangeAtClaim(t *testing.T) {
	ctx := context.Background()
	h, repo, task, bus := stepCompleteEntryFixture(t)
	_, err := repo.CreateTurnWithStepStamp(ctx, &models.Turn{ID: "turn-entry-claim-race", TaskID: task.ID, TaskSessionID: "session-entry-handler"})
	require.NoError(t, err)
	h.sessionRepo = &stepCompleteClaimReentryRepo{Repository: repo, beforeClaim: func() {
		for _, stepID := range []string{"step-b", "step-a"} {
			task.WorkflowStepID = stepID
			require.NoError(t, repo.UpdateTask(ctx, task))
		}
	}}
	msg := makeWSMessage(t, ws.ActionMCPStepComplete, map[string]interface{}{
		"task_id": task.ID, "session_id": "session-entry-handler", "summary": "moved while claiming",
	})
	resp, err := h.handleStepComplete(ctx, msg)
	require.NoError(t, err)
	require.Equal(t, ws.MessageTypeError, resp.Type)
	require.Contains(t, string(resp.Payload), "workflow entry changed")
	require.Empty(t, bus.events)
}

func TestStepCompleteLegacyTurnRequiresEstablishedEntry(t *testing.T) {
	entry := models.StepCompletionEntry{StepID: "step-a", TransitionID: 42, OccurredAt: time.Now().UTC()}
	for _, tc := range []struct {
		name    string
		started time.Time
		want    error
	}{
		{"older", entry.OccurredAt.Add(-time.Second), models.ErrStepCompletionEntryChanged},
		{"current", entry.OccurredAt.Add(time.Second), nil},
		{"ambiguous", time.Time{}, models.ErrStepCompletionEntryUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			turn := &models.Turn{StartedAt: tc.started, Metadata: map[string]interface{}{models.TurnMetaKeyWorkflowStepIDAtStart: entry.StepID}}
			_, err := stepCompletionEntryForTurn(turn, entry)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func stepCompleteEntryFixture(t *testing.T) (*Handlers, *sqliterepo.Repository, *models.Task, *mcpRecordingEventBus) {
	t.Helper()
	ctx := context.Background()
	svc, repo := newTestTaskService(t)
	seedStepCompleteTarget(t, repo, "task-entry-handler", "session-entry-handler", "step-a", models.TaskSessionStateRunning)
	bus := &mcpRecordingEventBus{}
	h := newStepCompleteHandler(t, svc, repo, bus)
	ctrl, wfRepo := newTestWorkflowController(t)
	require.NoError(t, repo.CreateWorkflow(ctx, &models.Workflow{ID: "wf-entry-handler", WorkspaceID: "ws-step-complete", Name: "Entry handler"}))
	for i, id := range []string{"step-a", "step-b"} {
		require.NoError(t, wfRepo.CreateStep(ctx, &wfmodels.WorkflowStep{ID: id, WorkflowID: "wf-entry-handler", Name: id, Position: i}))
	}
	h.workflowCtrl = ctrl
	task, err := repo.GetTask(ctx, "task-entry-handler")
	require.NoError(t, err)
	task.WorkflowID = "wf-entry-handler"
	require.NoError(t, repo.UpdateTask(ctx, task))
	_, err = repo.CreateTurnWithStepStamp(ctx, &models.Turn{ID: "turn-entry-old", TaskID: task.ID, TaskSessionID: "session-entry-handler"})
	require.NoError(t, err)
	entryID, err := repo.GetLatestTaskStepTransitionID(ctx, task.ID)
	require.NoError(t, err)
	require.NoError(t, repo.SetSessionMetadataKey(ctx, "session-entry-handler", models.SessionMetaKeyPendingStepCompletion, models.PendingStepCompletionSignal{
		StepID: "step-a", EntryTransitionID: entryID, Source: models.StepCompletionSourceAgent, Summary: "old entry", SignaledAt: time.Now().UTC(),
	}))
	for _, id := range []string{"step-b", "step-a"} {
		task.WorkflowStepID = id
		require.NoError(t, repo.UpdateTask(ctx, task))
	}
	return h, repo, task, bus
}

func TestHandleStepCompleteFreshEntryClaimsAfterSameStepReturn(t *testing.T) {
	ctx := context.Background()
	h, repo, task, bus := stepCompleteEntryFixture(t)
	_, err := repo.CreateTurnWithStepStamp(ctx, &models.Turn{ID: "turn-entry-new", TaskID: task.ID, TaskSessionID: "session-entry-handler"})
	require.NoError(t, err)
	msg := makeWSMessage(t, ws.ActionMCPStepComplete, map[string]interface{}{
		"task_id": task.ID, "session_id": "session-entry-handler", "summary": "new entry complete",
	})
	resp, err := h.handleStepComplete(ctx, msg)
	require.NoError(t, err)
	require.Equal(t, ws.MessageTypeResponse, resp.Type)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Payload, &payload))
	require.Equal(t, true, payload["accepted"], "a previous entry must not cause already_signaled")
	require.Len(t, bus.events, 1)
	session, err := repo.GetTaskSession(ctx, "session-entry-handler")
	require.NoError(t, err)
	signal, ok := models.LoadPendingStepSignal(session.Metadata)
	require.True(t, ok)
	currentID, err := repo.GetLatestTaskStepTransitionID(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, currentID, signal.EntryTransitionID)
	require.Equal(t, "new entry complete", signal.Summary)
}

func TestHandleStepCompleteRejectsOldTurnAfterSameStepReturn(t *testing.T) {
	h, repo, task, bus := stepCompleteEntryFixture(t)
	msg := makeWSMessage(t, ws.ActionMCPStepComplete, map[string]interface{}{
		"task_id": task.ID, "session_id": "session-entry-handler", "summary": "old caller",
	})
	resp, err := h.handleStepComplete(context.Background(), msg)
	require.NoError(t, err)
	require.Equal(t, ws.MessageTypeError, resp.Type)
	require.Contains(t, string(resp.Payload), "workflow entry changed")
	require.Empty(t, bus.events)
	session, err := repo.GetTaskSession(context.Background(), "session-entry-handler")
	require.NoError(t, err)
	rejection, recorded := models.LoadStepCompleteRejection(session.Metadata)
	require.True(t, recorded)
	require.Contains(t, rejection.Reason, "workflow entry changed")
}
