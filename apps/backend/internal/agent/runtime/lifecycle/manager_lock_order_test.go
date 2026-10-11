package lifecycle

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// lockOrderDeadlockGuard only bounds a hung test; the paths finish in
// microseconds once the store reader is released.
const lockOrderDeadlockGuard = 5 * time.Second

// goroutineParkedIn reports whether a goroutine whose stack contains frame is
// blocked on a sync primitive.
func goroutineParkedIn(frame string) bool {
	buffer := make([]byte, 4<<20)
	for _, stack := range strings.Split(string(buffer[:runtime.Stack(buffer, true)]), "\n\n") {
		header, _, _ := strings.Cut(stack, "\n")
		if strings.Contains(header, "[sync.") && strings.Contains(stack, frame) {
			return true
		}
	}
	return false
}

func newLockOrderTestExecution() *AgentExecution {
	execution := &AgentExecution{
		ID:             "exec-lock-order",
		TaskID:         "task-lock-order",
		SessionID:      "session-lock-order",
		AgentProfileID: "profile-1",
		Status:         v1.AgentStatusRunning,
		WorkspacePath:  "/workspace",
		promptDoneCh:   make(chan PromptCompletionSignal, 1),
	}
	execution.promptGeneration = 1
	execution.setPromptTurnID("turn-lock-order")
	return execution
}

// TestMarkReadyDoesNotDeadlockWithPromptCompletion covers the store-locked
// paths that build an AgentEventPayload. The payload snapshots promptTurnID
// through promptLifecycleMu, while a completion claim holds promptLifecycleMu
// and then waits for the execution store lock. If a payload path takes the
// store lock first, the two block each other forever and every
// GetBySessionID caller blocks behind the store lock.
//
// A held store reader makes each payload path queue for the store lock before
// the completion takes promptLifecycleMu, which is the interleaving observed
// when a cancel escalation raced the agent's late completion.
func TestMarkReadyDoesNotDeadlockWithPromptCompletion(t *testing.T) {
	cases := []struct {
		name    string
		frame   string
		prepare func(*AgentExecution)
		run     func(*Manager, *AgentExecution)
	}{
		{
			name:  "cancel escalation marks ready",
			frame: "escalateStuckCancel",
			run: func(m *Manager, execution *AgentExecution) {
				_ = m.escalateStuckCancel(context.Background(), execution, nil)
			},
		},
		{
			name:  "passthrough input marks running",
			frame: "PreparePassthroughRunning",
			prepare: func(execution *AgentExecution) {
				execution.PassthroughProcessID = "pty-lock-order"
				execution.Status = v1.AgentStatusReady
			},
			run: func(m *Manager, execution *AgentExecution) {
				_, _ = m.PreparePassthroughRunning(execution.SessionID)
			},
		},
		{
			name:  "recovered execution becomes available",
			frame: "prepareRecoveredAvailability",
			run: func(m *Manager, execution *AgentExecution) {
				_, _ = m.prepareRecoveredAvailability(context.Background(), execution)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := newTestManager(t)
			execution := newLockOrderTestExecution()
			if tc.prepare != nil {
				tc.prepare(execution)
			}
			require.NoError(t, mgr.executionStore.Add(execution))
			assertNoLockOrderDeadlock(t, mgr, execution, tc.frame, tc.run)
		})
	}
}

func assertNoLockOrderDeadlock(
	t *testing.T,
	mgr *Manager,
	execution *AgentExecution,
	frame string,
	run func(*Manager, *AgentExecution),
) {
	t.Helper()
	mgr.executionStore.mu.RLock()
	readerHeld := true
	releaseReader := func() {
		if readerHeld {
			readerHeld = false
			mgr.executionStore.mu.RUnlock()
		}
	}
	t.Cleanup(releaseReader)

	payloadDone := make(chan struct{})
	go func() {
		defer close(payloadDone)
		run(mgr, execution)
	}()
	require.Eventually(t, func() bool { return goroutineParkedIn(frame) },
		lockOrderDeadlockGuard, time.Millisecond, "payload path never waited for the store lock")

	completionDone := make(chan struct{})
	go func() {
		defer close(completionDone)
		claim, _ := mgr.claimPromptCompletion(execution, &agentctl.AgentEvent{
			Type:             "complete",
			PromptGeneration: 1,
		}, false)
		if claim.locked {
			execution.promptLifecycleMu.Unlock()
		}
	}()
	require.Eventually(t, func() bool { return goroutineParkedIn("claimPromptCompletion") },
		lockOrderDeadlockGuard, time.Millisecond, "completion claim never waited for a lock")

	releaseReader()
	for _, done := range []<-chan struct{}{payloadDone, completionDone} {
		select {
		case <-done:
		case <-time.After(lockOrderDeadlockGuard):
			t.Fatalf("%s and the prompt completion claim deadlocked on promptLifecycleMu and the execution store lock", frame)
		}
	}
	_, found := mgr.executionStore.GetBySessionID("another-session")
	require.False(t, found)
}
