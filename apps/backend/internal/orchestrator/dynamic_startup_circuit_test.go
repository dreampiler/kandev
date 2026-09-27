package orchestrator

import (
	"context"
	"errors"
	"testing"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// A process-start failure never reaches a classified ApplyFailure, so the
// callback must open the failed candidate's circuit directly. The next
// selection then skips the failed candidate instead of reselecting it.
func TestHandleAgentProcessStartFailedOpensCandidateCircuit(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-startup-circuit"
		sessionID   = "session-startup-circuit"
		executionID = "execution-startup-circuit"
		dynamicID   = "dynamic-startup-circuit"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateRunning)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, &mockAgentManager{})
	resolver := newWorkflowDynamicProfileResolverWithCandidates(t, dynamicID, []workflowDynamicCandidate{
		{executionProfileID: "candidate-1", enabled: true},
		{executionProfileID: "candidate-2", enabled: true},
	}, dynamicruntime.WithPersistence(repo))
	svc.SetProfileExecutionResolver(resolver)

	selected, err := resolver.Resolve(ctx, sessionID, dynamicID, 0, "")
	if err != nil {
		t.Fatalf("initial dynamic resolve: %v", err)
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

	svc.handleAgentProcessStartFailed(ctx, taskID, sessionID, executionID, errors.New("provider process failed"))

	state, err := repo.LoadRouteState(ctx, sessionID)
	if err != nil {
		t.Fatalf("LoadRouteState: %v", err)
	}
	if state == nil || state.Status != dynamicRouteStatusActionRequired {
		t.Fatalf("route state = %#v, want action_required", state)
	}

	next, err := resolver.Resolve(ctx, sessionID, dynamicID, selected.Generation, "")
	if err != nil {
		t.Fatalf("resolve after startup failure: %v", err)
	}
	if next.ExecutionProfileID != "candidate-2" {
		t.Fatalf("next candidate = %q, want candidate-2 because candidate-1's circuit is open", next.ExecutionProfileID)
	}
}
