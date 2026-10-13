package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"go.uber.org/zap"

	mcpprofile "github.com/kandev/kandev/internal/mcp/profile"
	mcpscope "github.com/kandev/kandev/internal/mcp/scope"
	"github.com/kandev/kandev/internal/office/crossworkspace"
	"github.com/kandev/kandev/internal/task/models"
	ws "github.com/kandev/kandev/pkg/websocket"
)

// OfficeCrossWorkspace is the Office cross-workspace access decision point
// (internal/office/crossworkspace). Optional: nil keeps every Office and
// Kanban MCP path exactly as it is without the setting.
type OfficeCrossWorkspace interface {
	Reachable(ctx context.Context, officeWorkspaceID, targetWorkspaceID string) error
	ListReachable(ctx context.Context, officeWorkspaceID string) (*crossworkspace.Directory, error)
	DeliverToOfficeTask(ctx context.Context, sender, target *models.Task, body string) (*crossworkspace.OfficeDelivery, bool, error)
	ReportToHandoffSource(ctx context.Context, callerWorkspaceID, deliveryTaskID, body string) (*crossworkspace.OfficeDelivery, error)
}

// SetOfficeCrossWorkspace wires the Office cross-workspace access setting.
func (h *Handlers) SetOfficeCrossWorkspace(x OfficeCrossWorkspace) {
	h.officeCrossWorkspace = x
}

// officeCrossWorkspaceTarget names how a guarded action identifies the
// workspace it reads or writes.
type officeCrossWorkspaceTarget int

const (
	targetByWorkspaceID officeCrossWorkspaceTarget = iota
	targetByWorkflowID
	targetByTaskID
)

// officeCrossWorkspaceActions are the actions CapabilityOfficeCrossWorkspace
// adds to the Office surface, keyed to how each names its target. Discovery
// is not an authorization boundary, so the guard re-decides reach for every
// Office principal call, whether or not its session advertised the tool.
var officeCrossWorkspaceActions = map[string]officeCrossWorkspaceTarget{
	ws.ActionMCPListWorkflows:       targetByWorkspaceID,
	ws.ActionMCPListWorkflowSteps:   targetByWorkflowID,
	ws.ActionMCPListTasks:           targetByWorkflowID,
	ws.ActionMCPGetTaskConversation: targetByTaskID,
	ws.ActionMCPListTaskSessions:    targetByTaskID,
	ws.ActionMCPMessageTask:         targetByTaskID,
	// Plan reads are promised to a reached workspace (AC-OFFICE-XWS-003.2);
	// listing and fetching revisions use the same task_id. Plan writes, move,
	// and delete stay out: cross-workspace control belongs to each target
	// workspace's coordinator (AC-OFFICE-XWS-004.3).
	ws.ActionMCPGetTaskPlan:           targetByTaskID,
	ws.ActionMCPListTaskPlanRevisions: targetByTaskID,
	ws.ActionMCPGetTaskPlanRevision:   targetByTaskID,
}

// authorizeOfficeCrossWorkspaceRequest confines an Office task principal to
// the workspaces its Office workspace can reach. Calls from any other
// principal, and Office calls to actions outside the cross-workspace set,
// pass through unchanged.
func (h *Handlers) authorizeOfficeCrossWorkspaceRequest(ctx context.Context, msg *ws.Message) (*ws.Message, *ws.Message, error) {
	principal, ok := mcpscope.PrincipalFromContext(ctx)
	if !ok || principal.Surface != mcpprofile.SurfaceOfficeTask || principal.WorkspaceID == "" {
		return nil, msg, nil
	}
	if msg.Action == ws.ActionMCPListWorkspaces {
		return h.officeWorkspaceDirectory(ctx, principal, msg)
	}
	kind, guarded := officeCrossWorkspaceActions[msg.Action]
	if !guarded {
		return nil, msg, nil
	}
	target, resolveErr := h.officeCrossWorkspaceTargetWorkspace(ctx, principal, msg, kind)
	if resolveErr != nil {
		return resolveErr, nil, nil
	}
	if target == principal.WorkspaceID {
		return nil, msg, nil
	}
	if h.officeCrossWorkspace == nil {
		return wsError(msg.ID, msg.Action, ws.ErrorCodeForbidden, crossworkspace.ErrDisabled.Error()), nil, nil
	}
	if err := h.officeCrossWorkspace.Reachable(ctx, principal.WorkspaceID, target); err != nil {
		return officeCrossWorkspaceError(msg, err), nil, nil
	}
	return nil, msg, nil
}

// officeWorkspaceDirectory answers list_workspaces_kandev for an Office
// principal with its own workspace plus, when the setting is on, every
// reachable workspace and its coordinator.
func (h *Handlers) officeWorkspaceDirectory(
	ctx context.Context, principal mcpscope.Principal, msg *ws.Message,
) (*ws.Message, *ws.Message, error) {
	if h.officeCrossWorkspace == nil {
		return nil, msg, nil
	}
	dir, err := h.officeCrossWorkspace.ListReachable(ctx, principal.WorkspaceID)
	if err != nil {
		return officeCrossWorkspaceError(msg, err), nil, nil
	}
	resp, err := ws.NewResponse(msg.ID, msg.Action, dir)
	return resp, nil, err
}

// officeCrossWorkspaceTargetWorkspace resolves the workspace a guarded
// action targets. An omitted id means the caller's own scope, which the
// handlers resolve from the calling task the same way.
func (h *Handlers) officeCrossWorkspaceTargetWorkspace(
	ctx context.Context, principal mcpscope.Principal, msg *ws.Message, kind officeCrossWorkspaceTarget,
) (string, *ws.Message) {
	var payload map[string]interface{}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return "", wsError(msg.ID, msg.Action, ws.ErrorCodeBadRequest, "Invalid payload: "+err.Error())
	}
	field := map[officeCrossWorkspaceTarget]string{
		targetByWorkspaceID: "workspace_id",
		targetByWorkflowID:  "workflow_id",
		targetByTaskID:      "task_id",
	}[kind]
	id, _ := payload[field].(string)
	id = strings.TrimSpace(id)
	// list_tasks_kandev accepts workspace_id instead of workflow_id; when it
	// names a workspace directly, that workspace is the target the reach check
	// must evaluate rather than the caller's own.
	if id == "" && kind == targetByWorkflowID {
		workspaceID, _ := payload["workspace_id"].(string)
		if workspaceID = strings.TrimSpace(workspaceID); workspaceID != "" {
			return workspaceID, nil
		}
	}
	if id == "" {
		return principal.WorkspaceID, nil
	}
	switch kind {
	case targetByWorkflowID:
		workflow, err := h.taskSvc.GetWorkflow(ctx, id)
		if err != nil || workflow == nil {
			return "", wsError(msg.ID, msg.Action, ws.ErrorCodeNotFound, "workflow not found: "+id)
		}
		return workflow.WorkspaceID, nil
	case targetByTaskID:
		task, err := h.taskSvc.GetTask(ctx, id)
		if err != nil || task == nil {
			return "", wsError(msg.ID, msg.Action, ws.ErrorCodeNotFound, "task not found: "+id)
		}
		return task.WorkspaceID, nil
	default:
		return id, nil
	}
}

func officeCrossWorkspaceError(msg *ws.Message, err error) *ws.Message {
	var validation *crossworkspace.ValidationError
	switch {
	case errors.Is(err, crossworkspace.ErrDisabled):
		return wsError(msg.ID, msg.Action, ws.ErrorCodeForbidden, err.Error())
	case errors.Is(err, crossworkspace.ErrNotReachable):
		return wsError(msg.ID, msg.Action, ws.ErrorCodeNotFound, err.Error())
	case errors.As(err, &validation):
		return wsError(msg.ID, msg.Action, ws.ErrorCodeValidation, err.Error())
	default:
		return wsError(msg.ID, msg.Action, ws.ErrorCodeInternalError, "cross-workspace access failed: "+err.Error())
	}
}

// deliverToOfficeTask hands a message whose target is an Office task to the
// Office cross-workspace setting. When the target's Office workspace has the
// setting on and reaches the sender, the message becomes a comment that
// wakes the Office assignee and the returned response ends the send;
// otherwise it returns nil and the caller delivers it to a session as usual.
func (h *Handlers) deliverToOfficeTask(
	ctx context.Context, msg *ws.Message, sender, target *models.Task, prompt, replyToQuestionID string,
) *ws.Message {
	if h.officeCrossWorkspace == nil || target == nil || !target.IsFromOffice || replyToQuestionID != "" {
		return nil
	}
	delivery, handled, err := h.officeCrossWorkspace.DeliverToOfficeTask(ctx, sender, target, prompt)
	if err != nil {
		h.logger.Error("office cross-workspace delivery failed",
			zap.String("task_id", target.ID), zap.String("sender_task_id", sender.ID), zap.Error(err))
		return wsError(msg.ID, msg.Action, ws.ErrorCodeInternalError, "failed to deliver message to the Office task: "+err.Error())
	}
	if !handled {
		return nil
	}
	resp, _ := ws.NewResponse(msg.ID, msg.Action, map[string]interface{}{
		"task_id":         delivery.TaskID,
		"comment_id":      delivery.CommentID,
		stopTaskStatusKey: "commented",
	})
	return resp
}

// handleReportToHandoffSource posts a report from a Kanban task session on
// the Office task that handed off task_id.
func (h *Handlers) handleReportToHandoffSource(ctx context.Context, msg *ws.Message) (*ws.Message, error) {
	var req struct {
		TaskID string `json:"task_id"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return ws.NewError(msg.ID, msg.Action, ws.ErrorCodeBadRequest, "Invalid payload: "+err.Error(), nil)
	}
	if strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.Body) == "" {
		return ws.NewError(msg.ID, msg.Action, ws.ErrorCodeValidation, "task_id and body are required", nil)
	}
	principal, ok := mcpscope.PrincipalFromContext(ctx)
	if !ok || principal.WorkspaceID == "" {
		return ws.NewError(msg.ID, msg.Action, ws.ErrorCodeForbidden,
			"report_to_handoff_source requires an in-session task caller", nil)
	}
	if h.officeCrossWorkspace == nil {
		return ws.NewError(msg.ID, msg.Action, ws.ErrorCodeForbidden, crossworkspace.ErrDisabled.Error(), nil)
	}
	delivery, err := h.officeCrossWorkspace.ReportToHandoffSource(ctx, principal.WorkspaceID, strings.TrimSpace(req.TaskID), req.Body)
	if err != nil {
		return officeCrossWorkspaceError(msg, err), nil
	}
	return ws.NewResponse(msg.ID, msg.Action, map[string]interface{}{
		"task_id":         delivery.TaskID,
		"comment_id":      delivery.CommentID,
		stopTaskStatusKey: "reported",
	})
}
