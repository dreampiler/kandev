package lifecycle

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// TestStallThreshold_ForegroundRunningKeepsToolAllowance pins the execution
// evidence rule: an open top-level tool whose ACP status label is not one of the
// executing labels still receives the 45-minute allowance once agentctl reports
// its foreground running, while a demonstrated exit and a non-running tool both
// keep the ordinary 15-minute policy.
func TestStallThreshold_ForegroundRunningKeepsToolAllowance(t *testing.T) {
	cases := []struct {
		name string
		tool activeTopLevelTool
		want time.Duration
	}{
		{
			name: "pending label without running evidence",
			tool: activeTopLevelTool{ToolCallID: "t1", Status: "pending"},
			want: stallEscalationThreshold,
		},
		{
			name: "pending label with running evidence",
			tool: activeTopLevelTool{ToolCallID: "t1", Status: "pending", ForegroundRunning: true},
			want: toolStallEscalationThreshold,
		},
		{
			name: "executing label",
			tool: activeTopLevelTool{ToolCallID: "t1", Status: "in_progress"},
			want: toolStallEscalationThreshold,
		},
		{
			name: "demonstrated exit overrides running evidence",
			tool: activeTopLevelTool{ToolCallID: "t1", Status: "in_progress", ForegroundRunning: true, ForegroundExited: true},
			want: stallEscalationThreshold,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			execution := &AgentExecution{openTools: map[string]activeTopLevelTool{tc.tool.ToolCallID: tc.tool}}
			if got := execution.stallThreshold(); got != tc.want {
				t.Fatalf("stallThreshold() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestWaitForPromptDone_ForegroundRunningExtendsEscalation verifies the running
// evidence survives the tick loop: the turn is not escalated at the ordinary
// 15-minute boundary and is only escalated past the tool allowance.
func TestWaitForPromptDone_ForegroundRunningExtendsEscalation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sm := NewSessionManager(newSessionTestLogger(), make(chan struct{}))
		execution := &AgentExecution{
			ID:           "test-exec",
			SessionID:    "test-session",
			promptDoneCh: make(chan PromptCompletionSignal, 1),
			Status:       v1.AgentStatusRunning,
			openTools: map[string]activeTopLevelTool{
				"t1": {ToolCallID: "t1", Status: "pending", ForegroundRunning: true},
			},
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
			t.Fatalf("waitForPromptDone escalated at the foreground boundary: %v", err)
		default:
		}

		time.Sleep(toolStallEscalationThreshold - stallEscalationThreshold + time.Minute)
		synctest.Wait()
		select {
		case err := <-waitResult:
			if !errors.Is(err, ErrCancelEscalated) {
				t.Fatalf("waitForPromptDone error = %v, want ErrCancelEscalated", err)
			}
		default:
			t.Fatal("waitForPromptDone did not escalate past the tool allowance")
		}
	})
}

// TestWaitForPromptDone_PendingClarificationSuppressesEscalation verifies that a
// turn awaiting a durable user answer is not treated as silence: no escalation
// fires while the request is pending, and a genuinely silent turn is escalated
// once the request is gone.
func TestWaitForPromptDone_PendingClarificationSuppressesEscalation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var pending atomic.Bool
		pending.Store(true)
		sm := NewSessionManager(newSessionTestLogger(), make(chan struct{}))
		sm.clarificationPending = func(context.Context, string) bool { return pending.Load() }
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

		time.Sleep(2 * stallEscalationThreshold)
		synctest.Wait()
		select {
		case err := <-waitResult:
			t.Fatalf("waitForPromptDone escalated a pending user-input turn: %v", err)
		default:
		}

		pending.Store(false)
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		select {
		case err := <-waitResult:
			if !errors.Is(err, ErrCancelEscalated) {
				t.Fatalf("waitForPromptDone error = %v, want ErrCancelEscalated", err)
			}
		default:
			t.Fatal("waitForPromptDone did not escalate after the user-input request cleared")
		}
	})
}
