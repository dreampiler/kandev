package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// seedTaskWorkflowSessionRoute records a workflow-session route whose current
// destination is destinationSessionID. The route must carry the fields
// LoadWorkflowSessionRoute requires so it is not treated as a stale record.
func seedTaskWorkflowSessionRoute(t *testing.T, repo *sqliterepo.Repository, taskID, destinationSessionID string) {
	t.Helper()
	route := models.WorkflowSessionRoute{
		OperationID:       "workflow-session:" + taskID + ":step-1:entry:00000000000000000001:profile::reuse",
		DestinationStepID: "step-1",
		TargetKind:        "profile",
		Phase:             workflowSessionRouteCommitted,
		DestinationID:     destinationSessionID,
	}
	require.NoError(t, repo.SetTaskMetadataKey(
		context.Background(), taskID, models.MetaKeyWorkflowSessionRoute, route,
	))
}

// testSupersededStartingRouteService wires a service whose route resolver can
// settle a durable "starting" route, mirroring the production reconcile path.
func testSupersededStartingRouteService(
	t *testing.T,
	repo *sqliterepo.Repository,
	taskID, sessionID, executionID string,
) *Service {
	t.Helper()
	ctx := context.Background()
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, &mockAgentManager{})

	seedEngine := dynamicruntime.NewEngine(dynamicruntime.WithPersistence(repo))
	svc.SetProfileExecutionResolver(agentruntime.NewProfileExecutionResolver(nil, seedEngine, true))
	seedClaimedDynamicRoute(t, ctx, repo, seedEngine, sessionID, executionID)

	// Reconcile through a fresh engine so the recovery reads the durable route
	// row, not the seeding process's cache.
	recoveryEngine := dynamicruntime.NewEngine(
		dynamicruntime.WithPersistence(repo),
		dynamicruntime.WithStateLoader(repo),
	)
	svc.SetProfileExecutionResolver(agentruntime.NewProfileExecutionResolver(nil, recoveryEngine, true))
	return svc
}

// TestReconcileOrphanedDynamicStartingRoutes_SweepsSupersededCreatedRoute fixes
// the tier_pace stall: a CREATED session claims a durable "starting" route
// during preparation, then the task's workflow-session route moves to a
// different destination. Nothing settles the abandoned claim, so the session
// stays "starting" until a restart. The runtime sweep must release it.
func TestReconcileOrphanedDynamicStartingRoutes_SweepsSupersededCreatedRoute(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-superseded-created-route"
		sessionID   = "session-superseded-created-route"
		executionID = "execution-superseded-created-route"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateCreated)
	svc := testSupersededStartingRouteService(t, repo, taskID, sessionID, executionID)
	seedTaskWorkflowSessionRoute(t, repo, taskID, "session-other-destination")

	svc.reconcileOrphanedDynamicStartingRoutes(ctx)

	routeState, err := repo.LoadRouteState(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, routeState)
	require.Equal(t, dynamicRouteStatusActionRequired, routeState.Status)
	session, err := repo.GetTaskSession(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, dynamicRouteStatusActionRequired, session.RouteState)
	require.Equal(t, models.TaskSessionStateCreated, session.State,
		"settling the route must not change the session's own state")
}

// TestReconcileOrphanedDynamicStartingRoutes_KeepsCreatedRouteThatIsCurrentDestination
// is the positive gate: while the task route still names this session, the
// "starting" claim belongs to a live preparation (for example a prepared
// workspace waiting for the first prompt or for capacity) and must be kept.
func TestReconcileOrphanedDynamicStartingRoutes_KeepsCreatedRouteThatIsCurrentDestination(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-current-destination-created-route"
		sessionID   = "session-current-destination-created-route"
		executionID = "execution-current-destination-created-route"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateCreated)
	svc := testSupersededStartingRouteService(t, repo, taskID, sessionID, executionID)
	seedTaskWorkflowSessionRoute(t, repo, taskID, sessionID)

	svc.reconcileOrphanedDynamicStartingRoutes(ctx)

	routeState, err := repo.LoadRouteState(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, routeState)
	require.Equal(t, "starting", routeState.Status)
}

// TestReconcileOrphanedDynamicStartingRoutes_KeepsCreatedRouteWithNoRecordedRoute
// guards the conservative default: without a recorded route there is no
// supersession evidence, so the claim is left untouched.
func TestReconcileOrphanedDynamicStartingRoutes_KeepsCreatedRouteWithNoRecordedRoute(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-no-route-created-claim"
		sessionID   = "session-no-route-created-claim"
		executionID = "execution-no-route-created-claim"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateCreated)
	svc := testSupersededStartingRouteService(t, repo, taskID, sessionID, executionID)

	svc.reconcileOrphanedDynamicStartingRoutes(ctx)

	routeState, err := repo.LoadRouteState(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, routeState)
	require.Equal(t, "starting", routeState.Status)
}

// TestCeilingSweepTick_ReconcilesSupersededStartingRoute pins the runtime
// owner: the periodic sweep, not startup alone, must settle an orphaned claim
// so a stall that begins after boot is recovered without a restart.
func TestCeilingSweepTick_ReconcilesSupersededStartingRoute(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-sweep-superseded-created-route"
		sessionID   = "session-sweep-superseded-created-route"
		executionID = "execution-sweep-superseded-created-route"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateCreated)
	svc := testSupersededStartingRouteService(t, repo, taskID, sessionID, executionID)
	seedTaskWorkflowSessionRoute(t, repo, taskID, "session-other-destination")

	svc.ceilingSweepTick(ctx, ceilingSweepPeriodic)

	routeState, err := repo.LoadRouteState(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, routeState)
	require.Equal(t, dynamicRouteStatusActionRequired, routeState.Status)
}

// TestCompleteAndStopSession_SettlesClaimedStartingRoute pins the boundary
// settlement: ending a session must release a route it still held as
// "starting" so the claim cannot outlive its launch owner.
func TestCompleteAndStopSession_SettlesClaimedStartingRoute(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-complete-settles-route"
		sessionID   = "session-complete-settles-route"
		executionID = "execution-complete-settles-route"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateCreated)
	svc := testSupersededStartingRouteService(t, repo, taskID, sessionID, executionID)

	session, err := repo.GetTaskSession(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, session)

	svc.completeAndStopSession(ctx, taskID, session)

	routeState, err := repo.LoadRouteState(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, routeState)
	require.Equal(t, dynamicRouteStatusActionRequired, routeState.Status)
}
