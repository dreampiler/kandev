package mcp

import (
	"context"
	"encoding/json"

	ws "github.com/kandev/kandev/pkg/websocket"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func (s *Server) registerReviewSourceTools() {
	s.mcpServer.AddTool(mcp.NewTool("run_code_review_kandev",
		mcp.WithDescription("Start an independent code review using a model different from the writer. Optional worktree_path and base_ref select a backend-local Git worktree; head_ref selects a committed three-dot range, otherwise review the working tree against base_ref. Without source fields, use task changes or the saved review source. Read results with get_code_review_kandev."),
		mcp.WithString("task_id", mcp.Description("Target task, defaulting to your current task.")),
		mcp.WithString("agent_profile_id", mcp.Description("Optional reviewer profile. The writer's model remains excluded.")),
		mcp.WithString("repository_id", mcp.Description("Optional repository scope for the Kandev workspace.")),
		mcp.WithString("worktree_path", mcp.Description("Absolute local Git worktree path on the backend host.")),
		mcp.WithString("base_ref", mcp.Description("Comparison base commit or ref, required with worktree_path.")),
		mcp.WithString("head_ref", mcp.Description("Optional committed head ref for base_ref...head_ref.")),
	), s.wrapHandler("run_code_review_kandev", s.taskReviewActionHandler(ws.ActionTaskReviewRun)))
	s.mcpServer.AddTool(mcp.NewTool("set_review_source_kandev",
		mcp.WithDescription("Save a task's local Git review source for workflow step-entry and manual review when the Kandev workspace has no changes. Set it before entering the review step. Use clear=true to remove it. This does not launch a review or change the writer/reviewer model rule."),
		mcp.WithString("task_id", mcp.Description("Target task, defaulting to your current task.")),
		mcp.WithString("worktree_path", mcp.Description("Absolute local Git worktree path on the backend host.")),
		mcp.WithString("base_ref", mcp.Description("Comparison base commit or ref, required unless clearing.")),
		mcp.WithString("head_ref", mcp.Description("Optional committed head ref for base_ref...head_ref.")),
		mcp.WithBoolean("clear", mcp.Description("Remove the saved review source. Omit source fields when true.")),
	), s.wrapHandler("set_review_source_kandev", s.taskReviewActionHandler(ws.ActionTaskReviewSourceSet)))
	s.mcpServer.AddTool(mcp.NewTool("get_code_review_kandev",
		mcp.WithDescription("Read the task's stored code-review runs, findings, and failure reasons. Use after run_code_review_kandev or workflow step-entry review; pending/running means results are not yet final."),
		mcp.WithString("task_id", mcp.Description("Target task, defaulting to your current task.")),
	), s.wrapHandler("get_code_review_kandev", s.taskReviewActionHandler(ws.ActionTaskReviewGet)))
}

func (s *Server) taskReviewActionHandler(action string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		taskID, err := s.resolveTaskID(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		payload := map[string]interface{}{"task_id": taskID}
		for _, key := range []string{"worktree_path", "base_ref", "head_ref", "agent_profile_id", "repository_id"} {
			if value, exists := req.GetArguments()[key]; exists {
				payload[key] = value
			}
		}
		if value, exists := req.GetArguments()["clear"]; exists {
			payload["clear"] = value
		}
		if action == ws.ActionTaskReviewRun && taskID == s.taskID && s.sessionID != "" {
			payload["session_id"] = s.sessionID
		}
		var result map[string]interface{}
		if err := s.backend.RequestPayload(ctx, action, payload, &result); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, err := json.Marshal(result)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	}
}
