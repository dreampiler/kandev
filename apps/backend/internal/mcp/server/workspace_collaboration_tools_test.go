package mcp

import (
	"encoding/json"
	"testing"

	mcpprofile "github.com/kandev/kandev/internal/mcp/profile"
	"github.com/stretchr/testify/require"
)

func messageTaskDeliveryModes(t *testing.T, s *Server) ([]any, string) {
	t.Helper()
	tool, ok := s.mcpServer.ListTools()["message_task_kandev"]
	require.True(t, ok, "message_task_kandev must be registered")
	schema, err := json.Marshal(tool.Tool.InputSchema)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(schema, &decoded))
	properties, ok := decoded["properties"].(map[string]any)
	require.True(t, ok)
	deliveryMode, ok := properties["delivery_mode"].(map[string]any)
	require.True(t, ok)
	modes, ok := deliveryMode["enum"].([]any)
	require.True(t, ok)
	return modes, tool.Tool.Description
}

func TestMessageTaskTool_ReportModeRequiresCollaborationCapability(t *testing.T) {
	disabled := NewWithProfile(
		&testBackend{}, "session-1", "task-1", 10005, newTestLogger(t), "", false,
		mcpprofile.New(mcpprofile.SurfaceKanbanTask, nil, nil),
	)
	modes, description := messageTaskDeliveryModes(t, disabled)
	require.NotContains(t, modes, "report")
	require.NotContains(t, description, `delivery_mode="report"`)
	require.Contains(t, description, `Returns "queued", "sent", or "started".`)

	enabled := NewWithProfile(
		&testBackend{}, "session-1", "task-1", 10005, newTestLogger(t), "", false,
		mcpprofile.New(
			mcpprofile.SurfaceKanbanTask,
			[]mcpprofile.Capability{mcpprofile.CapabilityWorkspaceCollaborationReport},
			nil,
		),
	)
	modes, description = messageTaskDeliveryModes(t, enabled)
	require.Contains(t, modes, "report")
	require.Contains(t, description, `delivery_mode="report"`)
	require.Contains(t, description, `or "reported".`)
}
