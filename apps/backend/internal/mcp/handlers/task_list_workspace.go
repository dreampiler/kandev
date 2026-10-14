package handlers

import (
	"context"

	"go.uber.org/zap"

	mcpprofile "github.com/kandev/kandev/internal/mcp/profile"
	mcpscope "github.com/kandev/kandev/internal/mcp/scope"
	"github.com/kandev/kandev/internal/task/dto"
	"github.com/kandev/kandev/internal/task/models"
	ws "github.com/kandev/kandev/pkg/websocket"
)

// workspaceTaskListPageSize bounds the workspace-scoped task list. It matches
// the largest page the sidebar list uses, so a controller sees the same tasks
// a person does in one call. The workspace form is a single page: tasks past
// this bound are counted in total but not returned.
const workspaceTaskListPageSize = 200

// handleListWorkspaceTasks lists every non-archived, non-ephemeral task in a
// workspace, including tasks that belong to no workflow. A controller principal
// (see callerIsWorkspaceController) also sees automation-run outputs; an
// ordinary task principal keeps the provenance-hiding list, so those rows stay
// out of a normal worker's view. A non-Office caller may only list its own
// workspace; cross-workspace listing stays on the Office reach path.
func (h *Handlers) handleListWorkspaceTasks(ctx context.Context, msg *ws.Message, workspaceID string) (*ws.Message, error) {
	if principal, ok := mcpscope.PrincipalFromContext(ctx); ok &&
		principal.Surface != mcpprofile.SurfaceOfficeTask &&
		principal.WorkspaceID != "" && workspaceID != principal.WorkspaceID {
		return ws.NewError(msg.ID, msg.Action, ws.ErrorCodeForbidden,
			"listing another workspace's tasks requires the Office cross-workspace setting", nil)
	}
	tasks, total, err := h.listWorkspaceTasks(ctx, workspaceID)
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

// listWorkspaceTasks selects the workspace-list variant for the caller. A
// controller principal reads through the inclusive contract so it can find and
// act on a board-less automation-run output; every other principal reads the
// provenance-hiding contract, so automation runs stay out of an ordinary
// worker's view. Both variants include workflow-less tasks.
func (h *Handlers) listWorkspaceTasks(ctx context.Context, workspaceID string) ([]*models.Task, int, error) {
	if h.callerIsWorkspaceController(ctx) {
		return h.taskSvc.ListTasksByWorkspaceIncludingAutomation(
			ctx, workspaceID, "", "", "", 1, workspaceTaskListPageSize, "", false, false, false, false, false)
	}
	return h.taskSvc.ListTasksByWorkspace(
		ctx, workspaceID, "", "", "", 1, workspaceTaskListPageSize, "", false, false, false, false)
}
