package backendapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/auth"
	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/coordinator"
	"github.com/kandev/kandev/internal/events/bus"
	mcpscope "github.com/kandev/kandev/internal/mcp/scope"
	mcpserver "github.com/kandev/kandev/internal/mcp/server"
	"github.com/kandev/kandev/internal/office/crossworkspace"
	officesqlite "github.com/kandev/kandev/internal/office/repository/sqlite"
	officeruntime "github.com/kandev/kandev/internal/office/runtime"
	"github.com/kandev/kandev/internal/orchestrator"
	"github.com/kandev/kandev/internal/task/models"
	tasksqlite "github.com/kandev/kandev/internal/task/repository/sqlite"
	ws "github.com/kandev/kandev/pkg/websocket"
)

// officeKanbanCaller dispatches one MCP action in-process. Satisfied by
// *mcpserver.DispatcherBackendClient.
type officeKanbanCaller interface {
	RequestPayload(ctx context.Context, action string, payload, result interface{}) error
}

// officeCrossWorkspaceTasks is the task-service surface the gateway reads
// target ids through.
type officeCrossWorkspaceTasks interface {
	GetTask(ctx context.Context, id string) (*models.Task, error)
	GetWorkflow(ctx context.Context, id string) (*models.Workflow, error)
}

// officeCrossWorkspaceGateway implements officeruntime.CrossWorkspaceGateway:
// it decides reach with the crossworkspace service, then serves the request
// through the same in-process MCP actions a Kanban task's tools use, so an
// Office agent reads exactly what a Kanban agent would and a message keeps
// message_task_kandev's queueing, sender attribution and operation identity.
// It lives here because it needs both the Office runtime and the MCP
// dispatcher, which no Office package may import.
type officeCrossWorkspaceGateway struct {
	reach *crossworkspace.Service
	mcp   officeKanbanCaller
	tasks officeCrossWorkspaceTasks
}

var _ officeruntime.CrossWorkspaceGateway = (*officeCrossWorkspaceGateway)(nil)

// newOfficeCrossWorkspaceGateway returns nil (an untyped nil interface, so
// the runtime's nil check holds) when a dependency is missing.
func newOfficeCrossWorkspaceGateway(
	reach *crossworkspace.Service, dispatcher mcpserver.Dispatcher, tasks officeCrossWorkspaceTasks,
) officeruntime.CrossWorkspaceGateway {
	if reach == nil || dispatcher == nil || tasks == nil {
		return nil
	}
	return &officeCrossWorkspaceGateway{
		reach: reach,
		mcp:   mcpserver.NewDispatcherBackendClient(dispatcher, nil),
		tasks: tasks,
	}
}

func (g *officeCrossWorkspaceGateway) Directory(ctx context.Context, runCtx officeruntime.RunContext) (*crossworkspace.Directory, error) {
	return g.reach.ListReachable(ctx, runCtx.WorkspaceID)
}

func (g *officeCrossWorkspaceGateway) Coordinator(
	ctx context.Context, runCtx officeruntime.RunContext, workspaceID string,
) (*crossworkspace.Coordinator, error) {
	return g.reach.ResolveCoordinator(ctx, runCtx.WorkspaceID, workspaceID)
}

// ListTasks lists one workflow's tasks, or every workflow's tasks when
// workflowID is empty.
func (g *officeCrossWorkspaceGateway) ListTasks(
	ctx context.Context, runCtx officeruntime.RunContext, workspaceID, workflowID string,
) (json.RawMessage, error) {
	scoped, err := g.reach.ReachContext(ctx, runCtx.WorkspaceID, workspaceID)
	if err != nil {
		return nil, err
	}
	workflowIDs := []string{workflowID}
	if workflowID != "" {
		workflow, wfErr := g.tasks.GetWorkflow(scoped, workflowID)
		if wfErr != nil || workflow == nil || workflow.WorkspaceID != workspaceID {
			return nil, &officeruntime.CrossWorkspaceError{Status: http.StatusNotFound, Message: "workflow not found in workspace: " + workflowID}
		}
	} else {
		workflowIDs, err = g.workflowIDs(scoped, workspaceID)
		if err != nil {
			return nil, err
		}
	}
	tasks := make([]json.RawMessage, 0)
	for _, id := range workflowIDs {
		var page struct {
			Tasks []json.RawMessage `json:"tasks"`
		}
		if err := g.call(scoped, ws.ActionMCPListTasks, map[string]string{"workflow_id": id}, &page); err != nil {
			return nil, err
		}
		tasks = append(tasks, page.Tasks...)
	}
	return json.Marshal(map[string]interface{}{
		"workspace_id": workspaceID,
		"workflow_ids": workflowIDs,
		"tasks":        tasks,
		"total":        len(tasks),
	})
}

func (g *officeCrossWorkspaceGateway) workflowIDs(ctx context.Context, workspaceID string) ([]string, error) {
	var resp struct {
		Workflows []struct {
			ID string `json:"id"`
		} `json:"workflows"`
	}
	if err := g.call(ctx, ws.ActionMCPListWorkflows, map[string]string{"workspace_id": workspaceID}, &resp); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(resp.Workflows))
	for _, wf := range resp.Workflows {
		if wf.ID != "" {
			ids = append(ids, wf.ID)
		}
	}
	return ids, nil
}

func (g *officeCrossWorkspaceGateway) TaskSessions(
	ctx context.Context, runCtx officeruntime.RunContext, taskID string,
) (json.RawMessage, error) {
	scoped, err := g.taskReach(ctx, runCtx, taskID)
	if err != nil {
		return nil, err
	}
	return g.callRaw(scoped, ws.ActionMCPListTaskSessions, map[string]string{"task_id": taskID})
}

func (g *officeCrossWorkspaceGateway) TaskConversation(
	ctx context.Context, runCtx officeruntime.RunContext, taskID string, query officeruntime.ConversationQuery,
) (json.RawMessage, error) {
	scoped, err := g.taskReach(ctx, runCtx, taskID)
	if err != nil {
		return nil, err
	}
	payload := map[string]interface{}{"task_id": taskID}
	setIfNotEmpty(payload, "session_id", query.SessionID)
	setIfNotEmpty(payload, "before", query.Before)
	setIfNotEmpty(payload, "after", query.After)
	setIfNotEmpty(payload, "sort", query.Sort)
	if query.Limit > 0 {
		payload["limit"] = query.Limit
	}
	return g.callRaw(scoped, ws.ActionMCPGetTaskConversation, payload)
}

func (g *officeCrossWorkspaceGateway) TaskPlan(
	ctx context.Context, runCtx officeruntime.RunContext, taskID string,
) (json.RawMessage, error) {
	scoped, err := g.taskReach(ctx, runCtx, taskID)
	if err != nil {
		return nil, err
	}
	return g.callRaw(scoped, ws.ActionMCPGetTaskPlan, map[string]string{"task_id": taskID})
}

// SendMessage sends as the run's own task and session, through
// message_task_kandev, so the recipient sees who asked and how to reply.
func (g *officeCrossWorkspaceGateway) SendMessage(
	ctx context.Context, runCtx officeruntime.RunContext, req officeruntime.CrossWorkspaceMessage,
) (json.RawMessage, error) {
	scoped, err := g.taskReach(ctx, runCtx, req.TaskID)
	if err != nil {
		return nil, err
	}
	payload := map[string]interface{}{
		"task_id":           req.TaskID,
		"prompt":            req.Prompt,
		"sender_task_id":    runCtx.TaskID,
		"sender_session_id": runCtx.SessionID,
	}
	setIfNotEmpty(payload, "operation_id", req.OperationID)
	return g.callRaw(scoped, ws.ActionMCPMessageTask, payload)
}

// taskReach resolves a task's workspace as the Office workspace's owner sees
// it and decides reach. An unknown task is out of reach, never a distinct
// "not found", so a caller cannot probe ids it may not see.
func (g *officeCrossWorkspaceGateway) taskReach(
	ctx context.Context, runCtx officeruntime.RunContext, taskID string,
) (context.Context, error) {
	ownerCtx, err := g.reach.ReachContext(ctx, runCtx.WorkspaceID, runCtx.WorkspaceID)
	if err != nil {
		return nil, err
	}
	task, err := g.tasks.GetTask(ownerCtx, taskID)
	if err != nil || task == nil {
		return nil, crossworkspace.ErrNotReachable
	}
	return g.reach.ReachContext(ctx, runCtx.WorkspaceID, task.WorkspaceID)
}

func (g *officeCrossWorkspaceGateway) callRaw(ctx context.Context, action string, payload interface{}) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := g.call(ctx, action, payload, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// call dispatches an MCP action and maps its error codes onto HTTP statuses.
func (g *officeCrossWorkspaceGateway) call(ctx context.Context, action string, payload, result interface{}) error {
	err := g.mcp.RequestPayload(ctx, action, payload, result)
	if err == nil {
		return nil
	}
	var backendErr *mcpserver.BackendError
	if errors.As(err, &backendErr) {
		return &officeruntime.CrossWorkspaceError{Status: mcpErrorStatus(backendErr.Code), Message: backendErr.Message}
	}
	return fmt.Errorf("%s: %w", action, err)
}

func mcpErrorStatus(code string) int {
	switch code {
	case ws.ErrorCodeBadRequest, ws.ErrorCodeValidation:
		return http.StatusBadRequest
	case ws.ErrorCodeNotFound:
		return http.StatusNotFound
	case ws.ErrorCodeForbidden, ws.ErrorCodeUnauthorized:
		return http.StatusForbidden
	case ws.ErrorCodeConflict:
		return http.StatusConflict
	case ws.ErrorCodeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadGateway
	}
}

func setIfNotEmpty(payload map[string]interface{}, key, value string) {
	if value != "" {
		payload[key] = value
	}
}

// officeWorkspaceCoordinatorLookup adapts the upstream workspace
// coordinator service to crossworkspace.WorkspaceCoordinatorLookup: the
// first coordinator of the workspace that has a conversation task.
type officeWorkspaceCoordinatorLookup struct {
	svc *coordinator.Service
}

func (l officeWorkspaceCoordinatorLookup) WorkspaceCoordinatorTask(ctx context.Context, workspaceID string) (string, error) {
	found, err := l.svc.ListCoordinators(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	for _, entry := range found {
		if entry.Coordinator != nil && entry.Coordinator.ConversationTaskID != nil && *entry.Coordinator.ConversationTaskID != "" {
			return *entry.Coordinator.ConversationTaskID, nil
		}
	}
	return "", nil
}

// buildOfficeCrossWorkspaceService composes the Office cross-workspace
// access service. Reach is decided with the Office workspace owner's
// identity, resolved by the same resolver in-session MCP dispatch uses.
func buildOfficeCrossWorkspaceService(
	repo *officesqlite.Repository,
	taskRepo *tasksqlite.Repository,
	comments crossworkspace.CommentPoster,
	services *Services,
	log *logger.Logger,
) *crossworkspace.Service {
	if services == nil || services.Task == nil {
		return nil
	}
	owners := mcpscope.NewResolver(
		taskRepo,
		services.Auth,
		func() bool { return services.Auth != nil && services.Auth.Mode() != auth.ModeDisabled },
		log,
	)
	svc := crossworkspace.NewService(repo, services.Task, owners, comments, log)
	if services.Coordinator != nil {
		svc.SetWorkspaceCoordinatorLookup(officeWorkspaceCoordinatorLookup{svc: services.Coordinator})
	}
	return svc
}

// wireOfficeCrossWorkspace connects the setting to session MCP profiles and
// to handoff completion reports.
func wireOfficeCrossWorkspace(services *Services, orchestratorSvc *orchestrator.Service, eventBus bus.EventBus, log *logger.Logger) {
	if services == nil || services.OfficeSvcs == nil || services.OfficeSvcs.CrossWorkspace == nil {
		return
	}
	svc := services.OfficeSvcs.CrossWorkspace
	if orchestratorSvc != nil {
		orchestratorSvc.SetOfficeCrossWorkspaceLookup(svc)
	}
	if err := svc.SubscribeHandoffCompletion(eventBus); err != nil {
		log.Warn("office cross-workspace: handoff completion reports disabled", zap.Error(err))
	}
}
