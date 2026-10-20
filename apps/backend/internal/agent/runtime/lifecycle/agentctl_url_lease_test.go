package lifecycle

import (
	"testing"
	"time"

	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
)

// waitForPendingLifecycleWriter returns once a writer is queued on the
// execution's agentctl lifecycle lock. A pending writer makes every new read
// lease wait, which is the state a terminal stop creates.
func waitForPendingLifecycleWriter(t *testing.T, execution *AgentExecution) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for execution.agentctlLifecycleMu.TryRLock() {
		execution.agentctlLifecycleMu.RUnlock()
		if time.Now().After(deadline) {
			t.Fatal("lifecycle writer never became pending")
		}
		time.Sleep(time.Millisecond)
	}
}

// A caller that already holds the client read lease (recovered-prompt restore)
// persists the execution, and persistence reads AgentctlURL under the
// execution store lock. With a terminal stop waiting for the write side, a
// recursive read lease there blocks forever and stalls every store reader.
func TestAgentctlURLDoesNotWaitBehindPendingLifecycleWriter(t *testing.T) {
	client := agentctl.NewClient("127.0.0.1", 41001, newTestLogger())
	execution := &AgentExecution{ID: "exec-lease", agentctl: client}

	_, releaseLease := execution.AcquireAgentCtlClient()
	leaseReleased := false
	release := func() {
		if !leaseReleased {
			leaseReleased = true
			releaseLease()
		}
	}
	defer release()

	writerDone := make(chan struct{})
	go func() {
		execution.agentctlLifecycleMu.Lock()
		defer execution.agentctlLifecycleMu.Unlock()
		close(writerDone)
	}()
	waitForPendingLifecycleWriter(t, execution)

	urlCh := make(chan string, 1)
	go func() { urlCh <- execution.AgentctlURL() }()

	select {
	case got := <-urlCh:
		if got != client.BaseURL() {
			t.Fatalf("AgentctlURL() = %q, want %q", got, client.BaseURL())
		}
	case <-time.After(time.Second):
		release()
		<-urlCh
		<-writerDone
		t.Fatal("AgentctlURL blocked behind a pending lifecycle writer while the caller held a read lease")
	}

	release()
	select {
	case <-writerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycle writer did not proceed after the read lease was released")
	}
}

func TestAgentctlURLReadsOverrideAndEmptyClient(t *testing.T) {
	if got := (&AgentExecution{}).AgentctlURL(); got != "" {
		t.Fatalf("AgentctlURL() without a client = %q, want empty", got)
	}
	var nilExecution *AgentExecution
	if got := nilExecution.AgentctlURL(); got != "" {
		t.Fatalf("nil AgentctlURL() = %q, want empty", got)
	}

	original := agentctl.NewClient("127.0.0.1", 41001, newTestLogger())
	replacement := agentctl.NewClient("127.0.0.1", 41002, newTestLogger())
	execution := &AgentExecution{ID: "exec-override", agentctl: original}
	execution.replaceAgentctlClient(replacement)
	if got := execution.AgentctlURL(); got != replacement.BaseURL() {
		t.Fatalf("AgentctlURL() after replacement = %q, want %q", got, replacement.BaseURL())
	}
}
