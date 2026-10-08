package orchestrator

import (
	"context"
	"testing"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/orchestrator/watcher"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	"github.com/stretchr/testify/require"
)

// newPostOutputInterruptionFixture builds a two-candidate dynamic session whose
// first candidate already produced output, so a mid-turn provider failure can be
// routed to the second candidate when the attempt correlates.
func newPostOutputInterruptionFixture(
	t *testing.T,
	taskID, sessionID, executionID, dynamicID string,
	seedExecution bool,
) (*Service, *sqliterepo.Repository) {
	t.Helper()
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateRunning)
	if seedExecution {
		seedExecutorRunning(t, repo, sessionID, taskID, executionID)
	}
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, &mockAgentManager{})
	resolver := newWorkflowDynamicProfileResolverWithCandidates(t, dynamicID, []workflowDynamicCandidate{
		{executionProfileID: "candidate-1", enabled: true, rulesJSON: `{"on_provider_error":"try_next"}`},
		{executionProfileID: "candidate-2", enabled: true},
	}, dynamicruntime.WithPersistence(repo))
	svc.SetProfileExecutionResolver(resolver)
	selected, err := resolver.Resolve(ctx, sessionID, dynamicID, 0, "")
	if err != nil {
		t.Fatalf("resolve dynamic route: %v", err)
	}
	session, err := repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	session.AgentProfileID = dynamicID
	session.ExecutionProfileID = selected.ExecutionProfileID
	session.RouteGeneration = selected.Generation
	session.RouteState = selected.Decision.Status
	session.AgentExecutionID = executionID
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatalf("UpdateTaskSession: %v", err)
	}
	svc.beginDynamicAttempt(sessionID)
	svc.bindDynamicAttemptExecution(sessionID, executionID)
	svc.observePromptAttempt(sessionID, executionID, 1, true, false)
	return svc, repo
}

func postOutputProviderUnavailable() *routingerr.Error {
	return &routingerr.Error{
		Code: routingerr.CodeProviderUnavailable, Class: routingerr.ClassTransient,
		FallbackAllowed: true, AutoRetryable: true,
	}
}

// @covers AC-AGENTS-DYNAMIC-MIDTURN-001.1
func TestCurrentInterruptedDynamicAttempt_AcceptsMissingRouteGeneration(t *testing.T) {
	ctx := context.Background()
	const (
		taskID    = "t-midturn-stale-gen"
		sessionID = "s-midturn-stale-gen"
		execID    = "e-midturn-stale-gen"
		dynID     = "dyn-midturn-stale-gen"
	)
	svc, repo := newPostOutputInterruptionFixture(t, taskID, sessionID, execID, dynID, true)
	session, err := repo.GetTaskSession(ctx, sessionID)
	require.NoError(t, err)

	evidence, ok := svc.promptAttemptForSession(sessionID)
	require.True(t, ok)
	evidence.mu.Lock()
	evidence.routeGeneration = 0
	evidence.mu.Unlock()

	data := watcher.AgentEventData{SessionID: sessionID, AgentExecutionID: execID, PromptGeneration: 1}
	require.True(t, svc.currentInterruptedDynamicAttempt(data, session),
		"a current execution/prompt attempt must be accepted even when its recorded route generation is absent")
}

func TestCurrentInterruptedDynamicAttempt_AcceptsMissingSessionExecution(t *testing.T) {
	ctx := context.Background()
	const (
		taskID    = "t-midturn-no-exec"
		sessionID = "s-midturn-no-exec"
		execID    = "e-midturn-no-exec"
		dynID     = "dyn-midturn-no-exec"
	)
	svc, repo := newPostOutputInterruptionFixture(t, taskID, sessionID, execID, dynID, false)
	session, err := repo.GetTaskSession(ctx, sessionID)
	require.NoError(t, err)
	require.Empty(t, session.AgentExecutionID)

	data := watcher.AgentEventData{SessionID: sessionID, AgentExecutionID: execID, PromptGeneration: 1}
	require.True(t, svc.currentInterruptedDynamicAttempt(data, session),
		"the prompt-attempt execution identity must correlate even when the session projection lost its execution")
}

func TestCurrentInterruptedDynamicAttempt_RejectsDifferentExecution(t *testing.T) {
	ctx := context.Background()
	const (
		taskID    = "t-midturn-other-exec"
		sessionID = "s-midturn-other-exec"
		execID    = "e-midturn-other-exec"
		dynID     = "dyn-midturn-other-exec"
	)
	svc, repo := newPostOutputInterruptionFixture(t, taskID, sessionID, execID, dynID, true)
	session, err := repo.GetTaskSession(ctx, sessionID)
	require.NoError(t, err)

	data := watcher.AgentEventData{SessionID: sessionID, AgentExecutionID: "some-other-execution", PromptGeneration: 1}
	require.False(t, svc.currentInterruptedDynamicAttempt(data, session),
		"a failure from a different execution must never be treated as the current attempt")
}

// @covers AC-AGENTS-DYNAMIC-MIDTURN-001.1
func TestRouteDynamicAgentFailure_AdvancesAfterOutputWithoutSessionExecution(t *testing.T) {
	ctx := context.Background()
	const (
		taskID    = "t-midturn-advance"
		sessionID = "s-midturn-advance"
		execID    = "e-midturn-advance"
		dynID     = "dyn-midturn-advance"
	)
	svc, repo := newPostOutputInterruptionFixture(t, taskID, sessionID, execID, dynID, false)

	handled := svc.routeDynamicAgentFailure(ctx, watcher.AgentEventData{
		TaskID: taskID, SessionID: sessionID, OwnerKind: queueStatusScopeTask,
		AgentExecutionID: execID, PromptGeneration: 1,
	}, postOutputProviderUnavailable())
	require.True(t, handled, "a current post-output provider_unavailable must advance to the next candidate")

	waitForRouteGeneration(t, repo, sessionID, 2)
}

// @covers AC-AGENTS-DYNAMIC-MIDTURN-001.7
func TestRouteDynamicAgentFailure_DeclinesPostOutputWithoutCorrelation(t *testing.T) {
	ctx := context.Background()
	const (
		taskID    = "t-midturn-decline"
		sessionID = "s-midturn-decline"
		execID    = "e-midturn-decline"
		dynID     = "dyn-midturn-decline"
	)
	svc, repo := newPostOutputInterruptionFixture(t, taskID, sessionID, execID, dynID, true)

	handled := svc.routeDynamicAgentFailure(ctx, watcher.AgentEventData{
		TaskID: taskID, SessionID: sessionID, OwnerKind: queueStatusScopeTask,
		AgentExecutionID: execID,
	}, postOutputProviderUnavailable())
	require.False(t, handled, "an uncorrelated post-output failure must stay for manual recovery")

	state, err := repo.LoadRouteState(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, int64(1), state.Generation)
	require.Equal(t, dynamicRouteStatusActionRequired, state.Status)
}

// @covers AC-AGENTS-DYNAMIC-MIDTURN-001.7
func TestRouteDynamicAgentFailure_DoesNotAdvanceAgainOnRepeatedFailure(t *testing.T) {
	ctx := context.Background()
	const (
		taskID    = "t-midturn-repeat"
		sessionID = "s-midturn-repeat"
		execID    = "e-midturn-repeat"
		dynID     = "dyn-midturn-repeat"
	)
	svc, repo := newPostOutputInterruptionFixture(t, taskID, sessionID, execID, dynID, true)
	data := watcher.AgentEventData{
		TaskID: taskID, SessionID: sessionID, OwnerKind: queueStatusScopeTask,
		AgentExecutionID: execID, PromptGeneration: 1,
	}

	require.True(t, svc.routeDynamicAgentFailure(ctx, data, postOutputProviderUnavailable()))
	waitForRouteGeneration(t, repo, sessionID, 2)

	// The route already moved past this attempt, so a repeated failure is
	// accounted for without claiming another successor generation.
	svc.routeDynamicAgentFailure(ctx, data, postOutputProviderUnavailable())
	requireRouteGenerationStays(t, repo, sessionID, 2)
}

// @covers AC-AGENTS-DYNAMIC-MIDTURN-001.7
func TestRouteDynamicAgentFailure_RejectsSupersededAttemptWithoutRouteGeneration(t *testing.T) {
	ctx := context.Background()
	const (
		taskID    = "t-midturn-superseded"
		sessionID = "s-midturn-superseded"
		execID    = "e-midturn-superseded"
		dynID     = "dyn-midturn-superseded"
	)
	svc, repo := newPostOutputInterruptionFixture(t, taskID, sessionID, execID, dynID, true)
	data := watcher.AgentEventData{
		TaskID: taskID, SessionID: sessionID, OwnerKind: queueStatusScopeTask,
		AgentExecutionID: execID, PromptGeneration: 1,
	}
	require.True(t, svc.routeDynamicAgentFailure(ctx, data, postOutputProviderUnavailable()))
	waitForRouteGeneration(t, repo, sessionID, 2)

	// The attempt advanced the route without recording its route generation; the
	// captured execution profile still proves the attempt is superseded.
	evidence, ok := svc.promptAttemptForSession(sessionID)
	require.True(t, ok)
	evidence.mu.Lock()
	evidence.routeGeneration = 0
	evidence.mu.Unlock()

	require.False(t, svc.routeDynamicAgentFailure(ctx, data, postOutputProviderUnavailable()),
		"an attempt the route already moved past must not advance a successor again")
	requireRouteGenerationStays(t, repo, sessionID, 2)
}

func requireRouteGenerationStays(t *testing.T, repo *sqliterepo.Repository, sessionID string, want int64) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		state, err := repo.LoadRouteState(ctx, sessionID)
		require.NoError(t, err)
		require.NotNil(t, state)
		require.Equal(t, want, state.Generation, "route must not advance past the claimed successor")
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForRouteGeneration(t *testing.T, repo *sqliterepo.Repository, sessionID string, want int64) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(3 * time.Second)
	for {
		state, err := repo.LoadRouteState(ctx, sessionID)
		if err != nil {
			t.Fatalf("LoadRouteState: %v", err)
		}
		if state != nil && state.Generation == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("route state = %#v, want generation %d", state, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
