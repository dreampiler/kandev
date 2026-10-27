package lifecycle

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/agent/agents"
	"github.com/kandev/kandev/internal/agentruntime"
)

// A workspace-only execution can be created before the dynamic conductor has
// selected a concrete candidate, so its AgentProfileID may still hold the
// logical (virtual) family. Promotion must re-bind it to the concrete execution
// profile the launch resolved, otherwise the later profile read-back resolves
// the virtual family and the agent start fails closed with ErrVirtualProfile.
func TestPromoteWorkspaceExecutionRebindsConcreteExecutionProfile(t *testing.T) {
	writeNativeOpenCodeVersionFixture(t, "1.18.32")
	m := newTestManager(t)
	m.profileResolver = &mockPassthroughProfileResolver{
		profiles: map[string]*AgentProfileInfo{
			"concrete-execution-profile": {AgentName: agents.OpenCodeACPAgentID},
		},
	}
	m.SetManagedRuntimeSelectionStore(nativeOpenCodeAdmissionManager("1.18.32").managedRuntimeSelections)

	execution := &AgentExecution{
		ID: "workspace-execution", TaskID: "task-1", SessionID: "session-1",
		RuntimeName: agentruntime.RuntimeStandalone, WorkspacePath: t.TempDir(),
		AgentProfileID: "logical-family-profile",
	}
	if err := m.executionStore.Add(execution); err != nil {
		t.Fatal(err)
	}
	req := &LaunchRequest{
		TaskID: execution.TaskID, SessionID: execution.SessionID,
		AgentProfileID: "logical-family-profile", ExecutionProfileID: "concrete-execution-profile",
		IsPassthrough: true,
	}
	if err := m.promoteWorkspaceExecution(context.Background(), execution, req); err != nil {
		t.Fatalf("promote workspace execution: %v", err)
	}
	if execution.AgentProfileID != "concrete-execution-profile" {
		t.Fatalf("execution.AgentProfileID = %q, want the concrete execution profile", execution.AgentProfileID)
	}
	if execution.OfficeAgentProfileID != "logical-family-profile" {
		t.Fatalf("execution.OfficeAgentProfileID = %q, want the logical Office profile", execution.OfficeAgentProfileID)
	}
}
