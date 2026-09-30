package lifecycle

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	"github.com/kandev/kandev/internal/events/bus"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// blockingStallBus records agent.stalled publishes and then holds them until
// release is closed, standing in for a synchronous stall handler that is stuck
// behind the session's cancel guard.
type blockingStallBus struct {
	MockEventBusWithTracking
	release <-chan struct{}
}

func (b *blockingStallBus) Publish(ctx context.Context, subject string, event *bus.Event) error {
	_ = b.MockEventBusWithTracking.Publish(ctx, subject, event)
	if subject == "agent.stalled" {
		<-b.release
	}
	return nil
}

// TestWaitForPromptDone_BlockedStallPublishStillEscalates: when the stall
// handler never returns, neither the five-minute advisory publish nor the
// terminal publish may keep the wait loop from releasing the prompt at the
// terminal threshold.
func TestWaitForPromptDone_BlockedStallPublishStillEscalates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		defer close(release)
		eventBus := &blockingStallBus{release: release}
		sm := NewSessionManager(newSessionTestLogger(), make(chan struct{}))
		sm.eventPublisher = NewEventPublisher(eventBus, newSessionTestLogger())
		execution := newTerminalStallExecution()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		waitResult := make(chan error, 1)
		go func() {
			_, err := sm.waitForPromptDone(ctx, execution, 7)
			waitResult <- err
		}()

		time.Sleep(stallEscalationThreshold + time.Minute)
		synctest.Wait()

		select {
		case err := <-waitResult:
			if !errors.Is(err, ErrCancelEscalated) {
				t.Fatalf("waitForPromptDone error = %v, want ErrCancelEscalated", err)
			}
		default:
			t.Fatal("waitForPromptDone stayed blocked behind a stall publish that never returned")
		}
		if got := countProlongedStallEvents(&eventBus.MockEventBusWithTracking); got != 1 {
			t.Fatalf("terminal classifications published = %d, want exactly 1", got)
		}
	})
}

// countProlongedStallEvents counts published agent.stalled events that carry
// the terminal discriminator, isolating them from the five-minute advisory
// publish that shares the same subject.
func countProlongedStallEvents(eventBus *MockEventBusWithTracking) int {
	eventBus.mu.Lock()
	defer eventBus.mu.Unlock()
	var count int
	for _, event := range eventBus.PublishedEvents {
		if event.Subject != "agent.stalled" {
			continue
		}
		payload, ok := event.Event.Data.(AgentStalledPayload)
		if !ok {
			continue
		}
		if payload.ProlongedStall {
			count++
		}
	}
	return count
}

func newTerminalStallExecution() *AgentExecution {
	execution := &AgentExecution{
		ID:           "test-exec",
		TaskID:       "test-task",
		SessionID:    "test-session",
		promptDoneCh: make(chan PromptCompletionSignal, 1),
		Status:       v1.AgentStatusRunning,
	}
	execution.lastActivityAt = time.Now()
	// The prompt produced a turn event, so it is a mid-work pause rather than a
	// never-started failure.
	execution.agentEventSincePrompt = true
	return execution
}

// TestWaitForPromptDone_PublishesTerminalClassificationOnce covers
// AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.1: a prompt that produced turn events
// and then stayed silent for the terminal threshold publishes the terminal
// classification exactly once for that generation and unblocks the waiter.
func TestWaitForPromptDone_PublishesTerminalClassificationOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eventBus := &MockEventBusWithTracking{}
		sm := NewSessionManager(newSessionTestLogger(), make(chan struct{}))
		sm.eventPublisher = NewEventPublisher(eventBus, newSessionTestLogger())
		execution := newTerminalStallExecution()

		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		waitResult := make(chan error, 1)
		go func() {
			_, err := sm.waitForPromptDone(ctx, execution, 7)
			waitResult <- err
		}()

		time.Sleep(stallEscalationThreshold + time.Minute)
		synctest.Wait()

		select {
		case err := <-waitResult:
			if !errors.Is(err, ErrCancelEscalated) {
				t.Fatalf("waitForPromptDone error = %v, want ErrCancelEscalated", err)
			}
		default:
			t.Fatal("waitForPromptDone did not return after the stall escalation threshold")
		}

		payload := lastStalledPayload(t, eventBus)
		if !payload.ProlongedStall {
			t.Fatal("ProlongedStall = false, want true for a terminal classification")
		}
		if payload.NeverStarted {
			t.Fatal("NeverStarted = true, want false for a prompt that produced turn events")
		}
		if got := countProlongedStallEvents(eventBus); got != 1 {
			t.Fatalf("terminal classifications published = %d, want exactly 1 per generation", got)
		}
	})
}

// TestWaitForPromptDone_ActivityPreventsTerminalClassification covers
// AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.3: a turn event inside the window
// advances the honest clock, so a prompt that keeps producing output is never
// classified terminal.
func TestWaitForPromptDone_ActivityPreventsTerminalClassification(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eventBus := &MockEventBusWithTracking{}
		sm := NewSessionManager(newSessionTestLogger(), make(chan struct{}))
		sm.eventPublisher = NewEventPublisher(eventBus, newSessionTestLogger())
		execution := newTerminalStallExecution()

		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		waitResult := make(chan error, 1)
		go func() {
			_, err := sm.waitForPromptDone(ctx, execution, 7)
			waitResult <- err
		}()

		stop := make(chan struct{})
		defer close(stop)
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					execution.markAgentActivity()
				case <-stop:
					return
				}
			}
		}()

		time.Sleep(2 * stallEscalationThreshold)
		synctest.Wait()

		select {
		case err := <-waitResult:
			t.Fatalf("waitForPromptDone returned %v despite continuing activity", err)
		default:
		}
		if got := countProlongedStallEvents(eventBus); got != 0 {
			t.Fatalf("terminal classifications published = %d, want 0 while the agent keeps producing events", got)
		}

		cancel()
		if err := <-waitResult; !errors.Is(err, context.Canceled) {
			t.Fatalf("waitForPromptDone error = %v, want context canceled", err)
		}
	})
}

// TestWaitForPromptDone_AdvisoryStallStaysAdvisory covers
// AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.4: inside the terminal window the
// five-minute notice is published as an advisory only, with no terminal
// classification.
func TestWaitForPromptDone_AdvisoryStallStaysAdvisory(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eventBus := &MockEventBusWithTracking{}
		sm := NewSessionManager(newSessionTestLogger(), make(chan struct{}))
		sm.eventPublisher = NewEventPublisher(eventBus, newSessionTestLogger())
		execution := newTerminalStallExecution()

		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		waitResult := make(chan error, 1)
		go func() {
			_, err := sm.waitForPromptDone(ctx, execution, 7)
			waitResult <- err
		}()

		time.Sleep(6 * time.Minute)
		synctest.Wait()

		select {
		case err := <-waitResult:
			t.Fatalf("waitForPromptDone returned %v inside the terminal window", err)
		default:
		}
		payload := lastStalledPayload(t, eventBus)
		if payload.ProlongedStall {
			t.Fatal("ProlongedStall = true inside the terminal window, want advisory only")
		}
		if payload.NeverStarted {
			t.Fatal("NeverStarted = true for a prompt that produced turn events")
		}
		if got := countProlongedStallEvents(eventBus); got != 0 {
			t.Fatalf("terminal classifications published = %d, want 0 before the terminal threshold", got)
		}

		cancel()
		if err := <-waitResult; !errors.Is(err, context.Canceled) {
			t.Fatalf("waitForPromptDone error = %v, want context canceled", err)
		}
	})
}

// TestWaitForPromptDone_NeverStartedNotClassifiedProlonged covers the design
// boundary: a prompt that never produced a turn event is terminal through the
// five-minute never-started branch and must not be reclassified as a prolonged
// stall at the terminal threshold.
func TestWaitForPromptDone_NeverStartedNotClassifiedProlonged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eventBus := &MockEventBusWithTracking{}
		sm := NewSessionManager(newSessionTestLogger(), make(chan struct{}))
		sm.eventPublisher = NewEventPublisher(eventBus, newSessionTestLogger())
		execution := &AgentExecution{
			ID:           "test-exec",
			TaskID:       "test-task",
			SessionID:    "test-session",
			promptDoneCh: make(chan PromptCompletionSignal, 1),
			Status:       v1.AgentStatusRunning,
		}
		// agentEventSincePrompt stays false: the agent never started.
		execution.lastActivityAt = time.Now()

		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		waitResult := make(chan error, 1)
		go func() {
			_, err := sm.waitForPromptDone(ctx, execution, 7)
			waitResult <- err
		}()

		time.Sleep(stallEscalationThreshold + time.Minute)
		synctest.Wait()

		select {
		case err := <-waitResult:
			if !errors.Is(err, ErrCancelEscalated) {
				t.Fatalf("waitForPromptDone error = %v, want ErrCancelEscalated", err)
			}
		default:
			t.Fatal("waitForPromptDone did not return after the stall escalation threshold")
		}
		if got := countProlongedStallEvents(eventBus); got != 0 {
			t.Fatalf("terminal classifications published = %d, want 0 for a never-started prompt", got)
		}
	})
}

// TestRecordActivity_TurnStartedBoundaryDoesNotAdvancePromptProgress pins the
// turnaround boundary against the honest clock: the adapter emits turn_started
// on every session/prompt dispatch, including synthetic wakeups, so it is a
// boundary frame rather than turn content and must not reset the inactivity
// clock.
func TestRecordActivity_TurnStartedBoundaryDoesNotAdvancePromptProgress(t *testing.T) {
	execution := &AgentExecution{
		ID:     "test-exec",
		Status: v1.AgentStatusRunning,
	}
	dispatchTime := time.Now().Add(-time.Minute)
	execution.lastActivityAt = dispatchTime

	mgr := &Manager{eventPublisher: NewEventPublisher(&MockEventBusWithTracking{}, newSessionTestLogger())}

	mgr.recordActivity(execution, agentctl.AgentEvent{Type: "turn_started"})

	lastActivity, agentEventSeen, _ := execution.promptActivitySnapshot()
	if !lastActivity.Equal(dispatchTime) {
		t.Fatalf("lastActivityAt = %v, want unchanged dispatch time %v after a turn_started boundary frame", lastActivity, dispatchTime)
	}
	if agentEventSeen {
		t.Fatal("agentEventSincePrompt = true after a turn_started boundary frame")
	}
}
