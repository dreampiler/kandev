package handlers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	ws "github.com/kandev/kandev/pkg/websocket"
	"github.com/stretchr/testify/require"
)

type timeoutMutationQueue struct {
	*messagequeue.Service
}

func (*timeoutMutationQueue) CancelAllWithEntries(context.Context, string) ([]messagequeue.QueuedMessage, error) {
	return nil, messagequeue.ErrSessionAdmissionTimeout
}

func (*timeoutMutationQueue) CancelAllForSession(context.Context, messagequeue.QueueSessionIdentity) (*messagequeue.QueueRemovalResult, error) {
	return nil, messagequeue.ErrSessionAdmissionTimeout
}

func (*timeoutMutationQueue) MergeIntoAbove(context.Context, string, string, string) (*messagequeue.QueuedMessage, error) {
	return nil, messagequeue.ErrSessionAdmissionTimeout
}

func (*timeoutMutationQueue) MergeIntoAboveForSession(context.Context, messagequeue.QueueSessionIdentity, string, string) (*messagequeue.QueuedMessage, error) {
	return nil, messagequeue.ErrSessionAdmissionTimeout
}

func (*timeoutMutationQueue) ReorderEntries(context.Context, string, []string) error {
	return messagequeue.ErrSessionAdmissionTimeout
}

func (*timeoutMutationQueue) ReorderEntriesForSession(context.Context, messagequeue.QueueSessionIdentity, []string) error {
	return messagequeue.ErrSessionAdmissionTimeout
}

func (*timeoutMutationQueue) SetSessionAutoMerge(context.Context, messagequeue.QueueSessionIdentity, bool) (messagequeue.AutoMergePolicy, error) {
	return messagequeue.AutoMergePolicy{}, messagequeue.ErrSessionAdmissionTimeout
}

func TestQueueMutationsReturnRetryableAdmissionTimeout(t *testing.T) {
	for _, identified := range []bool{false, true} {
		for _, action := range []string{ws.ActionMessageQueueCancel, ws.ActionMessageQueueMerge, ws.ActionMessageQueueReorder, ws.ActionMessageQueueAutoMergeSet} {
			t.Run(action+map[bool]string{false: "/legacy", true: "/identified"}[identified], func(t *testing.T) {
				queue := messagequeue.NewServiceMemory(logger.Default())
				queue.SetAutoMergeEnabled(false)
				entry, err := queue.QueueMessage(context.Background(), "session", "task", "retain me", "", "user", false, nil)
				require.NoError(t, err)
				var access QueueAccessAuthorizer = allowQueueAccess{}
				if identified {
					access = allowQueueIdentityAccess{}
				}
				h := NewQueueHandlers(&timeoutMutationQueue{Service: queue}, &mockEventBus{}, logger.Default(), nil, access, nil)
				msg := createTestMessage(t, action, map[string]interface{}{
					"task_id": "task", "session_id": "session", "session_incarnation_id": "memory:session",
					"entry_id": entry.ID, "ordered_ids": []string{entry.ID}, "enabled": false,
				})
				var response *ws.Message
				switch action {
				case ws.ActionMessageQueueCancel:
					response, err = h.wsCancelAll(context.Background(), msg)
				case ws.ActionMessageQueueMerge:
					response, err = h.wsMergeIntoAbove(context.Background(), msg)
				case ws.ActionMessageQueueReorder:
					response, err = h.wsReorder(context.Background(), msg)
				case ws.ActionMessageQueueAutoMergeSet:
					response, err = h.wsSetAutoMerge(context.Background(), msg)
				}
				require.NoError(t, err)
				require.Equal(t, ws.MessageTypeError, response.Type)
				var payload struct {
					Code string `json:"code"`
				}
				require.NoError(t, json.Unmarshal(response.Payload, &payload))
				require.Equal(t, ws.ErrorCodeUnavailable, payload.Code)
				require.Equal(t, "retain me", queue.GetStatus(context.Background(), "session").Entries[0].Content)
			})
		}
	}
}
