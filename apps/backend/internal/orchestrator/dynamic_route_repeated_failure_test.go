package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/agent/runtime/routingpolicy"
	"github.com/kandev/kandev/internal/orchestrator/watcher"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

func repeatedFailureRulesJSON(t *testing.T, threshold int64) string {
	t.Helper()
	document := routingpolicy.DefaultDocument()
	document.Unclassified = routingpolicy.Policy{
		OnExhausted:     routingpolicy.OutcomeStop,
		RepeatedFailure: routingpolicy.RepeatedFailurePolicy{Enabled: true, Threshold: threshold},
	}
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal repeated failure policy: %v", err)
	}
	return string(payload)
}

// seedRepeatedFailureRoute claims generation 1 for a two-candidate dynamic
// profile whose first candidate carries the repeated-failure unclassified
// policy, then projects the claim onto the task session.
func seedRepeatedFailureRoute(
	t *testing.T,
	ctx context.Context,
	repo *sqliterepo.Repository,
	resolver *agentruntime.ProfileExecutionResolver,
	dynamicProfileID, sessionID, executionID string,
) {
	t.Helper()
	execution, err := resolver.Resolve(ctx, sessionID, dynamicProfileID, 0, "")
	if err != nil {
		t.Fatalf("resolve dynamic route: %v", err)
	}
	if execution.ExecutionProfileID != "first" {
		t.Fatalf("resolved first candidate = %q, want first", execution.ExecutionProfileID)
	}
	session, err := repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	session.AgentProfileID = dynamicProfileID
	session.ExecutionProfileID = execution.ExecutionProfileID
	session.RouteGeneration = execution.Generation
	session.RouteState = execution.Decision.Status
	session.AgentExecutionID = executionID
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatalf("UpdateTaskSession: %v", err)
	}
}

func unclassifiedRuntimeEvent(taskID, sessionID, executionID string) watcher.AgentEventData {
	return watcher.AgentEventData{
		TaskID: taskID, SessionID: sessionID, AgentExecutionID: executionID,
		PromptGeneration: 1,
	}
}

func unclassifiedRuntimeError() *routingerr.Error {
	return &routingerr.Error{
		Code: routingerr.CodeAgentRuntime, Class: routingerr.ClassUnclassified,
		CatalogueVersion: routingerr.CatalogueVersion, FallbackAllowed: false,
	}
}

func TestRouteDynamicAgentFailure_RepeatedUnclassifiedBelowThresholdStops(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-repeated-below-threshold"
		sessionID   = "session-repeated-below-threshold"
		executionID = "execution-repeated-below-threshold"
		dynamicID   = "dynamic-repeated-below-threshold"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateWaitingForInput)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, &mockAgentManager{})

	resolver := newWorkflowDynamicProfileResolverWithCandidates(t, dynamicID, []workflowDynamicCandidate{
		{executionProfileID: "first", enabled: true, rulesJSON: repeatedFailureRulesJSON(t, 2)},
		{executionProfileID: "second", enabled: true},
	}, dynamicruntime.WithPersistence(repo))
	svc.SetProfileExecutionResolver(resolver)
	seedRepeatedFailureRoute(t, ctx, repo, resolver, dynamicID, sessionID, executionID)

	svc.beginDynamicAttempt(sessionID)
	svc.bindDynamicAttemptExecution(sessionID, executionID)

	handled := svc.routeDynamicAgentFailure(ctx, unclassifiedRuntimeEvent(taskID, sessionID, executionID), unclassifiedRuntimeError())
	if handled {
		t.Fatal("below-threshold repeated failure must not claim to handle the failure")
	}

	state, err := repo.LoadRouteState(ctx, sessionID)
	if err != nil {
		t.Fatalf("LoadRouteState: %v", err)
	}
	if state == nil || state.Generation != 1 {
		t.Fatalf("route state = %#v, want generation 1", state)
	}
	if state.Status != "action_required" {
		t.Fatalf("route status = %q, want action_required", state.Status)
	}
	if !strings.Contains(state.PolicyStateJSON, "consecutive_failures") {
		t.Fatalf("policy state did not record the failure streak: %s", state.PolicyStateJSON)
	}
}

func TestRouteDynamicAgentFailure_RepeatedUnclassifiedFallsBackAtThreshold(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-repeated-at-threshold"
		sessionID   = "session-repeated-at-threshold"
		executionID = "execution-repeated-at-threshold"
		dynamicID   = "dynamic-repeated-at-threshold"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateWaitingForInput)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, &mockAgentManager{})

	resolver := newWorkflowDynamicProfileResolverWithCandidates(t, dynamicID, []workflowDynamicCandidate{
		{executionProfileID: "first", enabled: true, rulesJSON: repeatedFailureRulesJSON(t, 2)},
		{executionProfileID: "second", enabled: true},
	}, dynamicruntime.WithPersistence(repo))
	svc.SetProfileExecutionResolver(resolver)
	seedRepeatedFailureRoute(t, ctx, repo, resolver, dynamicID, sessionID, executionID)

	// First failure stays below the threshold.
	svc.beginDynamicAttempt(sessionID)
	svc.bindDynamicAttemptExecution(sessionID, executionID)
	svc.routeDynamicAgentFailure(ctx, unclassifiedRuntimeEvent(taskID, sessionID, executionID), unclassifiedRuntimeError())

	// Second failure on the same profile reaches the threshold and re-routes to
	// the next candidate.
	svc.beginDynamicAttempt(sessionID)
	svc.bindDynamicAttemptExecution(sessionID, executionID)
	svc.routeDynamicAgentFailure(ctx, unclassifiedRuntimeEvent(taskID, sessionID, executionID), unclassifiedRuntimeError())

	state, err := repo.LoadRouteState(ctx, sessionID)
	if err != nil {
		t.Fatalf("LoadRouteState: %v", err)
	}
	if state == nil || state.Generation != 2 {
		t.Fatalf("route state = %#v, want generation 2", state)
	}
	if state.ExecutionProfileID != "second" {
		t.Fatalf("route execution profile = %q, want second", state.ExecutionProfileID)
	}
}

func TestRouteDynamicAgentFailure_RepeatedUnclassifiedStepKillSwitchDeclines(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-repeated-kill-switch"
		sessionID   = "session-repeated-kill-switch"
		executionID = "execution-repeated-kill-switch"
		dynamicID   = "dynamic-repeated-kill-switch"
		stepID      = "step-repeated-kill-switch"
	)
	repo := setupTestRepo(t)
	seedSession(t, repo, taskID, sessionID, stepID)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)
	stepGetter := newMockStepGetter()
	disabled := false
	stepGetter.steps[stepID] = &wfmodels.WorkflowStep{
		ID: stepID, WorkflowID: "wf1", AllowRepeatedFailureFallback: &disabled,
	}
	svc := createTestServiceWithScheduler(repo, stepGetter, taskRepo, &mockAgentManager{})

	resolver := newWorkflowDynamicProfileResolverWithCandidates(t, dynamicID, []workflowDynamicCandidate{
		{executionProfileID: "first", enabled: true, rulesJSON: repeatedFailureRulesJSON(t, 1)},
		{executionProfileID: "second", enabled: true},
	}, dynamicruntime.WithPersistence(repo))
	svc.SetProfileExecutionResolver(resolver)
	seedRepeatedFailureRoute(t, ctx, repo, resolver, dynamicID, sessionID, executionID)

	svc.beginDynamicAttempt(sessionID)
	svc.bindDynamicAttemptExecution(sessionID, executionID)
	handled := svc.routeDynamicAgentFailure(ctx, unclassifiedRuntimeEvent(taskID, sessionID, executionID), unclassifiedRuntimeError())
	if handled {
		t.Fatal("a step that forbids repeated-failure fallback must not re-route")
	}

	state, err := repo.LoadRouteState(ctx, sessionID)
	if err != nil {
		t.Fatalf("LoadRouteState: %v", err)
	}
	if state == nil || state.Generation != 1 {
		t.Fatalf("route state = %#v, want generation 1 (no successor)", state)
	}
	if state.ExecutionProfileID != "first" {
		t.Fatalf("route execution profile = %q, want first", state.ExecutionProfileID)
	}
}
