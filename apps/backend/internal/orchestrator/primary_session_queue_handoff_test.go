package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
)

func queuedContents(t *testing.T, svc *Service, sessionID string) []string {
	t.Helper()
	entries, _, err := svc.messageQueue.SnapshotSession(context.Background(), sessionID)
	require.NoError(t, err)
	contents := make([]string, 0, len(entries))
	for _, entry := range entries {
		contents = append(contents, entry.Content)
	}
	return contents
}

// A message queued for a primary that then fails would otherwise stay queued
// forever: the terminal session never becomes promptable again.
func TestPrimaryPromotionHandsOffTerminalSessionQueue(t *testing.T) {
	svc, repo := newPrimaryPromotionFixture(t, "",
		&models.TaskSession{ID: "session-live", State: models.TaskSessionStateRunning},
	)
	ctx := context.Background()
	_, err := svc.messageQueue.QueueMessage(ctx, primaryStoppedID, primaryTaskID, "follow-up", "", "user", false, nil)
	require.NoError(t, err)

	require.NoError(t, repo.UpdateTaskSessionState(ctx, primaryStoppedID, models.TaskSessionStateFailed, ""))
	svc.maybePromotePrimary(ctx, primaryTaskID, primaryStoppedID, models.TaskSessionStateFailed)

	require.Equal(t, "session-live", primarySessionID(t, repo))
	require.Empty(t, queuedContents(t, svc, primaryStoppedID))
	require.Equal(t, []string{"follow-up"}, queuedContents(t, svc, "session-live"))
}

// An explicit primary choice that moves the role off a terminal session
// carries its queue too, e.g. when no candidate existed at stop time.
func TestExplicitPrimaryChoiceHandsOffTerminalSessionQueue(t *testing.T) {
	svc, repo := newPrimaryPromotionFixture(t, "")
	ctx := context.Background()
	_, err := svc.messageQueue.QueueMessage(ctx, primaryStoppedID, primaryTaskID, "follow-up", "", "user", false, nil)
	require.NoError(t, err)
	stopPrimary(t, svc, repo)
	require.Equal(t, primaryStoppedID, primarySessionID(t, repo))

	// Starting is not promptable yet, so the moved message stays visible.
	next := &models.TaskSession{ID: "session-next", TaskID: primaryTaskID, State: models.TaskSessionStateStarting}
	require.NoError(t, repo.CreateTaskSession(ctx, next))
	require.NoError(t, svc.SetPrimarySession(ctx, next.ID))

	require.Equal(t, "session-next", primarySessionID(t, repo))
	require.Empty(t, queuedContents(t, svc, primaryStoppedID))
	require.Equal(t, []string{"follow-up"}, queuedContents(t, svc, "session-next"))
}

// A live previous primary keeps its own queue.
func TestPrimaryChoiceKeepsQueueOfLiveSession(t *testing.T) {
	svc, repo := newPrimaryPromotionFixture(t, "")
	ctx := context.Background()
	require.NoError(t, repo.UpdateTaskSessionState(ctx, primaryStoppedID, models.TaskSessionStateRunning, ""))
	_, err := svc.messageQueue.QueueMessage(ctx, primaryStoppedID, primaryTaskID, "follow-up", "", "user", false, nil)
	require.NoError(t, err)

	next := &models.TaskSession{ID: "session-next", TaskID: primaryTaskID, State: models.TaskSessionStateWaitingForInput}
	require.NoError(t, repo.CreateTaskSession(ctx, next))
	require.NoError(t, svc.SetPrimarySession(ctx, next.ID))

	require.Equal(t, []string{"follow-up"}, queuedContents(t, svc, primaryStoppedID))
	require.Empty(t, queuedContents(t, svc, "session-next"))
}
