package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPrimaryReplacementStopsSettledOldExecution(t *testing.T) {
	ctx := context.Background()
	svc, _, row, stopped := newReplacedSessionFixture(t)
	require.NoError(t, svc.SetPrimarySession(ctx, "replacement-session"))
	awaitReplacedExecutionStop(t, stopped, row.AgentExecutionID)
	primary, err := svc.repo.GetTaskSession(ctx, "replacement-session")
	require.NoError(t, err)
	require.True(t, primary.IsPrimary)
	require.Equal(t, models.TaskSessionStateWaitingForInput, primary.State)
}

func newReplacedSessionFixture(t *testing.T) (*Service, *idleParkingTestAgent, *models.ExecutorRunning, chan string) {
	t.Helper()
	ctx := context.Background()
	svc, agent, row := newIdleParkingFixture(t, true, 60, "codex-acp")
	require.NoError(t, svc.repo.SetSessionPrimary(ctx, row.SessionID))
	require.NoError(t, svc.repo.(*sqliterepo.Repository).CreateTaskSession(ctx, &models.TaskSession{
		ID: "replacement-session", TaskID: row.TaskID, State: models.TaskSessionStateWaitingForInput,
		StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}))
	agent.repoForExecutionLookup = svc.repo
	svc.executor = executor.NewExecutor(agent, svc.repo.(*sqliterepo.Repository), testLogger(), executor.ExecutorConfig{})
	stopped := make(chan string, 2)
	agent.stopAgentWithReasonFunc = func(_ context.Context, executionID, _ string, _ bool) error {
		stopped <- executionID
		return nil
	}
	beforePrimary, beforeErr := svc.primarySessionID(ctx, row.TaskID)
	require.NoError(t, beforeErr)
	require.Equal(t, row.SessionID, beforePrimary)
	return svc, agent, row, stopped
}

func awaitReplacedExecutionStop(t *testing.T, stopped <-chan string, expected string) {
	t.Helper()
	select {
	case executionID := <-stopped:
		require.Equal(t, expected, executionID)
	case <-time.After(3 * time.Second):
		t.Fatal("demoted settled execution was not stopped")
	}
}

func TestPrimaryReplacementDefersUntilActiveTurnSettles(t *testing.T) {
	ctx := context.Background()
	svc, _, row, stopped := newReplacedSessionFixture(t)
	svc.turnService = &alwaysActiveTurnService{}
	require.NoError(t, svc.SetPrimarySession(ctx, "replacement-session"))
	select {
	case <-stopped:
		t.Fatal("replacement stopped an active turn")
	default:
	}
	old, err := svc.repo.GetTaskSession(ctx, row.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TaskSessionStateWaitingForInput, old.State)
	svc.turnService = &inactiveTurnService{}
	svc.reclaimReplacedSessionsOnce(ctx)
	awaitReplacedExecutionStop(t, stopped, row.AgentExecutionID)
}

func TestPrimaryReplacementDoesNotStopRotatedExecution(t *testing.T) {
	ctx := context.Background()
	svc, _, row, stopped := newReplacedSessionFixture(t)
	svc.turnService = &alwaysActiveTurnService{}
	require.NoError(t, svc.SetPrimarySession(ctx, "replacement-session"))
	row.AgentExecutionID = "rotated-execution"
	require.NoError(t, svc.repo.UpsertExecutorRunning(ctx, row))
	svc.turnService = &inactiveTurnService{}
	svc.reclaimReplacedSessionsOnce(ctx)
	select {
	case <-stopped:
		t.Fatal("stale replacement stopped a rotated execution")
	default:
	}
}

func TestPrimaryReplacementDoesNotStopRepromotedSession(t *testing.T) {
	ctx := context.Background()
	svc, _, row, stopped := newReplacedSessionFixture(t)
	svc.turnService = &alwaysActiveTurnService{}
	require.NoError(t, svc.SetPrimarySession(ctx, "replacement-session"))
	require.NoError(t, svc.SetPrimarySession(ctx, row.SessionID))
	svc.turnService = &inactiveTurnService{}
	svc.reclaimReplacedSessionsOnce(ctx)
	select {
	case <-stopped:
		t.Fatal("stale replacement stopped a re-promoted primary")
	default:
	}
	current, err := svc.repo.GetTaskSession(ctx, row.SessionID)
	require.NoError(t, err)
	require.True(t, current.IsPrimary)
}

func TestPrimaryReplacementAllowsCallerOwnedPromotionLock(t *testing.T) {
	ctx := context.Background()
	svc, _, row, stopped := newReplacedSessionFixture(t)
	release := svc.acquireSessionLifecycleLock("replacement-session")
	done := make(chan error, 1)
	go func() { done <- svc.SetPrimarySession(ctx, "replacement-session") }()
	returned := false
	select {
	case err := <-done:
		require.NoError(t, err)
		returned = true
	case <-time.After(2 * time.Second):
	}
	release()
	if !returned {
		require.NoError(t, <-done)
	}
	awaitReplacedExecutionStop(t, stopped, row.AgentExecutionID)
	require.True(t, returned, "primary promotion must not re-lock a caller-owned lifecycle mutex")
}

func TestPrimaryReplacementPreservesProtectedOwner(t *testing.T) {
	ctx := context.Background()
	svc, _, row, stopped := newReplacedSessionFixture(t)
	task, err := svc.repo.GetTask(ctx, row.TaskID)
	require.NoError(t, err)
	task.Origin = models.TaskOriginCoordinator
	require.NoError(t, svc.repo.(*sqliterepo.Repository).UpdateTask(ctx, task))
	require.NoError(t, svc.SetPrimarySession(ctx, "replacement-session"))
	svc.reclaimReplacedSessionsOnce(ctx)
	select {
	case <-stopped:
		t.Fatal("replacement stopped a protected coordinator owner")
	default:
	}
	old, err := svc.repo.GetTaskSession(ctx, row.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TaskSessionStateWaitingForInput, old.State)
}

func TestQueueReplacementStopsSettledDemotedExecution(t *testing.T) {
	ctx := context.Background()
	svc, _, row, stopped := newReplacedSessionFixture(t)
	_, err := svc.messageQueue.QueueMessage(ctx, row.SessionID, row.TaskID, "successor prompt", "", "user", false, nil)
	require.NoError(t, err)
	require.NoError(t, svc.SetPrimarySession(ctx, "replacement-session"))
	select {
	case <-stopped:
		t.Fatal("old session stopped before its queue transferred")
	default:
	}
	require.NoError(t, svc.transferQueuedSessionState(ctx, row.TaskID, row.SessionID, "replacement-session"))
	svc.reclaimReplacedSessionsOnce(ctx)
	awaitReplacedExecutionStop(t, stopped, row.AgentExecutionID)
	require.Zero(t, svc.messageQueue.GetStatus(ctx, row.SessionID).Count)
	require.Equal(t, 1, svc.messageQueue.GetStatus(ctx, "replacement-session").Count)
}

func TestIdleReaperKeepsSixtyMinutePolicyAndUnknownOwnership(t *testing.T) {
	for _, tc := range []struct {
		name    string
		idle    time.Duration
		unknown bool
		want    int
	}{
		{name: "within sixty minutes", idle: 59 * time.Minute},
		{name: "past sixty minutes", idle: 61 * time.Minute, want: 1},
		{name: "unknown execution", idle: 61 * time.Minute, unknown: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, agent, _ := newIdleParkingFixture(t, true, 60, "codex-acp")
			agent.currentPromptLastActivityAt = time.Now().UTC().Add(-tc.idle)
			if tc.unknown {
				agent.currentPromptExecutionID = "foreign-execution"
			}
			svc.suspendWorkspaceIdleSessionsOnce(context.Background())
			require.Len(t, agent.suspensionCalls(), tc.want)
		})
	}
}
