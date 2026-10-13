package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpprofile "github.com/kandev/kandev/internal/mcp/profile"
)

var officeCrossWorkspaceToolNames = []string{
	"list_workspaces_kandev",
	"list_workflows_kandev",
	"list_workflow_steps_kandev",
	"list_tasks_kandev",
	"get_task_conversation_kandev",
	"list_task_sessions_kandev",
	"message_task_kandev",
}

func TestOfficeCrossWorkspaceTools_OnlyWithCapability(t *testing.T) {
	off := NewWithProfile(&testBackend{}, "s", "office-task", 10005, newTestLogger(t), "", false,
		mcpprofile.New(mcpprofile.SurfaceOfficeTask, nil, nil))
	offTools := getRegisteredToolNames(off)
	for _, name := range officeCrossWorkspaceToolNames {
		assert.NotContains(t, offTools, name, "setting off must keep the Office catalog unchanged")
	}

	on := NewWithProfile(&testBackend{}, "s", "office-task", 10005, newTestLogger(t), "", false,
		mcpprofile.New(mcpprofile.SurfaceOfficeTask, []mcpprofile.Capability{mcpprofile.CapabilityOfficeCrossWorkspace}, nil))
	onTools := getRegisteredToolNames(on)
	for _, name := range officeCrossWorkspaceToolNames {
		require.Contains(t, onTools, name)
	}
	for _, control := range []string{"stop_task_kandev", "move_task_kandev", "update_task_kandev", "archive_task_kandev"} {
		assert.NotContains(t, onTools, control, "control actions stay with the target workspace's coordinator")
	}
}

func TestHandoffReportTool_OnlyOnKanbanWithCapability(t *testing.T) {
	plain := NewWithProfile(&testBackend{}, "s", "kanban-task", 10005, newTestLogger(t), "", false,
		mcpprofile.New(mcpprofile.SurfaceKanbanTask, nil, nil))
	assert.NotContains(t, getRegisteredToolNames(plain), "report_to_handoff_source_kandev")

	reached := NewWithProfile(&testBackend{}, "s", "kanban-task", 10005, newTestLogger(t), "", false,
		mcpprofile.New(mcpprofile.SurfaceKanbanTask, []mcpprofile.Capability{mcpprofile.CapabilityOfficeHandoffReport}, nil))
	assert.Contains(t, getRegisteredToolNames(reached), "report_to_handoff_source_kandev")

	office := NewWithProfile(&testBackend{}, "s", "office-task", 10005, newTestLogger(t), "", false,
		mcpprofile.New(mcpprofile.SurfaceOfficeTask, []mcpprofile.Capability{mcpprofile.CapabilityOfficeHandoffReport}, nil))
	assert.NotContains(t, getRegisteredToolNames(office), "report_to_handoff_source_kandev")
}
