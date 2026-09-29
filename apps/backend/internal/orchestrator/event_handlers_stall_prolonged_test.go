package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/lifecycle"
	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// prolongedStallPayload describes a prompt that produced turn events and then
// went silent past the terminal inactivity threshold.
func prolongedStallPayload() lifecycle.AgentStalledPayload {
	return lifecycle.AgentStalledPayload{
		AgentExecutionID: "execution-1",
		TaskID:           "task-1",
		SessionID:        "session-1",
		PromptGeneration: 7,
		ActivityEpoch:    1,
		ProlongedStall:   true,
	}
}

// TestHandleAgentStalled_ProlongedStallRecordsAndStops covers
// AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.2: a terminal prolonged-stall
// classification records the session and task FAILED and issues exactly one
// forced teardown of the payload's execution.
func TestHandleAgentStalled_ProlongedStallRecordsAndStops(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "task-1", "session-1", "step-1")
	stopCalls := make(chan stopAgentCall, 1)
	agentMgr := &mockAgentManager{
		repoForExecutionLookup:   repo,
		currentPromptExecutionID: "execution-1",
		stopAgentWithReasonFunc: func(_ context.Context, executionID, reason string, force bool) error {
			stopCalls <- stopAgentCall{ExecutionID: executionID, Reason: reason, Force: force}
			return nil
		},
	}
	agentMgr.currentPromptGeneration.Store(7)
	agentMgr.currentPromptActivityEpoch.Store(1)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, "task-1", v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, agentMgr)
	svc.turnService = &repoTurnService{repo: repo}
	activeTurn, err := svc.turnService.StartTurn(ctx, "session-1")
	if err != nil {
		t.Fatalf("start active turn: %v", err)
	}
	messages := &mockMessageCreator{}
	svc.messageCreator = messages

	svc.handleAgentStalled(ctx, prolongedStallPayload())

	if len(messages.sessionMessages) != 1 {
		t.Fatalf("session messages = %d, want 1", len(messages.sessionMessages))
	}
	message := messages.sessionMessages[0]
	if message.messageType != string(v1.MessageTypeError) {
		t.Fatalf("message type = %q, want error", message.messageType)
	}
	if message.content != prolongedStallNoticeContent {
		t.Fatalf("notice content = %q, want %q", message.content, prolongedStallNoticeContent)
	}
	if message.turnID != activeTurn.ID {
		t.Fatalf("notice turn ID = %q, want active turn %q", message.turnID, activeTurn.ID)
	}
	if _, hasVisibility := message.metadata["action_visibility"]; hasVisibility {
		t.Fatalf("terminal notice has running visibility metadata: %#v", message.metadata)
	}
	if _, hasActions := message.metadata["actions"]; hasActions {
		t.Fatalf("terminal notice has a running cancel action: %#v", message.metadata)
	}

	call := waitForStopAgentCall(t, stopCalls)
	if call.ExecutionID != "execution-1" {
		t.Fatalf("stopped execution = %q, want execution-1", call.ExecutionID)
	}
	if !call.Force {
		t.Fatal("teardown stop was not forced")
	}

	agentMgr.mu.Lock()
	callCount := len(agentMgr.stopAgentWithReasonArgs)
	agentMgr.mu.Unlock()
	if callCount != 1 {
		t.Fatalf("StopAgentWithReason calls = %d, want 1", callCount)
	}

	after, err := repo.GetTaskSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("get session after handling stall: %v", err)
	}
	if after.State != models.TaskSessionStateFailed {
		t.Fatalf("session state = %q, want FAILED", after.State)
	}
	if after.ErrorMessage != errAgentProlongedStall.Error() {
		t.Fatalf("session error message = %q, want %q", after.ErrorMessage, errAgentProlongedStall.Error())
	}

	taskRepo.mu.Lock()
	taskState := taskRepo.updatedStates["task-1"]
	taskRepo.mu.Unlock()
	if taskState != v1.TaskStateFailed {
		t.Fatalf("task state = %q, want FAILED", taskState)
	}
}

// TestHandleAgentStalled_ProlongedStallKeepsTerminalStateWhenStopFails covers
// AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.2: a teardown failure must not
// overwrite the recorded terminal state or its message.
func TestHandleAgentStalled_ProlongedStallKeepsTerminalStateWhenStopFails(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "task-1", "session-1", "step-1")
	stopAttempted := make(chan struct{})
	agentMgr := &mockAgentManager{
		repoForExecutionLookup:   repo,
		currentPromptExecutionID: "execution-1",
		stopAgentWithReasonFunc: func(context.Context, string, string, bool) error {
			defer close(stopAttempted)
			return errors.New("agentctl unreachable")
		},
	}
	agentMgr.currentPromptGeneration.Store(7)
	agentMgr.currentPromptActivityEpoch.Store(1)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, "task-1", v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, agentMgr)
	svc.turnService = &repoTurnService{repo: repo}
	if _, err := svc.turnService.StartTurn(ctx, "session-1"); err != nil {
		t.Fatalf("start active turn: %v", err)
	}
	svc.messageCreator = &mockMessageCreator{}

	svc.handleAgentStalled(ctx, prolongedStallPayload())

	select {
	case <-stopAttempted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the detached teardown attempt")
	}

	after, err := repo.GetTaskSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("get session after handling stall: %v", err)
	}
	if after.State != models.TaskSessionStateFailed {
		t.Fatalf("session state = %q, want FAILED despite teardown failure", after.State)
	}
	if after.ErrorMessage != errAgentProlongedStall.Error() {
		t.Fatalf("session error message = %q, want %q", after.ErrorMessage, errAgentProlongedStall.Error())
	}
}

// TestHandleAgentStalled_ProlongedStallDoesNotAdvanceStep covers
// AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.5: the terminal settlement only
// reclaims the prompt and its ceiling slot; it must not consult or advance the
// workflow step.
func TestHandleAgentStalled_ProlongedStallDoesNotAdvanceStep(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "task-1", "session-1", "step-1")
	agentMgr := &mockAgentManager{
		repoForExecutionLookup:   repo,
		currentPromptExecutionID: "execution-1",
	}
	agentMgr.currentPromptGeneration.Store(7)
	agentMgr.currentPromptActivityEpoch.Store(1)
	stepGetter := newMockStepGetter()
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, "task-1", v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, stepGetter, taskRepo, agentMgr)
	svc.turnService = &repoTurnService{repo: repo}
	if _, err := svc.turnService.StartTurn(ctx, "session-1"); err != nil {
		t.Fatalf("start active turn: %v", err)
	}
	svc.messageCreator = &mockMessageCreator{}

	svc.handleAgentStalled(ctx, prolongedStallPayload())

	if calls := stepGetter.GetStepCalls(); calls != 0 {
		t.Fatalf("GetStep calls = %d, want 0: a prolonged stall must not advance the workflow step", calls)
	}
	after, err := repo.GetTaskSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("get session after handling stall: %v", err)
	}
	if after.State != models.TaskSessionStateFailed {
		t.Fatalf("session state = %q, want FAILED", after.State)
	}
	taskRepo.mu.Lock()
	taskState := taskRepo.updatedStates["task-1"]
	taskRepo.mu.Unlock()
	if taskState != v1.TaskStateFailed {
		t.Fatalf("task state = %q, want FAILED (not advanced)", taskState)
	}
}

// TestStallNoticeContentNamesProlongedCondition pins the terminal copy for the
// prolonged-stall classification, independent of tool metadata.
func TestStallNoticeContentNamesProlongedCondition(t *testing.T) {
	payload := lifecycle.AgentStalledPayload{ProlongedStall: true, ToolName: "shell", ToolTitle: "Start dev server"}
	if got := stallNoticeContent(payload); got != prolongedStallNoticeContent {
		t.Fatalf("stallNoticeContent() = %q, want %q", got, prolongedStallNoticeContent)
	}
	if strings.TrimSpace(prolongedStallNoticeContent) == "" {
		t.Fatal("prolonged stall notice content must not be blank")
	}
}
