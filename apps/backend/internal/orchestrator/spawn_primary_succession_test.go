package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

func orchestratorPrimarySessionIDs(t *testing.T, repo interface {
	ListTaskSessions(ctx context.Context, taskID string) ([]*models.TaskSession, error)
}, taskID string) []string {
	t.Helper()
	sessions, err := repo.ListTaskSessions(context.Background(), taskID)
	require.NoError(t, err)
	var ids []string
	for _, session := range sessions {
		if session != nil && session.IsPrimary {
			ids = append(ids, session.ID)
		}
	}
	return ids
}

// A spawn launch (SpawnOrigin set) succeeds a terminal primary through the real
// startTask path.
func TestStartTaskSpawnPromotesOverTerminalPrimary(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "task-spawn", "dead-primary", models.TaskSessionStateCompleted)
	require.NoError(t, repo.SetSessionPrimary(ctx, "dead-primary"))

	taskRepo := newMockTaskRepo()
	taskRepo.tasks["task-spawn"] = &v1.Task{
		ID: "task-spawn", Title: "Task", Description: "Do the work", State: v1.TaskStateInProgress,
	}
	agentMgr := &mockAgentManager{
		repoForExecutionLookup: repo,
		launchAgentFunc: func(_ context.Context, _ *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
			return &executor.LaunchAgentResponse{AgentExecutionID: "exec-new"}, nil
		},
	}
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, agentMgr)
	svc.eventBus = &mockEventBus{}

	execution, err := svc.startTask(
		ctx, "task-spawn", "profile1", "", "", "", "spawn work", "", false, false, nil,
		startTaskOptions{SpawnOrigin: &SpawnOrigin{TaskID: "task-spawn", SessionID: "dead-primary"}},
	)
	require.NoError(t, err)
	require.NotNil(t, execution)

	ids := orchestratorPrimarySessionIDs(t, repo, "task-spawn")
	require.Len(t, ids, 1)
	assert.Equal(t, execution.SessionID, ids[0], "the spawned successor should be primary")
	assert.NotEqual(t, "dead-primary", ids[0])
}

// Spawning while the primary is live leaves the primary in place.
func TestStartTaskSpawnKeepsLivePrimary(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "task-live", "live-primary", models.TaskSessionStateRunning)
	require.NoError(t, repo.SetSessionPrimary(ctx, "live-primary"))

	taskRepo := newMockTaskRepo()
	taskRepo.tasks["task-live"] = &v1.Task{
		ID: "task-live", Title: "Task", Description: "Do the work", State: v1.TaskStateInProgress,
	}
	agentMgr := &mockAgentManager{
		repoForExecutionLookup: repo,
		launchAgentFunc: func(_ context.Context, _ *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
			return &executor.LaunchAgentResponse{AgentExecutionID: "exec-new"}, nil
		},
	}
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, agentMgr)
	svc.eventBus = &mockEventBus{}

	execution, err := svc.startTask(
		ctx, "task-live", "profile1", "", "", "", "spawn work", "", false, false, nil,
		startTaskOptions{SpawnOrigin: &SpawnOrigin{TaskID: "task-live", SessionID: "live-primary"}},
	)
	require.NoError(t, err)
	require.NotNil(t, execution)

	ids := orchestratorPrimarySessionIDs(t, repo, "task-live")
	require.Len(t, ids, 1)
	assert.Equal(t, "live-primary", ids[0], "a live primary must keep primary")
	assert.NotEqual(t, execution.SessionID, ids[0])
}

// An ordinary (non-spawn) launch never promotes.
func TestStartTaskWithoutSpawnOriginDoesNotPromote(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "task-plain", "dead-primary", models.TaskSessionStateCompleted)
	require.NoError(t, repo.SetSessionPrimary(ctx, "dead-primary"))

	taskRepo := newMockTaskRepo()
	taskRepo.tasks["task-plain"] = &v1.Task{
		ID: "task-plain", Title: "Task", Description: "Do the work", State: v1.TaskStateInProgress,
	}
	agentMgr := &mockAgentManager{
		repoForExecutionLookup: repo,
		launchAgentFunc: func(_ context.Context, _ *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
			return &executor.LaunchAgentResponse{AgentExecutionID: "exec-new"}, nil
		},
	}
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, agentMgr)
	svc.eventBus = &mockEventBus{}

	_, err := svc.startTask(
		ctx, "task-plain", "profile1", "", "", "", "plain work", "", false, false, nil,
		startTaskOptions{},
	)
	require.NoError(t, err)

	ids := orchestratorPrimarySessionIDs(t, repo, "task-plain")
	require.Len(t, ids, 1)
	assert.Equal(t, "dead-primary", ids[0], "plain launches must not change primary")
}

// The succession hands the previous primary's queue to the successor and
// delivers it there.
func TestPromoteSpawnedSuccessorOnTerminalPrimaryTransfersQueue(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "t1", "s1", models.TaskSessionStateFailed)
	seedWaitingSession(t, repo, "t1", "s2")
	seedExecutorRunning(t, repo, "s2", "t1", "exec-2")
	require.NoError(t, repo.SetSessionPrimary(ctx, "s1"))

	agentMgr := &mockAgentManager{
		isAgentRunning:         true,
		repoForExecutionLookup: repo,
		promptDone:             make(chan struct{}),
	}
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), agentMgr)
	svc.executor = executor.NewExecutor(agentMgr, repo, testLogger(), executor.ExecutorConfig{})
	_, err := svc.messageQueue.QueueMessage(
		ctx, "s1", "t1", "queued prompt", "", messagequeue.QueuedByUser, false, nil,
	)
	require.NoError(t, err)

	svc.promoteSpawnedSuccessorOnTerminalPrimary(ctx, "t1", "s2")

	ids := orchestratorPrimarySessionIDs(t, repo, "t1")
	require.Len(t, ids, 1)
	assert.Equal(t, "s2", ids[0])
	select {
	case <-agentMgr.promptDone:
	case <-time.After(2 * time.Second):
		t.Fatal("successor did not receive the transferred queue")
	}
	agentMgr.mu.Lock()
	delivered := len(agentMgr.capturedPrompts) > 0 && agentMgr.capturedPrompts[0] == "queued prompt"
	agentMgr.mu.Unlock()
	assert.True(t, delivered, "captured prompts = %+v", agentMgr.capturedPrompts)
	if _, ok := svc.messageQueue.TakeQueued(ctx, "s1"); ok {
		t.Fatal("queue stayed on the terminal primary")
	}
}

// A live primary is preserved and the queue is untouched.
func TestPromoteSpawnedSuccessorOnTerminalPrimaryKeepsLivePrimary(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "t1", "s1", "step1")
	seedWaitingSession(t, repo, "t1", "s2")
	require.NoError(t, repo.SetSessionPrimary(ctx, "s1"))
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	_, err := svc.messageQueue.QueueMessage(
		ctx, "s1", "t1", "queued prompt", "", messagequeue.QueuedByUser, false, nil,
	)
	require.NoError(t, err)

	svc.promoteSpawnedSuccessorOnTerminalPrimary(ctx, "t1", "s2")

	ids := orchestratorPrimarySessionIDs(t, repo, "t1")
	require.Len(t, ids, 1)
	assert.Equal(t, "s1", ids[0])
	if _, ok := svc.messageQueue.TakeQueued(ctx, "s1"); !ok {
		t.Fatal("queue moved even though the primary stayed live")
	}
}

// A second spawn does not overwrite a successor that is already primary.
func TestPromoteSpawnedSuccessorOnTerminalPrimarySingleWinner(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "t1", "s1", models.TaskSessionStateCancelled)
	seedWaitingSession(t, repo, "t1", "s2")
	seedWaitingSession(t, repo, "t1", "s3")
	require.NoError(t, repo.SetSessionPrimary(ctx, "s1"))
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())

	svc.promoteSpawnedSuccessorOnTerminalPrimary(ctx, "t1", "s2")
	svc.promoteSpawnedSuccessorOnTerminalPrimary(ctx, "t1", "s3")

	ids := orchestratorPrimarySessionIDs(t, repo, "t1")
	require.Len(t, ids, 1)
	assert.Equal(t, "s2", ids[0], "the first successor keeps primary")
}
