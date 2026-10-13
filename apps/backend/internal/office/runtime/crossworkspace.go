package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kandev/kandev/internal/office/crossworkspace"
)

// CrossWorkspaceGateway is the runtime's port onto workspaces other than the
// run's own. Every method decides reach itself (the Office workspace's
// cross-workspace access setting plus its owner's read access) before it
// reads or writes anything, and returns crossworkspace.ErrDisabled or
// crossworkspace.ErrNotReachable when the target is out of reach. Reads
// return the same JSON the matching Kanban MCP tool returns.
//
// Control actions (stop, move, priority) are not part of the port by
// design: an Office agent asks the target workspace's coordinator, and the
// coordinator acts.
type CrossWorkspaceGateway interface {
	Directory(ctx context.Context, runCtx RunContext) (*crossworkspace.Directory, error)
	Coordinator(ctx context.Context, runCtx RunContext, workspaceID string) (*crossworkspace.Coordinator, error)
	ListTasks(ctx context.Context, runCtx RunContext, workspaceID, workflowID string) (json.RawMessage, error)
	TaskSessions(ctx context.Context, runCtx RunContext, taskID string) (json.RawMessage, error)
	TaskConversation(ctx context.Context, runCtx RunContext, taskID string, query ConversationQuery) (json.RawMessage, error)
	TaskPlan(ctx context.Context, runCtx RunContext, taskID string) (json.RawMessage, error)
	SendMessage(ctx context.Context, runCtx RunContext, req CrossWorkspaceMessage) (json.RawMessage, error)
}

// ConversationQuery mirrors get_task_conversation_kandev's paging inputs.
type ConversationQuery struct {
	SessionID string `json:"session_id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Before    string `json:"before,omitempty"`
	After     string `json:"after,omitempty"`
	Sort      string `json:"sort,omitempty"`
}

// CrossWorkspaceMessage is a message from the run's task to a task in a
// reachable workspace. The sender is always the run's own task and session.
type CrossWorkspaceMessage struct {
	TaskID      string `json:"task_id"`
	Prompt      string `json:"prompt"`
	OperationID string `json:"operation_id,omitempty"`
}

// CrossWorkspaceError carries a target-side failure (for example a target
// task that does not exist) with the HTTP status to answer it with.
type CrossWorkspaceError struct {
	Status  int
	Message string
}

func (e *CrossWorkspaceError) Error() string { return e.Message }

// errCrossWorkspaceTasklessMessage refuses a message from a run with no task
// or session: the recipient's reply needs an Office task to land on.
var errCrossWorkspaceTasklessMessage = &CrossWorkspaceError{
	Status:  http.StatusBadRequest,
	Message: "cross-workspace messages require a task-scoped run; the reply is delivered as a comment on that task",
}

// SetCrossWorkspace wires the cross-workspace gateway. Without it every
// cross-workspace route answers 503.
func (h *Handler) SetCrossWorkspace(gateway CrossWorkspaceGateway) {
	h.crossWorkspace = gateway
}

func registerCrossWorkspaceRoutes(group *gin.RouterGroup, h *Handler) {
	group.GET("/runtime/workspaces", h.listReachableWorkspaces)
	group.GET("/runtime/workspaces/:id/coordinator", h.getWorkspaceCoordinator)
	group.GET("/runtime/workspaces/:id/tasks", h.listWorkspaceTasks)
	group.GET("/runtime/tasks/:id/sessions", h.getTaskSessions)
	group.GET("/runtime/tasks/:id/conversation", h.getTaskConversation)
	group.GET("/runtime/tasks/:id/plan", h.getTaskPlan)
	group.POST("/runtime/tasks/:id/messages", h.sendTaskMessage)
}

// crossWorkspaceRead is the shared prologue of every read route: a valid
// run token, the list_tasks capability, and a wired gateway.
func (h *Handler) crossWorkspaceRead(c *gin.Context, action, targetType, targetID string) (RunContext, bool) {
	runCtx, _, ok := h.contextFromRequest(c)
	if !ok {
		return RunContext{}, false
	}
	if !runCtx.Capabilities.Allows(CapabilityListTasks) {
		h.respondRuntimeError(c, runCtx, action, targetType, targetID, ErrCapabilityDenied)
		return RunContext{}, false
	}
	if h.crossWorkspace == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cross-workspace access is not available"})
		return RunContext{}, false
	}
	return runCtx, true
}

func (h *Handler) listReachableWorkspaces(c *gin.Context) {
	runCtx, ok := h.crossWorkspaceRead(c, "list_workspaces", "workspace", "")
	if !ok {
		return
	}
	dir, err := h.crossWorkspace.Directory(c.Request.Context(), runCtx)
	if err != nil {
		h.respondCrossWorkspaceError(c, runCtx, "list_workspaces", "workspace", runCtx.WorkspaceID, err)
		return
	}
	h.appendActionRunEvent(c.Request.Context(), runCtx, "list_workspaces", "workspace", runCtx.WorkspaceID)
	c.JSON(http.StatusOK, dir)
}

func (h *Handler) getWorkspaceCoordinator(c *gin.Context) {
	wsID := strings.TrimSpace(c.Param("id"))
	runCtx, ok := h.crossWorkspaceRead(c, "get_workspace_coordinator", "workspace", wsID)
	if !ok {
		return
	}
	coordinator, err := h.crossWorkspace.Coordinator(c.Request.Context(), runCtx, wsID)
	if err != nil {
		h.respondCrossWorkspaceError(c, runCtx, "get_workspace_coordinator", "workspace", wsID, err)
		return
	}
	h.appendActionRunEvent(c.Request.Context(), runCtx, "get_workspace_coordinator", "workspace", wsID)
	c.JSON(http.StatusOK, gin.H{"coordinator": coordinator})
}

func (h *Handler) listWorkspaceTasks(c *gin.Context) {
	wsID := strings.TrimSpace(c.Param("id"))
	runCtx, ok := h.crossWorkspaceRead(c, "list_workspace_tasks", "workspace", wsID)
	if !ok {
		return
	}
	body, err := h.crossWorkspace.ListTasks(c.Request.Context(), runCtx, wsID, strings.TrimSpace(c.Query("workflow_id")))
	h.respondCrossWorkspaceRead(c, runCtx, "list_workspace_tasks", "workspace", wsID, body, err)
}

func (h *Handler) getTaskSessions(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("id"))
	runCtx, ok := h.crossWorkspaceRead(c, "get_task_sessions", "task", taskID)
	if !ok {
		return
	}
	body, err := h.crossWorkspace.TaskSessions(c.Request.Context(), runCtx, taskID)
	h.respondCrossWorkspaceRead(c, runCtx, "get_task_sessions", "task", taskID, body, err)
}

func (h *Handler) getTaskConversation(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("id"))
	runCtx, ok := h.crossWorkspaceRead(c, "get_task_conversation", "task", taskID)
	if !ok {
		return
	}
	query, err := parseConversationQuery(c.Request.URL.Query())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	body, err := h.crossWorkspace.TaskConversation(c.Request.Context(), runCtx, taskID, query)
	h.respondCrossWorkspaceRead(c, runCtx, "get_task_conversation", "task", taskID, body, err)
}

func (h *Handler) getTaskPlan(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("id"))
	runCtx, ok := h.crossWorkspaceRead(c, "get_task_plan", "task", taskID)
	if !ok {
		return
	}
	body, err := h.crossWorkspace.TaskPlan(c.Request.Context(), runCtx, taskID)
	h.respondCrossWorkspaceRead(c, runCtx, "get_task_plan", "task", taskID, body, err)
}

type sendTaskMessageRequest struct {
	Prompt      string `json:"prompt"`
	OperationID string `json:"operation_id"`
}

func (h *Handler) sendTaskMessage(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("id"))
	runCtx, _, ok := h.contextFromRequest(c)
	if !ok {
		return
	}
	if !runCtx.Capabilities.Allows(CapabilityPostComment) {
		h.respondRuntimeError(c, runCtx, "send_task_message", "task", taskID, ErrCapabilityDenied)
		return
	}
	var req sendTaskMessageRequest
	if !bindClosedJSON(c, &req) {
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prompt is required"})
		return
	}
	if h.crossWorkspace == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cross-workspace access is not available"})
		return
	}
	if runCtx.TaskID == "" || runCtx.SessionID == "" {
		h.respondCrossWorkspaceError(c, runCtx, "send_task_message", "task", taskID, errCrossWorkspaceTasklessMessage)
		return
	}
	body, err := h.crossWorkspace.SendMessage(c.Request.Context(), runCtx, CrossWorkspaceMessage{
		TaskID:      taskID,
		Prompt:      req.Prompt,
		OperationID: strings.TrimSpace(req.OperationID),
	})
	h.respondCrossWorkspaceRead(c, runCtx, "send_task_message", "task", taskID, body, err)
}

func (h *Handler) respondCrossWorkspaceRead(
	c *gin.Context, runCtx RunContext, action, targetType, targetID string, body json.RawMessage, err error,
) {
	if err != nil {
		h.respondCrossWorkspaceError(c, runCtx, action, targetType, targetID, err)
		return
	}
	h.appendActionRunEvent(c.Request.Context(), runCtx, action, targetType, targetID)
	if len(body) == 0 {
		body = json.RawMessage(`{}`)
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

// respondCrossWorkspaceError maps reach refusals to 403 (setting off) and
// 404 (out of reach, indistinguishable from not found), target-side errors
// to their own status, and everything else to the shared runtime responder.
func (h *Handler) respondCrossWorkspaceError(
	c *gin.Context, runCtx RunContext, action, targetType, targetID string, err error,
) {
	ctx := c.Request.Context()
	switch {
	case errors.Is(err, crossworkspace.ErrDisabled):
		h.appendDeniedRunEvent(ctx, runCtx, action, targetType, targetID, err)
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	case errors.Is(err, crossworkspace.ErrNotReachable):
		h.appendDeniedRunEvent(ctx, runCtx, action, targetType, targetID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	var target *CrossWorkspaceError
	if errors.As(err, &target) {
		if target.Status >= 400 && target.Status < 500 {
			h.appendDeniedRunEvent(ctx, runCtx, action, targetType, targetID, err)
		}
		c.JSON(target.Status, gin.H{"error": target.Message})
		return
	}
	h.respondRuntimeError(c, runCtx, action, targetType, targetID, err)
}

func parseConversationQuery(values url.Values) (ConversationQuery, error) {
	q := ConversationQuery{
		SessionID: strings.TrimSpace(values.Get("session_id")),
		Before:    strings.TrimSpace(values.Get("before")),
		After:     strings.TrimSpace(values.Get("after")),
		Sort:      strings.TrimSpace(values.Get("sort")),
	}
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return ConversationQuery{}, errors.New("limit must be a non-negative integer")
		}
		q.Limit = n
	}
	if q.Sort != "" && q.Sort != "asc" && q.Sort != "desc" {
		return ConversationQuery{}, errors.New("sort must be asc or desc")
	}
	return q, nil
}
