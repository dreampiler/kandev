package mcp

import (
	"context"
	"encoding/json"

	mcpprofile "github.com/kandev/kandev/internal/mcp/profile"
	ws "github.com/kandev/kandev/pkg/websocket"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// additionalToolGroups returns the tool groups that extend the base profile
// registry. The registry appends these after its own groups so the shared
// group list stays free of product-specific entries.
func (s *Server) additionalToolGroups() []profileToolGroup {
	external := surfaceEnabled(mcpprofile.SurfaceExternal)
	office := surfaceEnabled(mcpprofile.SurfaceOfficeTask)
	kanban := surfaceEnabled(mcpprofile.SurfaceKanbanTask)
	return []profileToolGroup{
		{name: "child-task-ordering", enabled: kanban, register: func(s *Server) { s.registerReorderChildTasksTool() }},
		// Its own kanban-only group, like child-task-ordering: the readback
		// resolves a send this session made, and the automation surface composes
		// from registerKanbanTools, so registering it there would silently widen
		// the fixed automation catalog.
		{name: "send-operation-readback", enabled: kanban, register: func(s *Server) { s.registerGetTaskMessageOperationTool() }},
		{name: "review-source", enabled: func(ctx mcpprofile.Context) bool { return kanban(ctx) || office(ctx) || external(ctx) }, register: func(s *Server) { s.registerReviewSourceTools() }},
		{name: "office-cross-workspace", enabled: andProfilePredicates(office, capabilityEnabled(mcpprofile.CapabilityOfficeCrossWorkspace)), register: func(s *Server) { s.registerOfficeCrossWorkspaceTools() }},
		{name: "office-handoff-report", enabled: andProfilePredicates(kanban, capabilityEnabled(mcpprofile.CapabilityOfficeHandoffReport)), register: func(s *Server) { s.registerHandoffReportTool() }},
	}
}

func (s *Server) registerListWorkspacesTool() {
	s.mcpServer.AddTool(
		mcp.NewToolWithRawSchema("list_workspaces_kandev",
			"List all workspaces. Use this first to get workspace IDs.",
			json.RawMessage(`{"type":"object","properties":{}}`),
		),
		s.wrapHandler("list_workspaces_kandev", s.listWorkspacesHandler()),
	)
}

func (s *Server) registerListWorkflowsTool() {
	s.mcpServer.AddTool(
		mcp.NewTool("list_workflows_kandev",
			mcp.WithDescription("List all workflows in a workspace. Omit workspace_id to list the workflows of the current task's own workspace."),
			mcp.WithString("workspace_id", mcp.Description("The workspace ID. Optional: defaults to the current task's workspace. Use list_workspaces_kandev to pick another one.")),
		),
		s.wrapHandler("list_workflows_kandev", s.listWorkflowsHandler()),
	)
}

func (s *Server) registerListWorkflowStepsTool() {
	s.mcpServer.AddTool(
		mcp.NewTool("list_workflow_steps_kandev",
			mcp.WithDescription("List all workflow steps in a workflow. Omit workflow_id to list the steps of the current task's own workflow."),
			mcp.WithString("workflow_id", mcp.Description("The workflow ID. Optional: defaults to the current task's workflow. Use list_workflows_kandev to pick another one.")),
		),
		s.wrapHandler("list_workflow_steps_kandev", s.listWorkflowStepsHandler()),
	)
}

func (s *Server) registerListTasksTool() {
	s.mcpServer.AddTool(
		mcp.NewTool("list_tasks_kandev",
			mcp.WithDescription("List all tasks in a workflow, or every task in a workspace when workspace_id is given. The workspace form also returns tasks that belong to no workflow, and a workspace controller additionally sees automation-run outputs; an ordinary task principal does not. The workspace form returns a single page of up to 200 tasks in the server's default order, with total reporting the full count. Each task includes its associated GitHub pull requests (number, url, title, state) under the \"prs\" field when any exist — use the PR state (open/closed/merged) to find tasks whose work has landed."),
			mcp.WithString("workflow_id", mcp.Description("The workflow ID. Optional: defaults to the current task's workflow. Use list_workflows_kandev to pick another one.")),
			mcp.WithString("workspace_id", mcp.Description("List every task in this workspace instead of one workflow's. Optional. A non-Office caller may only name its own workspace. When both workflow_id and workspace_id are given, workspace_id wins.")),
		),
		s.wrapHandler("list_tasks_kandev", s.listTasksHandler()),
	)
}

func (s *Server) registerMessageTaskTool() {
	s.mcpServer.AddTool(
		mcp.NewTool("message_task_kandev",
			mcp.WithDescription(`Send a prompt to an existing Kandev task session, not a native subagent. Use delivery_mode="queued" (default) for information that can wait, or delivery_mode="interrupt" for urgent replacement work on a running direct child; non-parent interrupts fail and if cancellation is unsafe, the prompt remains queued. Halt-only work uses stop_task_kandev. The primary session is used by default; if it is terminal, Kandev tries the newest session that can accept messages, or names spawn_session_kandev when none can. Pass reply_to_question_id for an autopilot child question, and operation_id to name a send. Returns "queued", "sent", or "started".`),
			mcp.WithString("task_id", mcp.Required(), mcp.Description("The target task's full UUID (not a truncated prefix)")),
			mcp.WithString("session_id", mcp.Description("Optional target session ID (must belong to task_id). Omit to message the task's primary session. Required when messaging a sibling session on your OWN task (task_id may then be your own task ID) — e.g. a session you spawned with spawn_session_kandev.")),
			mcp.WithString("prompt", mcp.Required(), mcp.Description("The message to deliver to the task's agent")),
			mcp.WithString("delivery_mode",
				mcp.Enum("queued", "interrupt"),
				mcp.DefaultString("queued"),
				mcp.Description(`How to deliver this message if the target is currently running/starting. "queued" (default): wait for the current turn to finish, like any other peer message. "interrupt": cancel the target's current turn now and deliver this message immediately instead — only allowed when you are the target task's direct parent; requesting "interrupt" as a non-parent is rejected with an error rather than silently queued.`),
			),
			mcp.WithString("reply_to_question_id", mcp.Description("Optional question ID from an autopilot child. When set, the direct parent answer is recorded against that pending question and the delivery is idempotent.")),
			mcp.WithString("operation_id", mcp.Description("Optional caller-chosen identity naming this one logical send (letters, digits, '.', '_', ':', '-'; 1-128 chars). Reuse the SAME value when retrying after a timeout or a lost response: the retry then returns the recorded outcome instead of delivering again. A request timeout (MCP -32001) is the client-side limit, not a delivery failure, and the backend may still complete the send; resolve the same value with get_task_message_operation_kandev instead of resending under a new one. A different send needs a different value; reusing one for different content is rejected with operation_id_conflict and delivers nothing.")),
		),
		s.wrapHandler("message_task_kandev", s.messageTaskHandler()),
	)
}

func (s *Server) registerGetTaskConversationTool() {
	s.mcpServer.AddTool(
		mcp.NewTool("get_task_conversation_kandev",
			mcp.WithDescription("Get conversation history for a task. If session_id is omitted, the primary session is used."),
			mcp.WithString("task_id", mcp.Description("The task ID. Optional: defaults to the current task.")),
			mcp.WithString("session_id", mcp.Description("Optional session ID (must belong to task_id)")),
			mcp.WithNumber("limit", mcp.Description("Optional page size (defaults to backend setting, max backend-capped)")),
			mcp.WithString("before", mcp.Description("Optional cursor message ID to fetch messages before this ID")),
			mcp.WithString("after", mcp.Description("Optional cursor message ID to fetch messages after this ID")),
			mcp.WithString("sort", mcp.Description("Optional sort order: asc or desc")),
			mcp.WithArray("message_types", mcp.Description("Optional message type filters (e.g. message, tool_call, error)"), mcp.Items(map[string]any{"type": "string"})),
		),
		s.wrapHandler("get_task_conversation_kandev", s.getTaskConversationHandler()),
	)
}

// registerReorderChildTasksTool registers reorder_child_tasks_kandev. It is a
// kanban-task-only group (not part of registerKanbanTools) so the automation
// catalog, which is composed from registerKanbanTools, does not gain it.
func (s *Server) registerReorderChildTasksTool() {
	s.mcpServer.AddTool(
		mcp.NewTool("reorder_child_tasks_kandev",
			mcp.WithDescription(`Set the processing order of your same-workspace direct children in one workflow step and band (admitted or queued). Use ordering for scheduling; use blocked_by only when a task consumes another task's output. placement="in_place" (default) permutes the named tasks within their current slots; placement="front" puts them ahead of the rest, preserving the remaining relative order. The board and queued admission use the new order. A membership conflict is retried once, then reported. Returns the band's task IDs and positions.`),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
			mcp.WithArray("ordered_task_ids", mcp.Required(), mcp.Items(map[string]any{"type": "string"}),
				mcp.Description("Your direct child task IDs (full UUIDs) in the desired order, first = processed first.")),
			mcp.WithString("placement",
				mcp.Enum("in_place", "front"),
				mcp.DefaultString("in_place"),
				mcp.Description(`"in_place" (default): reorder only within the named tasks' current slots. "front": move the named tasks to the head of their band.`)),
		),
		s.wrapHandler("reorder_child_tasks_kandev", s.reorderChildTasksHandler()),
	)
}

// registerGetTaskMessageOperationTool registers get_task_message_operation_kandev,
// the readback half of message_task_kandev's operation identity. It exists for
// exactly one situation: the send call timed out or lost its response and the
// caller must know whether the message landed before deciding to send again.
// It performs no delivery and is registered wherever message_task_kandev is.
func (s *Server) registerGetTaskMessageOperationTool() {
	s.mcpServer.AddTool(
		mcp.NewTool("get_task_message_operation_kandev",
			mcp.WithDescription(`Resolve a message_task_kandev operation_id to what actually happened, without delivering anything. Use it after a send timed out or its response was lost: the answer is state "committed" (delivered - do not resend), "pending" (the first attempt may still commit - wait and read again), "failed" (definitively not delivered - retry_safe is true, so resend under a FRESH operation_id), or claimed=false (no send was ever claimed under this identity, so sending again under it is safe). Only the session that made the send can read its outcome; it never returns prompt content.`),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
			mcp.WithString("operation_id", mcp.Required(), mcp.Description("The same operation_id passed to the message_task_kandev call whose outcome is unknown")),
		),
		s.wrapHandler("get_task_message_operation_kandev", s.getTaskMessageOperationHandler()),
	)
}

// listScopePayload builds the backend payload for a list tool whose scope
// argument is optional. An agent running inside a task holds neither its
// workspace_id nor its workflow_id, so an omitted scope is resolved from the
// calling task's own scope on the backend rather than rejected as a missing
// required argument. Returns a ready-to-render error message when neither an
// explicit value nor a calling task is available.
func (s *Server) listScopePayload(req mcp.CallToolRequest, field string) (map[string]string, string) {
	if value := req.GetString(field, ""); value != "" {
		return map[string]string{field: value}, ""
	}
	if s.taskID == "" {
		return nil, field + " is required (there is no current task to resolve it from)"
	}
	return map[string]string{"caller_task_id": s.taskID}, ""
}

// getTaskMessageOperationHandler reads one operation identity's recorded
// outcome. It delivers nothing, so it is the safe thing to call after a
// message_task_kandev timeout. Sender attribution is injected from the
// server's own task/session: an operation is readable only by the session that
// claimed it, and a caller cannot widen that by naming another task.
func (s *Server) getTaskMessageOperationHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		operationID, err := req.RequireString("operation_id")
		if err != nil {
			return mcp.NewToolResultError("operation_id is required"), nil
		}
		payload := map[string]interface{}{
			"task_id":           s.taskID,
			"operation_id":      operationID,
			"sender_task_id":    s.taskID,
			"sender_session_id": s.sessionID,
		}
		var result map[string]interface{}
		if err := s.backend.RequestPayload(ctx, ws.ActionMCPGetTaskMessageOperation, payload, &result); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	}
}

func (s *Server) reorderChildTasksHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		orderedIDs, err := req.RequireStringSlice("ordered_task_ids")
		if err != nil || len(orderedIDs) == 0 {
			return mcp.NewToolResultError("ordered_task_ids is required"), nil
		}
		// Build a fresh payload so callers cannot override trusted sender
		// attribution; the backend authorizes against sender_task_id.
		payload := map[string]interface{}{
			"ordered_task_ids": orderedIDs,
			"sender_task_id":   s.taskID,
		}
		copyOptionalStringArg(payload, req, "placement")
		var result map[string]interface{}
		if err := s.backend.RequestPayload(ctx, ws.ActionMCPReorderChildTasks, payload, &result); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	}
}
