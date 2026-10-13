package handlers

import (
	"context"

	"go.uber.org/zap"

	mcpprofile "github.com/kandev/kandev/internal/mcp/profile"
	mcpscope "github.com/kandev/kandev/internal/mcp/scope"
	"github.com/kandev/kandev/internal/task/dto"
	ws "github.com/kandev/kandev/pkg/websocket"
)

// workspaceTaskListPageSize bounds the workspace-scoped task list. It matches
// the largest page the sidebar list uses, so a controller sees the same tasks
// a person does in one call.
const workspaceTaskListPageSize = 200

// handleListWorkspaceTasks lists every non-archived, non-ephemeral task in a
// workspace, including tasks that belong to no workflow. A non-Office caller
// may only list its own workspace; cross-workspace listing stays on the Office
// reach path. Automation-origin tasks keep their existing provenance hiding and
// are still excluded by the underlying list contract.
func (h *Handlers) handleListWorkspaceTasks(ctx context.Context, msg *ws.Message, workspaceID string) (*ws.Message, error) {
	if principal, ok := mcpscope.PrincipalFromContext(ctx); ok &&
		principal.Surface != mcpprofile.SurfaceOfficeTask &&
		principal.WorkspaceID != "" && workspaceID != principal.WorkspaceID {
		return ws.NewError(msg.ID, msg.Action, ws.ErrorCodeForbidden,
			"listing another workspace's tasks requires the Office cross-workspace setting", nil)
	}
	tasks, total, err := h.taskSvc.ListTasksByWorkspace(
		ctx, workspaceID, "", "", "", 1, workspaceTaskListPageSize, "", false, false, false, false)
	if err != nil {
		h.logger.Error("failed to list workspace tasks", zap.Error(err))
		return ws.NewError(msg.ID, msg.Action, ws.ErrorCodeInternalError, "Failed to list tasks", nil)
	}
	dtos := make([]dto.TaskDTO, 0, len(tasks))
	for _, task := range tasks {
		dtos = append(dtos, dto.FromTask(task))
	}
	h.enrichTasksWithPendingActions(ctx, tasks, dtos)
	h.enrichTasksWithPRs(ctx, dtos)
	return ws.NewResponse(msg.ID, msg.Action, dto.ListTasksResponse{Tasks: dtos, Total: total})
}
