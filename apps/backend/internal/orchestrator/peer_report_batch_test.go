package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
)

func newPeerReportFixture(t *testing.T) (*Service, *mockAgentManager, chan struct{}) {
	t.Helper()
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "t-peer", "s-peer", "step1")
	seedExecutorRunning(t, repo, "s-peer", "t-peer", "exec-peer")
	session, err := repo.GetTaskSession(ctx, "s-peer")
	require.NoError(t, err)
	session.State = models.TaskSessionStateWaitingForInput
	require.NoError(t, repo.UpdateTaskSession(ctx, session))

	promptDone := make(chan struct{})
	agentMgr := &mockAgentManager{isAgentRunning: true, repoForExecutionLookup: repo, promptDone: promptDone}
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), agentMgr)
	svc.executor = executor.NewExecutor(agentMgr, repo, testLogger(), executor.ExecutorConfig{})
	svc.messageQueue = newAuthoritativeMemoryQueue(repo, testLogger())
	return svc, agentMgr, promptDone
}

func queuePeerReport(t *testing.T, svc *Service, sender, content string) {
	t.Helper()
	_, err := svc.messageQueue.QueueMessageWithMetadata(
		context.Background(),
		"s-peer",
		"t-peer",
		content,
		"",
		messagequeue.QueuedByAgent,
		false,
		nil,
		map[string]interface{}{messagequeue.MetadataSenderTaskID: sender},
	)
	require.NoError(t, err)
}

func waitForQueuedPrompt(t *testing.T, promptDone chan struct{}) {
	t.Helper()
	select {
	case <-promptDone:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the queued prompt")
	}
}

func TestDrainQueuedMessage_DeliversChildReportBurstAsOnePrompt(t *testing.T) {
	svc, agentMgr, promptDone := newPeerReportFixture(t)
	queuePeerReport(t, svc, "child-a", "report one")
	queuePeerReport(t, svc, "child-b", "report two")
	queuePeerReport(t, svc, "child-c", "report three")

	require.True(t, svc.drainQueuedMessageForPromptableSession(context.Background(), "s-peer"))
	waitForQueuedPrompt(t, promptDone)

	require.Len(t, agentMgr.capturedPrompts, 1)
	prompt := agentMgr.capturedPrompts[0]
	require.Contains(t, prompt, "from task child-a")
	require.Contains(t, prompt, "from task child-b")
	require.Contains(t, prompt, "from task child-c")
	require.Contains(t, prompt, "report one")
	require.Contains(t, prompt, "report three")
	// An ordinary reserve removes its row when the batch is taken, so an empty
	// queue here reports the reserve, not the settlement. Claim settlement for
	// the same batch is asserted against SQLite in the messagequeue package.
	require.Zero(t, svc.messageQueue.GetStatus(context.Background(), "s-peer").Count)
	require.False(t, svc.drainQueuedMessageForPromptableSession(context.Background(), "s-peer"),
		"the whole burst was consumed by one turn")
}

func TestDrainQueuedMessage_KeepsUserInstructionBehindReports(t *testing.T) {
	svc, agentMgr, promptDone := newPeerReportFixture(t)
	queuePeerReport(t, svc, "child-a", "report one")
	_, err := svc.messageQueue.QueueMessage(
		context.Background(), "s-peer", "t-peer", "operator instruction", "", "user", false, nil)
	require.NoError(t, err)
	queuePeerReport(t, svc, "child-b", "report two")

	require.True(t, svc.drainQueuedMessageForPromptableSession(context.Background(), "s-peer"))
	waitForQueuedPrompt(t, promptDone)
	require.Len(t, agentMgr.capturedPrompts, 1)
	require.Contains(t, agentMgr.capturedPrompts[0], "report one")
	require.NotContains(t, agentMgr.capturedPrompts[0], "report two")
	require.NotContains(t, agentMgr.capturedPrompts[0], "operator instruction")
	require.Equal(t, 2, svc.messageQueue.GetStatus(context.Background(), "s-peer").Count)
}
