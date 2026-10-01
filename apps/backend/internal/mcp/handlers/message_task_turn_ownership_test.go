package handlers

import (
	"context"
	"database/sql"
	"errors"
	"github.com/kandev/kandev/internal/events"
	"github.com/kandev/kandev/internal/events/bus"
	"github.com/kandev/kandev/internal/orchestrator"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestRejectedTaskMessagePreservesExistingNativeTurn(t *testing.T) {
	f := newSelfPlacementFixture(t)
	const sid = "self-placement-session"
	turn, err := f.svc.StartTurn(f.ctx, sid)
	require.NoError(t, err)
	require.NoError(t, f.repo.UpdateTaskSessionState(f.ctx, sid, models.TaskSessionStateRunning, ""))
	session, err := f.repo.GetTaskSession(f.ctx, sid)
	require.NoError(t, err)
	f.h.sessionLauncher = &fakeOrchestrator{promptErrFirst: orchestrator.ErrAgentPromptInProgress}
	_, err = f.h.promptPreparedTaskMessage(f.ctx, f.child.ID, session, "rejected peer message", nil, nil)
	require.ErrorIs(t, err, orchestrator.ErrAgentPromptInProgress)
	active, err := f.svc.GetActiveTurn(f.ctx, sid)
	require.NoError(t, err)
	require.NotNil(t, active, "rejected MCP compensation must preserve the native provider turn")
	require.Equal(t, turn.ID, active.ID)
	messages, err := f.repo.ListMessages(f.ctx, sid)
	require.NoError(t, err)
	require.Empty(t, messages, "only the rejected message must be removed")
	persisted, err := f.repo.GetTaskSession(f.ctx, sid)
	require.NoError(t, err)
	require.Equal(t, models.TaskSessionStateRunning, persisted.State)
}

func TestRejectedTaskMessageCleansOnlyItsUnsentTurn(t *testing.T) {
	for _, scenario := range []string{"own_empty", "adopted_running", "adopted_message", "completed_then_successor"} {
		t.Run(scenario, func(t *testing.T) {
			f := newSelfPlacementFixture(t)
			const sid = "self-placement-session"
			message, owned := f.h.recordUserMessageForPrompt(f.ctx, f.child.ID, sid, "rejected dispatch", nil)
			require.NotNil(t, message)
			require.Equal(t, message.TurnID, owned)
			var successorID string
			switch scenario {
			case "adopted_running":
				require.NoError(t, f.repo.UpdateTaskSessionState(f.ctx, sid, models.TaskSessionStateRunning, ""))
			case "adopted_message":
				_, err := f.svc.CreateMessage(f.ctx, &service.CreateMessageRequest{TaskID: f.child.ID, TaskSessionID: sid, TurnID: owned, Content: "native queue transcript", AuthorType: "user"})
				require.NoError(t, err)
			case "completed_then_successor":
				require.NoError(t, f.svc.CompleteTurn(f.ctx, owned))
				turn, err := f.svc.StartTurn(f.ctx, sid)
				require.NoError(t, err)
				successorID = turn.ID
			}
			f.h.deleteRecordedUserMessage(f.ctx, message, owned)
			turn, err := f.repo.GetTurn(f.ctx, owned)
			if scenario == "own_empty" {
				require.True(t, errors.Is(err, sql.ErrNoRows))
				require.Nil(t, turn)
			} else {
				require.NoError(t, err)
				require.NotNil(t, turn)
			}
			messages, err := f.repo.ListMessages(f.ctx, sid)
			require.NoError(t, err)
			if scenario == "adopted_message" {
				require.Len(t, messages, 1)
				require.Equal(t, "native queue transcript", messages[0].Content)
			} else {
				require.Empty(t, messages)
			}
			if successorID != "" {
				active, err := f.svc.GetActiveTurn(f.ctx, sid)
				require.NoError(t, err)
				require.NotNil(t, active)
				require.Equal(t, successorID, active.ID)
			}
		})
	}
}

type acceptedTaskMessageFailure struct{}

func (*acceptedTaskMessageFailure) Error() string {
	return "prompt accepted but post-dispatch handling failed"
}
func (*acceptedTaskMessageFailure) DetachedResumeAccepted() bool { return true }

func TestAcceptedTaskMessageFailureKeepsTranscriptAndReportsSent(t *testing.T) {
	f := newSelfPlacementFixture(t)
	const sid = "self-placement-session"
	session, err := f.repo.GetTaskSession(f.ctx, sid)
	require.NoError(t, err)
	f.h.sessionLauncher = &fakeOrchestrator{promptErrFirst: &acceptedTaskMessageFailure{}}
	result, err := f.h.promptPreparedTaskMessage(f.ctx, f.child.ID, session, "accepted prompt", nil, nil)
	require.NoError(t, err, "an accepted dispatch must not be reported as a retryable rejection")
	require.Equal(t, taskMessageStatusSent, result.status)
	messages, err := f.repo.ListMessages(f.ctx, sid)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "accepted prompt", messages[0].Content)
	turn, err := f.svc.GetActiveTurn(f.ctx, sid)
	require.NoError(t, err)
	require.NotNil(t, turn)
	require.Equal(t, messages[0].TurnID, turn.ID)
}

func TestRejectedOwnMessageTurnPublishesRemovalWithDBReadback(t *testing.T) {
	ctx := context.Background()
	svc, repo, eventBus := newTestTaskServiceWithEventBus(t)
	workspaces, err := svc.ListWorkspaces(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, workspaces)
	workflows, err := svc.ListWorkflows(ctx, workspaces[0].ID, false)
	require.NoError(t, err)
	require.NotEmpty(t, workflows)
	created, err := svc.CreateTask(ctx, &service.CreateTaskRequest{WorkspaceID: workspaces[0].ID, WorkflowID: workflows[0].ID, Title: "Unsent message turn"})
	require.NoError(t, err)
	const sid = "rejected-own-turn-event-session"
	require.NoError(t, repo.CreateTaskSession(ctx, &models.TaskSession{ID: sid, TaskID: created.Task.ID, State: models.TaskSessionStateWaitingForInput, IsPrimary: true}))
	removed := make(chan *bus.Event, 1)
	_, err = eventBus.Subscribe(events.TurnRemoved, func(_ context.Context, event *bus.Event) error { removed <- event; return nil })
	require.NoError(t, err)
	h := &Handlers{taskSvc: svc, logger: testLogger(t)}
	message, owned := h.recordUserMessageForPrompt(ctx, created.Task.ID, sid, "rejected prompt", nil)
	require.NotNil(t, message)
	require.NotEmpty(t, owned)
	h.deleteRecordedUserMessage(ctx, message, owned)
	select {
	case event := <-removed:
		payload, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		require.Equal(t, owned, payload["id"])
		require.Equal(t, sid, payload["session_id"])
	case <-time.After(3 * time.Second):
		t.Fatal("owned turn removal event not published")
	}
	turn, err := repo.GetTurn(ctx, owned)
	require.ErrorIs(t, err, sql.ErrNoRows)
	require.Nil(t, turn)
	active, err := svc.GetActiveTurn(ctx, sid)
	require.NoError(t, err)
	require.Nil(t, active)
	messages, err := repo.ListMessages(ctx, sid)
	require.NoError(t, err)
	require.Empty(t, messages)
	session, err := repo.GetTaskSession(ctx, sid)
	require.NoError(t, err)
	require.Equal(t, models.TaskSessionStateWaitingForInput, session.State)
}
