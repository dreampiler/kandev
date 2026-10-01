package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/agent/executor"
	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	"github.com/kandev/kandev/internal/agentctl/types/streams"
	"github.com/kandev/kandev/internal/events"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

func TestActiveUpdateStreamReattachRetriesSameExecutionWithoutPromptReplay(t *testing.T) {
	sm := NewStreamManager(newTestLogger(), StreamCallbacks{}, nil, nil)
	attempts := 0
	err := sm.retryActiveUpdateStream(context.Background(), func() bool { return true }, func() error {
		attempts++
		if attempts < 2 {
			return errors.New("stream dial failed")
		}
		return nil
	})
	if err != nil || attempts != 2 {
		t.Fatalf("reattach result = (%v, %d attempts), want success on second dial", err, attempts)
	}
}

func TestActiveUpdateStreamReattachStopsAfterBound(t *testing.T) {
	sm := NewStreamManager(newTestLogger(), StreamCallbacks{}, nil, nil)
	attempts := 0
	err := sm.retryActiveUpdateStream(context.Background(), func() bool { return true }, func() error {
		attempts++
		return errors.New("unreachable")
	})
	if err == nil || attempts != 3 {
		t.Fatalf("reattach result = (%v, %d attempts), want failure after three", err, attempts)
	}
}

func TestActiveUpdateStreamReattachExcludesIntentionalStop(t *testing.T) {
	sm := NewStreamManager(newTestLogger(), StreamCallbacks{}, nil, nil)
	attempts := 0
	err := sm.retryActiveUpdateStream(context.Background(), func() bool { return false }, func() error {
		attempts++
		return nil
	})
	if err == nil || attempts != 0 {
		t.Fatalf("intentional stop result = (%v, %d attempts), want no dial", err, attempts)
	}
}

func TestActiveUpdateStreamReattachOldReaderCannotOwnNewStream(t *testing.T) {
	execution := &AgentExecution{}
	old := execution.updateStreamGeneration.Add(1)
	execution.updateStreamGeneration.Add(1)
	if execution.ownsUpdateStream(old) {
		t.Fatal("old reader still owns replacement stream")
	}
}

func TestActiveUpdateStreamReattachOldClientCannotOwnReplacement(t *testing.T) {
	oldClient := &agentctl.Client{}
	execution := &AgentExecution{agentctl: oldClient}
	newClient := &agentctl.Client{}
	execution.replaceAgentctlClient(newClient)
	if execution.ownsAgentCtlClient(oldClient) {
		t.Fatal("old client's delayed disconnect still owns replacement")
	}
	if !execution.ownsAgentCtlClient(newClient) {
		t.Fatal("replacement client lost ownership")
	}
}

func TestActiveUpdateStreamReattachClaimsOnlyDispatchedPrompt(t *testing.T) {
	h := newWorkspaceEventsHarness(t)
	h.exec.Status = v1.AgentStatusRunning
	generation, err := h.mgr.BeginPrompt("exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if h.mgr.claimActiveStreamReattach(h.exec, generation) {
		t.Fatal("undispatched prompt was claimed for reattachment")
	}
	h.mgr.executionStore.MarkPromptDispatched(h.exec.ID, generation)
	if !h.mgr.claimActiveStreamReattach(h.exec, generation) {
		t.Fatal("dispatched prompt was not claimed")
	}
	if h.mgr.claimActiveStreamReattach(h.exec, generation) {
		t.Fatal("same prompt obtained a second reattachment budget")
	}
}

func TestActiveUpdateStreamReattachStopExcludesPrompt(t *testing.T) {
	h := newWorkspaceEventsHarness(t)
	generation, err := h.mgr.BeginPrompt("exec-1")
	if err != nil {
		t.Fatal(err)
	}
	h.mgr.executionStore.MarkPromptDispatched(h.exec.ID, generation)
	h.exec.stopRequested.Store(true)
	if h.mgr.claimActiveStreamReattach(h.exec, generation) {
		t.Fatal("stop-requested execution was claimed for reattachment")
	}
}

func TestActiveUpdateStreamReattachCancelExcludesPrompt(t *testing.T) {
	h := newWorkspaceEventsHarness(t)
	generation, err := h.mgr.BeginPrompt("exec-1")
	if err != nil {
		t.Fatal(err)
	}
	h.mgr.executionStore.MarkPromptDispatched(h.exec.ID, generation)
	h.exec.cancelRequested.Store(true)
	if h.mgr.claimActiveStreamReattach(h.exec, generation) {
		t.Fatal("cancel-requested turn was claimed for reattachment")
	}
	next, err := h.mgr.BeginPrompt("exec-1")
	if err != nil {
		t.Fatal(err)
	}
	h.mgr.executionStore.MarkPromptDispatched(h.exec.ID, next)
	if !h.mgr.claimActiveStreamReattach(h.exec, next) {
		t.Fatal("new prompt remained excluded after prior cancel")
	}
}

func TestActiveUpdateStreamReattachCancelStillSettlesDisconnect(t *testing.T) {
	h := newWorkspaceEventsHarness(t)
	h.exec.promptDoneCh = make(chan PromptCompletionSignal, 1)
	generation, err := h.mgr.BeginPrompt(h.exec.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.mgr.executionStore.MarkPromptDispatched(h.exec.ID, generation)
	h.exec.cancelRequested.Store(true)
	h.mgr.handleStreamDisconnectWithStartupGeneration(h.exec, errors.New("stream lost during cancel"), generation, h.exec.startupAttemptSnapshot())
	select {
	case signal := <-h.exec.promptDoneCh:
		if !signal.IsError || signal.PromptGeneration != generation {
			t.Fatalf("cancel disconnect signal = %#v", signal)
		}
	default:
		t.Fatal("cancelled prompt remained in flight after stream loss")
	}
}

func TestActiveUpdateStreamReattachKeepsPromptArmed(t *testing.T) {
	h := newWorkspaceEventsHarness(t)
	h.exec.Status = v1.AgentStatusRunning
	h.exec.agentctl = &agentctl.Client{}
	h.exec.promptDoneCh = make(chan PromptCompletionSignal, 1)
	generation, err := h.mgr.BeginPrompt(h.exec.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.mgr.executionStore.MarkPromptDispatched(h.exec.ID, generation)
	startup := h.exec.startupAttemptSnapshot()
	attempts := 0
	if !h.mgr.tryActiveStreamReattach(h.exec, generation, startup, func() error {
		attempts++
		return nil
	}) {
		t.Fatal("active prompt did not reattach")
	}
	if attempts != 1 || h.exec.Status != v1.AgentStatusRunning {
		t.Fatalf("reattach attempts=%d status=%s, want one dial and RUNNING", attempts, h.exec.Status)
	}
	select {
	case signal := <-h.exec.promptDoneCh:
		t.Fatalf("reattachment incorrectly completed prompt: %#v", signal)
	default:
	}
}

func TestActiveUpdateStreamReattachExhaustionUsesExistingFailure(t *testing.T) {
	h := newWorkspaceEventsHarness(t)
	h.exec.Status = v1.AgentStatusRunning
	h.exec.agentctl = &agentctl.Client{}
	h.exec.promptDoneCh = make(chan PromptCompletionSignal, 1)
	generation, err := h.mgr.BeginPrompt(h.exec.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.mgr.executionStore.MarkPromptDispatched(h.exec.ID, generation)
	startup := h.exec.startupAttemptSnapshot()
	attempts := 0
	if h.mgr.tryActiveStreamReattach(h.exec, generation, startup, func() error {
		attempts++
		return errors.New("unreachable")
	}) {
		t.Fatal("exhausted reattachment reported success")
	}
	if attempts != 3 {
		t.Fatalf("dial attempts = %d, want 3", attempts)
	}
	h.mgr.handleStreamDisconnectWithStartupGeneration(h.exec, errors.New("stream lost"), generation, startup)
	select {
	case signal := <-h.exec.promptDoneCh:
		if !signal.IsError || signal.PromptGeneration != generation {
			t.Fatalf("failure signal = %#v", signal)
		}
	default:
		t.Fatal("existing failure path did not settle prompt")
	}
}

func TestActiveUpdateStreamReattachAppliesRetainedOutcomeOnce(t *testing.T) {
	backend := &fakeTurnOutcomeBackend{MockExecutor: &MockExecutor{name: executor.NameStandalone}}
	mgr, eventBus := newTurnOutcomeTestManager(t, backend)
	backend.eventBus = eventBus
	execution := createTestExecution("exec-1", "task-1", "session-1")
	execution.RuntimeName = executor.NameStandalone
	execution.standaloneInstanceID = "instance-1"
	if err := mgr.executionStore.Add(execution); err != nil {
		t.Fatal(err)
	}
	generation, err := mgr.BeginPrompt(execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	mgr.executionStore.MarkPromptDispatched(execution.ID, generation)
	backend.outcome = &agentctl.TurnOutcome{
		TurnID: 9,
		Event:  streams.AgentEvent{Type: streams.EventTypeComplete, PromptGeneration: generation},
	}
	if err := mgr.reconcileActiveStreamTurnOutcome(execution, generation, execution.startupAttemptSnapshot()); err != nil {
		t.Fatal(err)
	}
	if !hasEventType(eventBus.PublishedEvents, events.AgentReady) {
		t.Fatal("retained completion did not settle the active prompt")
	}
	if len(backend.ackedCalls) != 1 || backend.publishedCountsAtAck[0] == 0 {
		t.Fatalf("outcome was not acknowledged after application: %#v", backend.ackedCalls)
	}
	before := len(eventBus.PublishedEvents)
	mgr.handleAgentEvent(execution, streams.AgentEvent{
		Type: streams.EventTypeComplete, PromptGeneration: generation, ControlTurnID: 9,
	})
	if len(eventBus.PublishedEvents) != before {
		t.Fatal("late live terminal event applied retained outcome twice")
	}
}
