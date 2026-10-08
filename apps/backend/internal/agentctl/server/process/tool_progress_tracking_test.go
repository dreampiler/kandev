package process

import (
	"testing"

	"github.com/kandev/kandev/internal/agentctl/server/adapter"
	"github.com/kandev/kandev/internal/agentctl/server/config"
)

// TestRecordToolProgressTracksNonTerminalStatus verifies that a tool reported
// under a non-executing status label (for example OpenCode's "pending" while the
// command runs) is still observed, so ProbeToolProgress can supply the lifecycle
// with positive running evidence. A terminal update clears the observation.
func TestRecordToolProgressTracksNonTerminalStatus(t *testing.T) {
	mgr := NewManager(&config.InstanceConfig{WorkDir: t.TempDir()}, newTestLogger(t))
	key := toolProgressKey{"s1", "t1"}

	mgr.recordToolProgress(adapter.AgentEvent{
		Type: "tool_call", SessionID: "s1", ToolCallID: "t1", ToolStatus: "pending",
	})
	mgr.toolProgressMu.Lock()
	_, tracked := mgr.toolProgress[key]
	mgr.toolProgressMu.Unlock()
	if !tracked {
		t.Fatal("non-terminal tool was not tracked for progress probing")
	}

	mgr.recordToolProgress(adapter.AgentEvent{
		Type: "tool_update", SessionID: "s1", ToolCallID: "t1", ToolStatus: "completed",
	})
	mgr.toolProgressMu.Lock()
	_, stillTracked := mgr.toolProgress[key]
	mgr.toolProgressMu.Unlock()
	if stillTracked {
		t.Fatal("terminal tool remained tracked")
	}
}
