package lifecycle

import (
	"testing"
	"time"

	client "github.com/kandev/kandev/internal/agent/runtime/agentctl"
)

func TestUpdateStreamStageSnapshotDoesNotWaitForStoppingLock(t *testing.T) {
	execution := &AgentExecution{}
	execution.agentctlLifecycleMu.Lock()
	defer execution.agentctlLifecycleMu.Unlock()
	done := make(chan bool, 1)
	go func() {
		_, available := tryUpdateStreamStageSnapshot(execution)
		done <- available
	}()
	select {
	case available := <-done:
		if available {
			t.Fatal("locked client reported an available stream")
		}
	case <-time.After(time.Second):
		t.Fatal("stage snapshot blocked behind stopping client lock")
	}
}

func TestUpdateStreamStageStaleHeartbeatLeavesUpstreamUnavailable(t *testing.T) {
	snapshot := client.UpdateStreamStageSnapshot{HeartbeatAgeMillis: 31_000}
	if age := upstreamStageAge(snapshot, 5); age != -1 {
		t.Fatalf("stale upstream stage age = %d, want unavailable", age)
	}
	snapshot.HeartbeatAgeMillis = 2_000
	if age := upstreamStageAge(snapshot, 5); age != 2_005 {
		t.Fatalf("fresh upstream stage age = %d, want 2005", age)
	}
}
