package lifecycle

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// TestWaitForPromptDone_EscalatesAfterStallThreshold covers the unrecoverable
// stall path: with no agent activity for stallEscalationThreshold, the wait
// injects a synthetic cancel-release completion so the blocked SendPrompt
// returns ErrCancelEscalated, which the orchestrator turns into a session
// state revert that frees the AC-1 ceiling population.
func TestWaitForPromptDone_EscalatesAfterStallThreshold(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sm := NewSessionManager(newSessionTestLogger(), make(chan struct{}))
		execution := &AgentExecution{
			ID:           "test-exec",
			SessionID:    "test-session",
			promptDoneCh: make(chan PromptCompletionSignal, 1),
			Status:       v1.AgentStatusRunning,
		}
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
	})
}

// TestWaitForPromptDone_ActivityResetsEscalationClock pins the activity-based
// judgment: an agent that keeps producing events well past the threshold is
// never escalated, because elapsed time is measured from lastActivityAt, not
// from dispatch.
func TestWaitForPromptDone_ActivityResetsEscalationClock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sm := NewSessionManager(newSessionTestLogger(), make(chan struct{}))
		execution := &AgentExecution{
			ID:           "test-exec",
			SessionID:    "test-session",
			promptDoneCh: make(chan PromptCompletionSignal, 1),
			Status:       v1.AgentStatusRunning,
		}
		execution.lastActivityAt = time.Now()

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
					execution.lastActivityAtMu.Lock()
					execution.lastActivityAt = time.Now()
					execution.lastActivityAtMu.Unlock()
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

		cancel()
		if err := <-waitResult; !errors.Is(err, context.Canceled) {
			t.Fatalf("waitForPromptDone error = %v, want context canceled", err)
		}
	})
}
