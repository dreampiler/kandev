package orchestrator

import (
	"context"
	"testing"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/orchestrator/watcher"
)

func midTurnProviderUnavailable() *routingerr.Error {
	return &routingerr.Error{
		Code: routingerr.CodeProviderUnavailable, Class: routingerr.ClassTransient,
		Confidence: routingerr.ConfHigh, FallbackAllowed: true, AutoRetryable: true,
	}
}

// With escalation enabled, a mid-turn failure counts against the candidate,
// and a later turn on the same route that completes cleanly resets the count.
// A completion reported for a different candidate leaves it unchanged.
//
// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.20
func TestDynamicCleanTurnCompletionResetsCircuitFailures(t *testing.T) {
	const (
		taskID      = "task-circuit-completion-reset"
		sessionID   = "session-circuit-completion-reset"
		executionID = "execution-circuit-completion-reset"
	)
	ctx := context.Background()
	route := setupMidTurnCircuitRoute(t, taskID, sessionID, executionID, midTurnFailing,
		dynamicruntime.WithCircuitBackoffEscalation(true))
	key := dynamicruntime.ResourceKey(dynamicruntime.ScopeProfile, midTurnFailing)

	route.svc.routeDynamicAgentFailure(ctx,
		midTurnFailureEvent(taskID, sessionID, executionID, midTurnFailing), midTurnProviderUnavailable())
	if got := route.circuits.Failures(key); got != 1 {
		t.Fatalf("failures after a mid-turn failure = %d, want 1", got)
	}
	session, err := route.repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}

	route.svc.resetDynamicCircuitFailuresForCompletion(ctx, watcher.AgentEventData{
		TaskID: taskID, SessionID: sessionID, AgentExecutionID: executionID, ExecutionProfileID: midTurnNext,
	}, session)
	if got := route.circuits.Failures(key); got != 1 {
		t.Fatalf("a completion of another candidate changed the count to %d", got)
	}

	route.svc.resetDynamicCircuitFailuresForCompletion(ctx, watcher.AgentEventData{
		TaskID: taskID, SessionID: sessionID, AgentExecutionID: executionID, ExecutionProfileID: midTurnFailing,
	}, session)
	if got := route.circuits.Failures(key); got != 0 {
		t.Fatalf("failures after a clean turn = %d, want 0", got)
	}
}

// Disabled, the mid-turn failure keeps the fixed backoff with no count.
//
// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.20
func TestDynamicCircuitFailuresStayZeroWithoutEscalation(t *testing.T) {
	const (
		taskID      = "task-circuit-no-escalation"
		sessionID   = "session-circuit-no-escalation"
		executionID = "execution-circuit-no-escalation"
	)
	ctx := context.Background()
	route := setupMidTurnCircuitRoute(t, taskID, sessionID, executionID, midTurnFailing)
	route.svc.routeDynamicAgentFailure(ctx,
		midTurnFailureEvent(taskID, sessionID, executionID, midTurnFailing), midTurnProviderUnavailable())
	if !route.open(midTurnFailing) {
		t.Fatal("the mid-turn failure did not open the circuit")
	}
	if got := route.circuits.Failures(dynamicruntime.ResourceKey(dynamicruntime.ScopeProfile, midTurnFailing)); got != 0 {
		t.Fatalf("failures = %d, want 0 while escalation is disabled", got)
	}
}
