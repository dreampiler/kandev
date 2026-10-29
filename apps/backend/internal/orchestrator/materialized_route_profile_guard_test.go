package orchestrator

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

const (
	materializedRouteTaskID         = "task-materialized-route"
	materializedRouteSessionID      = "session-materialized-route"
	materializedRouteLogicalProfile = "profile-dynamic-a"
	materializedRouteConcrete       = "profile-concrete-a"
)

func newMaterializedRouteService(t *testing.T) (*Service, *sqliterepo.Repository, *atomic.Int32) {
	t.Helper()
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, materializedRouteTaskID, materializedRouteSessionID, models.TaskSessionStateWaitingForInput)
	session, err := repo.GetTaskSession(ctx, materializedRouteSessionID)
	require.NoError(t, err)
	session.AgentProfileID = materializedRouteLogicalProfile
	session.ExecutionProfileID = materializedRouteConcrete
	session.RouteGeneration = 1
	require.NoError(t, repo.UpdateTaskSession(ctx, session))
	require.NoError(t, repo.SaveRouteState(ctx, dynamicruntime.RouteState{
		SessionID: materializedRouteSessionID, LogicalProfileID: materializedRouteLogicalProfile,
		ExecutionProfileID: materializedRouteConcrete, Generation: 1, ProfileVersion: 1,
		Status: "active", UpdatedAt: time.Now().UTC(),
	}))

	var launches atomic.Int32
	agentManager := &mockAgentManager{
		repoForExecutionLookup: repo,
		launchAgentFunc: func(context.Context, *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
			launches.Add(1)
			return &executor.LaunchAgentResponse{AgentExecutionID: "execution-materialized-route"}, nil
		},
	}
	schedulerRepo := newMockTaskRepo()
	schedulerRepo.tasks[materializedRouteTaskID] = &v1.Task{ID: materializedRouteTaskID, WorkspaceID: "ws1", Title: "Test Task"}
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), schedulerRepo, agentManager)
	return svc, repo, &launches
}

// Selecting a different logical profile on a session whose dynamic route is
// already materialized for another family is a session handoff, not a resume.
// Relabelling in place would show the new profile while the authoritative route
// keeps launching the previous family's concrete candidate.
func TestStartCreatedSession_RefusesProfileChangeOnMaterializedDynamicRoute(t *testing.T) {
	ctx := context.Background()
	svc, repo, launches := newMaterializedRouteService(t)

	_, err := svc.StartCreatedSession(ctx, materializedRouteTaskID, materializedRouteSessionID,
		"profile-dynamic-b", "continue", false, false, true, nil, nil)

	session, getErr := repo.GetTaskSession(ctx, materializedRouteSessionID)
	require.NoError(t, getErr)
	require.Equal(t, materializedRouteLogicalProfile, session.AgentProfileID,
		"the session label must keep the route's logical profile")
	require.Equal(t, materializedRouteConcrete, session.ExecutionProfileID)
	require.Zero(t, launches.Load(), "no agent may launch under the mismatched label")
	require.Error(t, err)
	state, stateErr := repo.LoadRouteState(ctx, materializedRouteSessionID)
	require.NoError(t, stateErr)
	require.Equal(t, materializedRouteLogicalProfile, state.LogicalProfileID)
	require.EqualValues(t, 1, state.Generation)
}

// The same logical profile resumes its route, and a session without a
// materialized route keeps its first profile selection.
func TestStartCreatedSession_MaterializedRouteGuardKeepsSameProfileAndFirstSelection(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newMaterializedRouteService(t)
	require.NoError(t, svc.refuseMaterializedRouteProfileChange(ctx, &models.TaskSession{
		ID: materializedRouteSessionID, AgentProfileID: materializedRouteLogicalProfile,
	}, materializedRouteLogicalProfile))

	unrouted := setupTestRepo(t)
	seedTaskAndSession(t, unrouted, "task-unrouted", "session-unrouted", models.TaskSessionStateCreated)
	unroutedSvc := createTestServiceWithScheduler(unrouted, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{})
	require.NoError(t, unroutedSvc.refuseMaterializedRouteProfileChange(ctx, &models.TaskSession{
		ID: "session-unrouted", AgentProfileID: "profile-first",
	}, "profile-second"))
}
