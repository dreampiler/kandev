package handlers

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/kandev/kandev/internal/orchestrator"
	"github.com/kandev/kandev/internal/task/models"
	ws "github.com/kandev/kandev/pkg/websocket"
	"github.com/stretchr/testify/require"
)

type messageRecoveryLauncher struct {
	*fakeOrchestrator
	recoveredSession string
	recoveryAccepted bool
}

func (f *messageRecoveryLauncher) AutoRecoverBlockedPrompt(_ context.Context, _, sessionID string, _ error) bool {
	f.recoveredSession = sessionID
	return f.recoveryAccepted
}

func TestTaskMessageRecoveryKeepsRecordedMessageAfterSetPrimary(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestTaskService(t)
	sender, target, session := seedTaskWithSession(t, svc, repo, models.TaskSessionStateWaitingForInput)
	require.NoError(t, repo.SetSessionPrimary(ctx, session.ID))
	h, base := newMessageTaskHandler(t, svc, repo)
	launcher := &messageRecoveryLauncher{fakeOrchestrator: base, recoveryAccepted: true}
	launcher.promptErrFirst = fmt.Errorf("%w: unresolved_durable_work", orchestrator.ErrSessionRecoveryRequired)
	h.sessionLauncher = launcher
	msg := makeWSMessage(t, ws.ActionMCPMessageTask, senderPayload(target.ID, "Continue the interrupted work", sender.ID))
	resp, err := h.handleMessageTask(ctx, msg)
	require.NoError(t, err)
	require.Equal(t, session.ID, launcher.recoveredSession, "message wake must reach the existing history-continuation recovery")
	require.NotEqual(t, ws.MessageTypeError, resp.Type)
	messages, err := svc.ListMessages(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, messages, 1, "history continuation needs the newly recorded message")
	require.Equal(t, sender.ID, messages[0].Metadata["sender_task_id"])
}

func TestTaskMessageRecoveryRejectionRemovesUnsentMessage(t *testing.T) {
	for _, tc := range []struct {
		name         string
		cause        error
		wantRecovery bool
	}{
		{name: "blocked recovery declined", cause: orchestrator.ErrSessionRecoveryRequired, wantRecovery: true},
		{name: "ordinary error", cause: errors.New("provider refused the prompt")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc, repo := newTestTaskService(t)
			sender, target, session := seedTaskWithSession(t, svc, repo, models.TaskSessionStateWaitingForInput)
			h, base := newMessageTaskHandler(t, svc, repo)
			launcher := &messageRecoveryLauncher{fakeOrchestrator: base}
			launcher.promptErrFirst = tc.cause
			h.sessionLauncher = launcher
			resp, err := h.handleMessageTask(ctx, makeWSMessage(t, ws.ActionMCPMessageTask, senderPayload(target.ID, "New instruction", sender.ID)))
			require.NoError(t, err)
			require.Equal(t, ws.MessageTypeError, resp.Type)
			require.Equal(t, tc.wantRecovery, launcher.recoveredSession != "")
			messages, err := svc.ListMessages(ctx, session.ID)
			require.NoError(t, err)
			require.Empty(t, messages)
		})
	}
}
