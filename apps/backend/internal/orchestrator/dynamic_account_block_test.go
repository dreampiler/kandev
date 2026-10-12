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

const accountBlockedProviderError = `Error from provider meta-contributor: 403 Forbidden {"error":{"code":"user_blocked",` +
	`"message":"Your access has been restricted due to repeated policy violations."}}`

// An account restriction reported after the attempt already produced output
// stays with the operator: the output is preserved and no successor launches.
// The restricted profile must still be held, or the next selection would send
// requests to the blocked account again.
func TestAccountBlockedAfterOutputHoldsProfile(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-account-blocked-output"
		sessionID   = "session-account-blocked-output"
		executionID = "execution-blocked"
		dynamicID   = "dynamic-account-blocked"
		blocked     = "candidate-blocked"
		next        = "candidate-next"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateRunning)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, &mockAgentManager{})
	circuits := dynamicruntime.NewCircuitRegistry()
	resolver := newWorkflowDynamicProfileResolverWithCandidates(t, dynamicID, []workflowDynamicCandidate{
		{executionProfileID: blocked, enabled: true},
		{executionProfileID: next, enabled: true},
	}, dynamicruntime.WithPersistence(repo), dynamicruntime.WithCircuitRegistry(circuits))
	svc.SetProfileExecutionResolver(resolver)
	initial, err := resolver.Resolve(ctx, sessionID, dynamicID, 0, "")
	if err != nil || initial.ExecutionProfileID != blocked {
		t.Fatalf("initial resolve = %#v, %v; want %s", initial, err, blocked)
	}
	session, err := repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	session.AgentProfileID = dynamicID
	session.ExecutionProfileID = blocked
	session.RouteGeneration = initial.Generation
	session.RouteState = dynamicRouteStatusActive
	session.AgentExecutionID = executionID
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatalf("project route: %v", err)
	}
	data := watcher.AgentEventData{
		TaskID: taskID, SessionID: sessionID, AgentExecutionID: executionID,
		AgentProfileID: dynamicID, ExecutionProfileID: blocked, PromptGeneration: 1,
		ErrorMessage: accountBlockedProviderError, DynamicRouteAttempt: true,
		EvidenceKnown: true, OutputObserved: true,
	}
	classified := classifyKanbanFailure(data)
	if classified.Code != routingerr.CodeAccountBlocked {
		t.Fatalf("classified = %s (%s), want account_blocked", classified.Code, classified.ClassifierRule)
	}

	svc.routeDynamicAgentFailure(ctx, data, classified)

	now := time.Now()
	if got, ok := resolver.CandidateSuspension(ctx, blocked, now); !ok || !got.Blocked() ||
		got.Code != routingerr.CodeAccountBlocked || !got.Until.IsZero() {
		t.Fatalf("blocked suspension = %#v (resolved=%v), want an account-block hold", got, ok)
	}
	if got, ok := resolver.CandidateSuspension(ctx, next, now); !ok || got.Blocked() {
		t.Fatalf("next suspension = %#v (resolved=%v), want available", got, ok)
	}
	state, err := repo.LoadRouteState(ctx, sessionID)
	if err != nil {
		t.Fatalf("LoadRouteState: %v", err)
	}
	if state == nil || state.Generation != initial.Generation || state.ExecutionProfileID != blocked {
		t.Fatalf("route state = %#v, want generation %d still on %s", state, initial.Generation, blocked)
	}
}
