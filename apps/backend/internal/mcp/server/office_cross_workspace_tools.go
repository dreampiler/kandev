package mcp

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	ws "github.com/kandev/kandev/pkg/websocket"
)

// registerOfficeCrossWorkspaceTools adds the cross-workspace read and
// message tools to an Office task session whose workspace turned on
// cross-workspace access (CapabilityOfficeCrossWorkspace). They are the
// Kanban tools of the same names; the backend's Office guard decides reach
// on every call, so a session that outlives the setting being turned off
// is refused rather than trusted.
//
// get_task_plan_kandev is already on the Office surface (the plan group).
// stop, move and priority tools are deliberately absent: the target
// workspace's coordinator owns control actions.
func (s *Server) registerOfficeCrossWorkspaceTools() {
	s.mcpServer.AddTool(
		mcp.NewToolWithRawSchema("list_workspaces_kandev",
			"List the workspaces this Office workspace can reach through cross-workspace access, including its own. "+
				"Each other workspace carries its coordinator: the task to address with message_task_kandev "+
				"when you need something done there (source \"setting\" or \"workspace_coordinator\"; \"none\" means no coordinator is configured).",
			json.RawMessage(`{"type":"object","properties":{}}`),
		),
		s.wrapHandler("list_workspaces_kandev", s.listWorkspacesHandler()),
	)
	s.registerListWorkflowsTool()
	s.registerListWorkflowStepsTool()
	s.registerListTasksTool()
	s.registerGetTaskConversationTool()
	s.registerListTaskSessionsTool()
	s.registerMessageTaskTool()
}

// registerHandoffReportTool adds report_to_handoff_source_kandev to a Kanban
// task session whose workspace receives handoffs from an Office workspace
// with cross-workspace access on (CapabilityOfficeHandoffReport).
func (s *Server) registerHandoffReportTool() {
	s.mcpServer.AddTool(
		mcp.NewTool("report_to_handoff_source_kandev",
			mcp.WithDescription(`Report progress or the result of a task that an Office agent handed off to this workspace back to the Office task that handed it off. The report becomes a comment on that Office task and wakes its assignee. task_id defaults to the current task; a workspace coordinator can pass the handed-off task's ID instead. Fails when the task was not handed off by an Office task, or when that Office workspace has cross-workspace access turned off.`),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
			mcp.WithString(mcpKeyTaskID, mcp.Description("The handed-off task's ID. Optional: defaults to the current task.")),
			mcp.WithString("body", mcp.Required(), mcp.Description("The report for the Office task's assignee")),
		),
		s.wrapHandler("report_to_handoff_source_kandev", s.reportToHandoffSourceHandler()),
	)
}

func (s *Server) reportToHandoffSourceHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body, err := req.RequireString("body")
		if err != nil || body == "" {
			return mcp.NewToolResultError("body is required"), nil
		}
		taskID := req.GetString(mcpKeyTaskID, "")
		if taskID == "" {
			taskID = s.taskID
		}
		if taskID == "" {
			return mcp.NewToolResultError("task_id is required: this session has no current task"), nil
		}
		payload := map[string]interface{}{
			mcpKeyTaskID: taskID,
			"body":       body,
		}
		var result map[string]interface{}
		if err := s.backend.RequestPayload(ctx, ws.ActionMCPReportToHandoffSource, payload, &result); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	}
}
