package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

func queueSnapshotContents(t *testing.T, svc *Service, sessionID string) []string {
	t.Helper()
	entries, _, err := svc.messageQueue.SnapshotSession(context.Background(), sessionID)
	require.NoError(t, err)
	contents := make([]string, 0, len(entries))
	for _, entry := range entries {
		contents = append(contents, entry.Content)
	}
	return contents
}

func setActionRequiredRoute(t *testing.T, repo *sqliterepo.Repository, sessionID string) {
	t.Helper()
	ctx := context.Background()
	session, err := repo.GetTaskSession(ctx, sessionID)
	require.NoError(t, err)
	session.AgentProfileID = "dynamic-logical"
	session.ExecutionProfileID = "candidate-1"
	session.RouteGeneration = 1
	session.RouteState = dynamicRouteStatusActionRequired
	session.RouteReason = dynamicRouteOrphanedStartingReason
	require.NoError(t, repo.UpdateTaskSession(ctx, session))
}

// A queued message for a WAITING_FOR_INPUT session whose dynamic route awaits a
// recovery action and which has no executor record cannot run until the user
// applies that action. Automatic drains must leave the entry queued instead of
// reserving it, failing in ensureSessionRunning, and requeueing it forever.
//
// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.15
func TestQueueDrainSkipsSessionWhoseDynamicRouteAwaitsAction(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "t1", "s1", models.TaskSessionStateWaitingForInput)
	setActionRequiredRoute(t, repo, "s1")
	agentMgr := &mockAgentManager{repoForExecutionLookup: repo}
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), agentMgr)
	workerDone := make(chan struct{}, 4)
	svc.onQueuedMessageExecutionComplete = func() { workerDone <- struct{}{} }

	_, err := svc.messageQueue.QueueMessage(ctx, "s1", "t1", "hello", "", "user", false, nil)
	require.NoError(t, err)

	for attempt := 0; attempt < 2; attempt++ {
		if svc.drainQueuedMessageForPromptableSession(ctx, "s1") {
			select {
			case <-workerDone:
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for the dispatched queued message to settle")
			}
			t.Fatalf("drain %d dispatched a queued message to a session that cannot run until its route action is applied", attempt+1)
		}
	}
	require.Equal(t, []string{"hello"}, queueSnapshotContents(t, svc, "s1"))
}

// A dispatch that reaches a WAITING_FOR_INPUT session whose dynamic route is
// still "starting" with no executor record has no launch owner. The failure
// releases the route to action_required so the session shows its route
// recovery actions, keeps the message queued, and stops further drains.
//
// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.16
func TestQueuedDispatchToStrandedStartingRouteMarksActionRequired(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-stranded-queue"
		sessionID   = "session-stranded-queue"
		executionID = "execution-stranded-queue"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateWaitingForInput)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)
	agentMgr := &mockAgentManager{repoForExecutionLookup: repo}
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, agentMgr)
	seedEngine := dynamicruntime.NewEngine(dynamicruntime.WithPersistence(repo))
	svc.SetProfileExecutionResolver(agentruntime.NewProfileExecutionResolver(nil, seedEngine, true))
	seedClaimedDynamicRoute(t, ctx, repo, seedEngine, sessionID, executionID)
	recoveryEngine := dynamicruntime.NewEngine(
		dynamicruntime.WithPersistence(repo),
		dynamicruntime.WithStateLoader(repo),
	)
	svc.SetProfileExecutionResolver(agentruntime.NewProfileExecutionResolver(nil, recoveryEngine, true))
	workerDone := make(chan struct{}, 4)
	svc.onQueuedMessageExecutionComplete = func() { workerDone <- struct{}{} }

	_, err := svc.messageQueue.QueueMessage(ctx, sessionID, taskID, "hello", "", "user", false, nil)
	require.NoError(t, err)
	require.True(t, svc.drainQueuedMessageForPromptableSession(ctx, sessionID),
		"the first drain must attempt the dispatch while the route still reads starting")
	select {
	case <-workerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the queued dispatch to settle")
	}

	session, err := repo.GetTaskSession(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, dynamicRouteStatusActionRequired, session.RouteState,
		"a dispatch with no launch owner must release the stranded route for user recovery")
	require.Equal(t, dynamicRouteOrphanedStartingReason, session.RouteReason)
	require.Equal(t, models.TaskSessionStateWaitingForInput, session.State)
	require.Equal(t, []string{"hello"}, queueSnapshotContents(t, svc, sessionID))
	require.False(t, svc.drainQueuedMessageForPromptableSession(ctx, sessionID),
		"later drains must wait for the route recovery action")
}
