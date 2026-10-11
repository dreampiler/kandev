package orchestrator

import (
	"strings"
	"testing"

	"github.com/kandev/kandev/internal/agent/runtime/lifecycle"
)

func postStartErrorPayload(
	fixture unclassifiedWorkflowFenceFixture,
	executionID string,
	promptGeneration uint64,
	message string,
) *lifecycle.AgentStreamEventPayload {
	return &lifecycle.AgentStreamEventPayload{
		TaskID: fixture.taskID, SessionID: fixture.sessionID, ExecutionID: executionID,
		OwnerKind: lifecycle.ExecutionOwnerTask,
		Data: &lifecycle.AgentStreamEventData{
			Type: agentEventError, Error: message, PromptGeneration: promptGeneration,
		},
	}
}

// failTwicePostStart delivers two pre-result post-start failures on candidate-a
// with a manual retry between them, and returns the resulting route.
func failTwicePostStart(t *testing.T, first, second string) (string, string, int64) {
	t.Helper()
	fixture := newUnclassifiedWorkflowFenceFixture(t, false, false)
	t.Cleanup(fixture.svc.stopDynamicSuccessorWorkers)

	beginEmptyTurnPrompt(fixture, "execution-one", 1)
	fixture.svc.handleAgentStreamEvent(fixture.ctx, postStartErrorPayload(fixture, "execution-one", 1, first))
	fixture.resume(t, "execution-two")
	fixture.persistRunningExecution(t, "execution-two")
	beginEmptyTurnPrompt(fixture, "execution-two", 2)
	fixture.svc.handleAgentStreamEvent(fixture.ctx, postStartErrorPayload(fixture, "execution-two", 2, second))

	status, candidate, generation, _ := loadEmptyTurnPolicy(t, fixture)
	return status, candidate, generation
}

func TestPostStartProcessExitCountsUnclassifiedFailure(t *testing.T) {
	// Same exit code, different stderr: the stderr suffix is not part of the
	// identity, so the second failure reaches the threshold of two.
	_, candidate, generation := failTwicePostStart(t,
		"Agent process exited with code 1: first stderr line",
		"Agent process exited with code 1: a different stderr line",
	)
	if candidate != "candidate-b" || generation != 2 {
		t.Fatalf("route after two code-1 exits = %s/%d, want candidate-b/2", candidate, generation)
	}
}

func TestPostStartProcessExitDifferentCodesDoNotMatch(t *testing.T) {
	status, candidate, generation := failTwicePostStart(t,
		"Agent process exited with code 1: stderr",
		"Agent process exited with code 2: stderr",
	)
	if status != "action_required" || candidate != "candidate-a" {
		t.Fatalf("route after exits with codes 1 and 2 = %s/%s/%d, want action_required on candidate-a",
			status, candidate, generation)
	}
}

func TestPostStartGenericFailureCountsWithBoundedPrefix(t *testing.T) {
	// Messages that agree on their first 256 sanitized bytes share one
	// identity even when the tails differ.
	prefix := "provider response header timeout: " + strings.Repeat("upstream gateway did not answer ", 10)
	_, candidate, generation := failTwicePostStart(t, prefix+" request one", prefix+" request two")
	if candidate != "candidate-b" || generation != 2 {
		t.Fatalf("route after two generic failures with one prefix = %s/%d, want candidate-b/2", candidate, generation)
	}
}

func TestPostStartGenericFailuresWithDifferentPrefixesDoNotMatch(t *testing.T) {
	status, candidate, _ := failTwicePostStart(t,
		"provider response header timeout after 30s",
		"runtime execution error in tool sandbox",
	)
	if status != "action_required" || candidate != "candidate-a" {
		t.Fatalf("route after two different generic failures = %s/%s, want action_required on candidate-a", status, candidate)
	}
}

func TestPostStartProcessExitAfterOutputIsNotCounted(t *testing.T) {
	fixture := newUnclassifiedWorkflowFenceFixture(t, false, false)
	defer fixture.svc.stopDynamicSuccessorWorkers()
	for index, executionID := range []string{"execution-one", "execution-two"} {
		generation := uint64(index + 1)
		if index > 0 {
			fixture.resume(t, executionID)
			fixture.persistRunningExecution(t, executionID)
		}
		beginEmptyTurnPrompt(fixture, executionID, generation)
		fixture.svc.observePromptAttempt(fixture.sessionID, executionID, generation, true, false)
		fixture.svc.handleAgentStreamEvent(fixture.ctx, postStartErrorPayload(
			fixture, executionID, generation, "Agent process exited with code 1: stderr",
		))
	}
	_, candidate, generation, streak := loadEmptyTurnPolicy(t, fixture)
	if candidate != "candidate-a" || streak != nil {
		t.Fatalf("route after exits that followed output = %s/%d streak %+v, want candidate-a without a streak",
			candidate, generation, streak)
	}
}
