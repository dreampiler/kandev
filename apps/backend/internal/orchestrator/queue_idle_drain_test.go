package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
)

// A message left on an idle session, with no turn end or enqueue to drain it,
// is delivered by the sweep backstop.
func TestIdleQueueDrainDispatchesMessageLeftOnIdleSession(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{})
	seedTaskAndSession(t, repo, "task1", "session1", models.TaskSessionStateWaitingForInput)
	_, err := svc.messageQueue.QueueMessage(ctx, "session1", "task1", "left behind", "", messagequeue.QueuedByUser, false, nil)
	require.NoError(t, err)

	svc.drainIdleQueues(ctx)

	require.Empty(t, svc.messageQueue.GetStatus(ctx, "session1").Entries)
}

// The backstop never overrides a paused Auto-run policy.
func TestIdleQueueDrainKeepsPausedQueue(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{})
	seedTaskAndSession(t, repo, "task1", "session1", models.TaskSessionStateWaitingForInput)
	_, err := svc.messageQueue.QueueMessage(ctx, "session1", "task1", "paused", "", messagequeue.QueuedByUser, false, nil)
	require.NoError(t, err)
	require.NoError(t, svc.messageQueue.SetAutoRun(ctx, "session1", false))

	svc.drainIdleQueues(ctx)

	status := svc.messageQueue.GetStatus(ctx, "session1")
	require.False(t, status.AutoRun)
	require.Len(t, status.Entries, 1)
}

// A busy session keeps its queue for the turn-end drain.
func TestIdleQueueDrainSkipsRunningSession(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{})
	seedTaskAndSession(t, repo, "task1", "session1", models.TaskSessionStateRunning)
	_, err := svc.messageQueue.QueueMessage(ctx, "session1", "task1", "wait", "", messagequeue.QueuedByUser, false, nil)
	require.NoError(t, err)

	svc.drainIdleQueues(ctx)

	require.Len(t, svc.messageQueue.GetStatus(ctx, "session1").Entries, 1)
}

func TestIdleQueueDrainBackoffSpacesAttemptsAndForgetsEmptiedQueues(t *testing.T) {
	var backoff idleQueueDrainBackoff
	now := time.Now()
	require.True(t, backoff.due("s1", now))
	backoff.attempted("s1", now)
	require.False(t, backoff.due("s1", now.Add(30*time.Second)))
	require.True(t, backoff.due("s1", now.Add(idleQueueDrainInitialBackoff)))
	backoff.attempted("s1", now.Add(idleQueueDrainInitialBackoff))
	require.False(t, backoff.due("s1", now.Add(2*idleQueueDrainInitialBackoff)))

	backoff.retain(nil)
	require.True(t, backoff.due("s1", now))
}
