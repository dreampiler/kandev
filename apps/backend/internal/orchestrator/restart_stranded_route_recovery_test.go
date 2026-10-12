package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

type strandedRouteFixture struct {
	svc        *Service
	repo       *sqliterepo.Repository
	engine     *dynamicruntime.Engine
	agent      *mockAgentManager
	taskID     string
	sessionID  string
	generation int64
}

// newStrandedRouteFixture reproduces the state a second backend restart leaves
// behind for a dynamic-profile session: the route is parked at action_required
// (its agent died first and the policy stopped), a recovery block records the
// interrupted turn's unknown outcome, and a turn row is still open with no
// runtime behind it.
func newStrandedRouteFixture(t *testing.T) *strandedRouteFixture {
	t.Helper()
	ctx := context.Background()
	const (
		taskID      = "task-stranded-route"
		sessionID   = "session-stranded-route"
		executionID = "execution-stranded-route"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateWaitingForInput)
	seedExecutorRunning(t, repo, sessionID, taskID, executionID)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateReview)
	agent := &mockAgentManager{isAgentRunningFn: func(context.Context, string) bool { return false }}
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, agent)
	svc.executor = executor.NewExecutor(agent, repo, testLogger(), executor.ExecutorConfig{})
	svc.turnService = &repoTurnService{repo: repo}

	engine := dynamicruntime.NewEngine(dynamicruntime.WithPersistence(repo))
	svc.SetProfileExecutionResolver(agentruntime.NewProfileExecutionResolver(nil, engine, true))
	decision := seedClaimedDynamicRoute(t, ctx, repo, engine, sessionID, executionID)
	svc.markDynamicRouteActionRequired(ctx, sessionID, decision.Generation, "agent_process_start_failed")

	session, err := repo.GetTaskSession(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, dynamicRouteStatusActionRequired, session.RouteState)
	require.NoError(t, repo.UpsertSessionRecoveryBlock(ctx, &models.SessionRecoveryBlock{
		SessionID: session.ID, IncarnationID: session.QueueIncarnationID,
		Reason: unknownPromptOutcome, State: models.RecoveryBlockOpen,
	}))
	_, err = svc.turnService.StartTurn(ctx, sessionID)
	require.NoError(t, err)
	return &strandedRouteFixture{
		svc: svc, repo: repo, engine: engine, agent: agent,
		taskID: taskID, sessionID: sessionID, generation: decision.Generation,
	}
}

func (f *strandedRouteFixture) explicitRecoveryAdmission(ctx context.Context) error {
	_, err := f.svc.loadResumeSessionCandidate(ctx, f.taskID, f.sessionID, executor.ResumeOptions{
		RecoveryAction: recoveryActionContinueHistory,
	}, nil)
	return err
}

func (f *strandedRouteFixture) routeStatus(t *testing.T, ctx context.Context) (string, string) {
	t.Helper()
	state, err := f.repo.LoadRouteState(ctx, f.sessionID)
	require.NoError(t, err)
	require.NotNil(t, state)
	session, err := f.repo.GetTaskSession(ctx, f.sessionID)
	require.NoError(t, err)
	return state.Status, session.RouteState
}

// TestRestartStrandedDynamicRoute_BothRecoveryPathsAreAdmissible pins the mutual
// block: the route-action guard refuses the orphaned turn and the explicit
// recovery guard refuses the parked route. The fix must leave both admissible.
func TestRestartStrandedDynamicRoute_BothRecoveryPathsAreAdmissible(t *testing.T) {
	ctx := context.Background()
	f := newStrandedRouteFixture(t)

	var blocked *sessionOpenRecoveryBlockedError
	err := f.explicitRecoveryAdmission(ctx)
	require.True(t, errors.As(err, &blocked), "explicit recovery on a parked route is refused without a reclaim: %v", err)
	require.Equal(t, autoResumeBlockedDynamicRoute, blocked.reason)

	handlerCalls := 0
	f.svc.SetRouteActionHandler(func(context.Context, RouteActionRequest) (*RouteActionResult, error) {
		handlerCalls++
		return &RouteActionResult{SessionID: f.sessionID}, nil
	})
	_, err = f.svc.ApplyRouteAction(ctx, RouteActionRequest{
		SessionID: f.sessionID, Action: RouteActionRetry, ExpectedGeneration: f.generation,
	})
	require.NoError(t, err, "a turn no runtime can settle must not block the route action")
	require.Equal(t, 1, handlerCalls)
	active, err := f.svc.turnService.GetActiveTurn(ctx, f.sessionID)
	require.NoError(t, err)
	require.Nil(t, active, "the orphaned turn is settled before the route action runs")

	reclaimedGeneration, reclaimed := f.svc.reclaimActionRequiredRouteForRecovery(ctx, f.sessionID, recoveryActionContinueHistory)
	require.True(t, reclaimed)
	require.Equal(t, f.generation, reclaimedGeneration)
	durable, projected := f.routeStatus(t, ctx)
	require.Equal(t, dynamicRouteStatusStarting, durable)
	require.Equal(t, dynamicRouteStatusStarting, projected)
	require.NoError(t, f.explicitRecoveryAdmission(ctx), "the reclaimed route admits the explicit recovery")
}

func TestRestartStrandedDynamicRoute_FailedRecoveryReturnsRouteToActionRequired(t *testing.T) {
	ctx := context.Background()
	f := newStrandedRouteFixture(t)

	generation, reclaimed := f.svc.reclaimActionRequiredRouteForRecovery(ctx, f.sessionID, recoveryActionResume)
	require.True(t, reclaimed)
	f.svc.restoreRouteAfterFailedRecovery(ctx, f.sessionID, generation)

	durable, projected := f.routeStatus(t, ctx)
	require.Equal(t, dynamicRouteStatusActionRequired, durable)
	require.Equal(t, dynamicRouteStatusActionRequired, projected)
	session, err := f.repo.GetTaskSession(ctx, f.sessionID)
	require.NoError(t, err)
	require.Equal(t, explicitRecoveryFailedRouteReason, session.RouteReason)
}

func TestRestartStrandedDynamicRoute_LiveRuntimeKeepsGuards(t *testing.T) {
	ctx := context.Background()
	f := newStrandedRouteFixture(t)
	f.agent.isAgentRunningFn = func(context.Context, string) bool { return true }

	_, reclaimed := f.svc.reclaimActionRequiredRouteForRecovery(ctx, f.sessionID, recoveryActionContinueHistory)
	require.False(t, reclaimed, "a live runtime still owns the session")
	durable, projected := f.routeStatus(t, ctx)
	require.Equal(t, dynamicRouteStatusActionRequired, durable)
	require.Equal(t, dynamicRouteStatusActionRequired, projected)

	f.svc.SetRouteActionHandler(func(context.Context, RouteActionRequest) (*RouteActionResult, error) {
		t.Fatal("route action handler must not run while a live runtime may own the turn")
		return nil, nil
	})
	_, err := f.svc.ApplyRouteAction(ctx, RouteActionRequest{
		SessionID: f.sessionID, Action: RouteActionRetry, ExpectedGeneration: f.generation,
	})
	require.ErrorIs(t, err, ErrRouteActionActiveTurn)
}

// TestRestartStrandedDynamicRoute_RouteActionHandsOffToContinuation covers the
// successor launch of a manual route action while the restart-interrupted turn
// still blocks the session: it must not re-send the last prompt. The claimed
// generation is parked for continue_from_history instead.
func TestRestartStrandedDynamicRoute_RouteActionHandsOffToContinuation(t *testing.T) {
	ctx := context.Background()
	f := newStrandedRouteFixture(t)

	// The route-action handler claimed a retry of the current candidate.
	decision, err := f.engine.ResumePendingNow(ctx, f.sessionID, f.generation)
	require.NoError(t, err)
	require.Equal(t, dynamicRouteStatusRetrying, decision.Status)
	session, err := f.repo.GetTaskSession(ctx, f.sessionID)
	require.NoError(t, err)
	session.RouteState = dynamicRouteStatusRetrying
	require.NoError(t, f.repo.UpdateTaskSession(ctx, session))

	require.NoError(t, f.svc.LaunchDynamicRouteAction(ctx, f.sessionID))
	require.Empty(t, f.agent.capturedPromptCalls, "the interrupted prompt is not re-sent")
	value, handed := f.svc.routeActionContinuations.Load(f.sessionID)
	require.True(t, handed)
	require.Equal(t, f.taskID, value)
	durable, projected := f.routeStatus(t, ctx)
	require.Equal(t, dynamicRouteStatusActionRequired, durable)
	require.Equal(t, dynamicRouteStatusActionRequired, projected)
	require.False(t, f.engine.OwnsRetryClaim(f.sessionID, f.generation), "the parked claim no longer blocks later route actions")

	_, reclaimed := f.svc.reclaimActionRequiredRouteForRecovery(ctx, f.sessionID, recoveryActionContinueHistory)
	require.True(t, reclaimed, "the continuation owns the parked generation")
}
