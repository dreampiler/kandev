package handlers

import (
	"context"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/service"
	ws "github.com/kandev/kandev/pkg/websocket"
	"go.uber.org/zap"
)

// resolveListScope returns the identifier a list action should use when the
// caller omitted it, falling back to the calling task's own scope.
//
// An agent that already runs inside a task holds neither its workspace_id nor
// its workflow_id, so omitting one was a frequent first-call failure that cost
// a round trip to recover. The fallback is the caller's own task scope, so it
// never widens a read — it names the board the caller is already on. It applies
// only when the request carries caller_task_id, which the MCP server sends for
// exactly the tools whose scope argument is optional; every other caller keeps
// the required-argument rejection.
func (h *Handlers) resolveListScope(ctx context.Context, msg *ws.Message, fieldName string) (string, *ws.Message) {
	value, err := unmarshalStringField(msg.Payload, fieldName)
	if err != nil {
		return "", wsError(msg.ID, msg.Action, ws.ErrorCodeBadRequest, "Invalid payload: "+err.Error())
	}
	if value != "" {
		return value, nil
	}
	callerTaskID, callerErr := unmarshalStringField(msg.Payload, "caller_task_id")
	if callerErr != nil || callerTaskID == "" {
		return "", wsError(msg.ID, msg.Action, ws.ErrorCodeValidation, fieldName+" is required")
	}
	task, taskErr := h.taskSvc.GetTask(ctx, callerTaskID)
	if taskErr != nil || task == nil {
		return "", wsError(msg.ID, msg.Action, ws.ErrorCodeNotFound,
			fieldName+" is required and could not be resolved from the calling task "+
				callerTaskID+"; call list_workspaces_kandev (or list_workflows_kandev) and pass the ID explicitly.")
	}
	value = taskWorkspaceOrWorkflowID(task, fieldName)
	if value == "" {
		return "", wsError(msg.ID, msg.Action, ws.ErrorCodeValidation,
			fieldName+" is required: the calling task "+callerTaskID+" has no "+fieldName+
				". Pass it explicitly — call list_workspaces_kandev or list_workflows_kandev to discover it.")
	}
	return value, nil
}

// taskWorkspaceOrWorkflowID projects the one scope field a list action keys on
// from the calling task.
func taskWorkspaceOrWorkflowID(task *models.Task, fieldName string) string {
	switch fieldName {
	case "workspace_id":
		return task.WorkspaceID
	case "workflow_id":
		return task.WorkflowID
	default:
		return ""
	}
}

// recordUserMessageForPrompt retains creation ownership separately from the
// message's turn ID: attaching to an existing turn never grants cleanup rights.
func (h *Handlers) recordUserMessageForPrompt(ctx context.Context, taskID, sessionID, prompt string, metadata map[string]interface{}) (*models.Message, string) {
	if h.taskSvc == nil {
		return nil, ""
	}
	turn, err := h.taskSvc.GetActiveTurn(ctx, sessionID)
	if err != nil {
		return h.recordUserMessage(ctx, taskID, sessionID, prompt, metadata), ""
	}
	ownedTurnID := ""
	if turn == nil {
		turn, err = h.taskSvc.StartTurn(ctx, sessionID)
		if err != nil {
			return h.recordUserMessage(ctx, taskID, sessionID, prompt, metadata), ""
		}
		ownedTurnID = turn.ID
	}
	message, err := h.taskSvc.CreateMessage(ctx, &service.CreateMessageRequest{
		TaskSessionID: sessionID, TaskID: taskID, TurnID: turn.ID,
		Content: prompt, AuthorType: "user", Metadata: metadata,
	})
	if err != nil {
		h.logger.Warn("failed to record user message for message_task", zap.String("task_id", taskID), zap.String("session_id", sessionID), zap.Error(err))
		if ownedTurnID != "" {
			_, _ = h.taskSvc.RollbackUnsentMessageTurn(ctx, sessionID, ownedTurnID)
		}
		return nil, ""
	}
	return message, ownedTurnID
}
