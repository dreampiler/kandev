package orchestrator

import (
	"context"
	"testing"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/orchestrator/watcher"
	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// A dynamic route moved from claude to sol, but the sol launch was deferred by
// the session ceiling, so the claude execution kept serving the session. A
// later failure of that claude execution must not suspend sol or advance the
// route past it: sol never ran.
func TestDynamicFailureOfSupersededCandidateDoesNotSuspendCurrentCandidate(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-dynamic-superseded-failure"
		sessionID   = "session-dynamic-superseded-failure"
		executionID = "execution-claude-still-serving"
		dynamicID   = "dynamic-superseded-failure"
		claude      = "candidate-claude"
		sol         = "candidate-sol"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateRunning)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, &mockAgentManager{})
	resolver := newWorkflowDynamicProfileResolverWithCandidates(t, dynamicID, []workflowDynamicCandidate{
		{executionProfileID: claude, enabled: true, rulesJSON: `{"on_provider_error":"try_next"}`},
		{executionProfileID: sol, enabled: true, rulesJSON: `{"on_provider_error":"try_next"}`},
		{executionProfileID: "candidate-nemotron", enabled: true},
	}, dynamicruntime.WithPersistence(repo))
	svc.SetProfileExecutionResolver(resolver)

	initial, err := resolver.Resolve(ctx, sessionID, dynamicID, 0, "")
	if err != nil || initial.ExecutionProfileID != claude {
		t.Fatalf("initial resolve = %#v, %v; want %s", initial, err, claude)
	}
	rateLimited := &routingerr.Error{
		Code: routingerr.CodeRateLimited, Class: routingerr.ClassTransient,
		Confidence: routingerr.ConfHigh, FallbackAllowed: true, AutoRetryable: true,
	}
	moved, err := resolver.ResolveExecutionAfterFailure(ctx, sessionID, dynamicID, claude, initial.Generation, rateLimited)
	if err != nil || moved.ExecutionProfileID != sol {
		t.Fatalf("route after the first claude failure = %#v, %v; want %s", moved, err, sol)
	}
	session, err := repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	session.AgentProfileID = dynamicID
	session.ExecutionProfileID = moved.ExecutionProfileID
	session.RouteGeneration = moved.Generation
	session.RouteState = dynamicRouteStatusActive
	session.AgentExecutionID = executionID
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatalf("project route: %v", err)
	}
	// The next queued prompt still reaches the claude execution.
	svc.beginDynamicAttempt(sessionID)
	svc.bindDynamicAttemptExecution(sessionID, executionID)

	svc.routeDynamicAgentFailure(ctx, watcher.AgentEventData{
		TaskID: taskID, SessionID: sessionID, AgentExecutionID: executionID,
		AgentProfileID: dynamicID, ExecutionProfileID: claude, PromptGeneration: 1,
	}, rateLimited)

	now := time.Now()
	if got, ok := resolver.CandidateSuspension(ctx, sol, now); !ok || got.Blocked() {
		t.Fatalf("sol suspension after a claude failure = %#v (resolved=%v), want available", got, ok)
	}
	if got, ok := resolver.CandidateSuspension(ctx, claude, now); !ok || !got.Blocked() {
		t.Fatalf("claude suspension = %#v (resolved=%v), want the failure kept on claude", got, ok)
	}
	state, err := repo.LoadRouteState(ctx, sessionID)
	if err != nil {
		t.Fatalf("LoadRouteState: %v", err)
	}
	if state == nil || state.Generation != moved.Generation || state.ExecutionProfileID != sol {
		t.Fatalf("route state = %#v, want generation %d still on %s", state, moved.Generation, sol)
	}
}

// The route claimed sol while the claude execution kept serving the session.
// Claude's first real output is claude's success: it must clear claude's strike
// history and leave sol's untouched, although the prompt began after the route
// already named sol.
func TestDynamicOutputSuccessBelongsToTheExecutionsOwnProfile(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-dynamic-output-attribution"
		sessionID   = "session-dynamic-output-attribution"
		executionID = "execution-claude-output"
		dynamicID   = "dynamic-output-attribution"
		claude      = "candidate-claude"
		sol         = "candidate-sol"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateRunning)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, &mockAgentManager{})
	now := time.Now()
	circuits := dynamicruntime.NewCircuitRegistry(dynamicruntime.WithCircuitClock(func() time.Time { return now }))
	resolver := newWorkflowDynamicProfileResolverWithCandidates(t, dynamicID, []workflowDynamicCandidate{
		{executionProfileID: claude, enabled: true},
		{executionProfileID: sol, enabled: true},
	}, dynamicruntime.WithPersistence(repo), dynamicruntime.WithCircuitRegistry(circuits))
	svc.SetProfileExecutionResolver(resolver)
	session, err := repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	session.AgentProfileID = dynamicID
	session.ExecutionProfileID = sol
	session.RouteGeneration = 2
	session.AgentExecutionID = executionID
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatalf("project route: %v", err)
	}
	claudeKey := dynamicruntime.ResourceKey(dynamicruntime.ScopeProfile, claude)
	solKey := dynamicruntime.ResourceKey(dynamicruntime.ScopeProfile, sol)
	circuits.Open(claudeKey, now.Add(-time.Minute), routingerr.CodeRateLimited)
	circuits.Open(solKey, now.Add(-time.Minute), routingerr.CodeRateLimited)
	svc.beginPromptAttempt(sessionID, executionID, 1, true)

	svc.recordDynamicResourceOutput(ctx, sessionID, executionID, claude, 1)

	if got := circuits.Inspect(claudeKey, now); got.Strikes != 0 || got.State != dynamicruntime.ResourceAvailable {
		t.Fatalf("claude after its own output = %#v, want cleared", got)
	}
	if got := circuits.Inspect(solKey, now); got.Strikes != 1 {
		t.Fatalf("sol after claude's output = %#v, want its strike kept", got)
	}
}
