package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/orchestrator"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/service"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	ws "github.com/kandev/kandev/pkg/websocket"
)

type failedRecoveryOrchestrator struct {
	fgActivityOrchestrator
	eligible  bool
	recovered chan string
}

func (o *failedRecoveryOrchestrator) CanAutoRecoverFailedSession(context.Context, string, string) bool {
	return o.eligible
}

func (o *failedRecoveryOrchestrator) PromptTask(context.Context, string, string, string, string, bool, []v1.MessageAttachment, bool) (*orchestrator.PromptResult, error) {
	return nil, orchestrator.ErrSessionRecoveryRequired
}

func (o *failedRecoveryOrchestrator) AutoRecoverBlockedPrompt(_ context.Context, _, sessionID string, _ error) bool {
	o.recovered <- sessionID
	return true
}

func TestMessageToRecoverableFailedPrimaryUsesExistingContinuation(t *testing.T) {
	for _, tc := range []struct {
		name               string
		state              models.TaskSessionState
		eligible, accepted bool
	}{
		{"recoverable failure", models.TaskSessionStateFailed, true, true},
		{"ordinary failure", models.TaskSessionStateFailed, false, false},
		{"user cancelled", models.TaskSessionStateCancelled, true, false},
		{"completed", models.TaskSessionStateCompleted, true, false},
		{"waiting recovery", models.TaskSessionStateWaitingForInput, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &messageAddSwitchRepo{
				tasks: map[string]*models.Task{"t1": {ID: "t1", WorkspaceID: "ws1", State: v1.TaskStateInProgress}},
				sessions: map[string]*models.TaskSession{"s1": {
					ID: "s1", TaskID: "t1", State: tc.state, IsPrimary: true, UpdatedAt: time.Now().UTC(),
				}},
				primaryID: "s1",
			}
			log, err := logger.NewLogger(logger.LoggingConfig{Level: "error", Format: "json"})
			require.NoError(t, err)
			svc := service.NewService(service.Repos{Tasks: repo, TaskRepos: repo, Messages: repo, Turns: repo, Sessions: repo}, nil, log, service.RepositoryDiscoveryConfig{})
			orch := &failedRecoveryOrchestrator{eligible: tc.eligible, recovered: make(chan string, 1)}
			h := NewMessageHandlers(svc, orch, log)
			req, err := ws.NewRequest("recover-message", ws.ActionMessageAdd, map[string]any{
				"task_id": "t1", "session_id": "s1", "content": "continue the controller",
			})
			require.NoError(t, err)
			resp, err := h.wsAddMessage(context.Background(), req)
			require.NoError(t, err)
			if !tc.accepted {
				require.Equal(t, ws.MessageTypeError, resp.Type)
				require.Zero(t, repo.messageCount())
				require.Empty(t, orch.recovered)
				return
			}
			require.Equal(t, ws.MessageTypeResponse, resp.Type)
			select {
			case sessionID := <-orch.recovered:
				require.Equal(t, "s1", sessionID)
			case <-time.After(5 * time.Second):
				t.Fatal("accepted message did not enter automatic continue_from_history")
			}
			require.Equal(t, 1, repo.messageCount(), "continuation must include the recorded message")
		})
	}
}
