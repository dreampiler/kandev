package handlers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpprofile "github.com/kandev/kandev/internal/mcp/profile"
	mcpscope "github.com/kandev/kandev/internal/mcp/scope"
	"github.com/kandev/kandev/internal/office/crossworkspace"
	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	ws "github.com/kandev/kandev/pkg/websocket"
)

// fakeOfficeCrossWorkspace decides reach from a fixed set and records what
// the handlers asked it.
type fakeOfficeCrossWorkspace struct {
	reachable   map[string]error
	delivered   bool
	reportedFor string
	reportedWS  string
}

func (f *fakeOfficeCrossWorkspace) Reachable(_ context.Context, _, target string) error {
	if err, ok := f.reachable[target]; ok {
		return err
	}
	return crossworkspace.ErrDisabled
}

func (f *fakeOfficeCrossWorkspace) ListReachable(_ context.Context, office string) (*crossworkspace.Directory, error) {
	return &crossworkspace.Directory{Enabled: true, Workspaces: []crossworkspace.ReachableWorkspace{
		{ID: office, Self: true},
		{ID: "ws-kanban", Coordinator: &crossworkspace.Coordinator{WorkspaceID: "ws-kanban", TaskID: "ctl", Source: "setting"}},
	}}, nil
}

func (f *fakeOfficeCrossWorkspace) DeliverToOfficeTask(_ context.Context, _, target *models.Task, _ string) (*crossworkspace.OfficeDelivery, bool, error) {
	if !f.delivered {
		return nil, false, nil
	}
	return &crossworkspace.OfficeDelivery{TaskID: target.ID, CommentID: "comment-1"}, true, nil
}

func (f *fakeOfficeCrossWorkspace) ReportToHandoffSource(_ context.Context, callerWS, taskID, _ string) (*crossworkspace.OfficeDelivery, error) {
	f.reportedWS, f.reportedFor = callerWS, taskID
	return &crossworkspace.OfficeDelivery{TaskID: "office-task", CommentID: "comment-2"}, nil
}

func officePrincipalCtx(workspaceID string) context.Context {
	return mcpscope.WithPrincipal(context.Background(), mcpscope.Principal{
		WorkspaceID:     workspaceID,
		CallerTaskID:    "office-task",
		CallerSessionID: "office-session",
		Surface:         mcpprofile.SurfaceOfficeTask,
	})
}

func seedOfficeAndKanban(t *testing.T) (*Handlers, *fakeOfficeCrossWorkspace) {
	t.Helper()
	svc, repo := newTestTaskService(t)
	ctx := context.Background()
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws-office", Name: "Office", OfficeWorkflowID: "wf-office"}))
	require.NoError(t, repo.CreateWorkflow(ctx, &models.Workflow{ID: "wf-office", WorkspaceID: "ws-office", Name: "Office"}))
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws-kanban", Name: "Kanban"}))
	require.NoError(t, repo.CreateWorkflow(ctx, &models.Workflow{ID: "wf-kanban", WorkspaceID: "ws-kanban", Name: "Board"}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "office-task", WorkspaceID: "ws-office", WorkflowID: "wf-office", Title: "Office task", State: v1.TaskStateInProgress,
	}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "ctl", WorkspaceID: "ws-kanban", WorkflowID: "wf-kanban", Title: "Controller", State: v1.TaskStateInProgress,
	}))
	fake := &fakeOfficeCrossWorkspace{reachable: map[string]error{}}
	h := &Handlers{taskSvc: svc, logger: testLogger(t).WithFields()}
	h.SetOfficeCrossWorkspace(fake)
	return h, fake
}

func TestOfficeCrossWorkspaceGuard_DeniesUnreachableTargets(t *testing.T) {
	h, _ := seedOfficeAndKanban(t)
	ctx := officePrincipalCtx("ws-office")
	for _, msg := range []*ws.Message{
		makeWSMessage(t, ws.ActionMCPListTasks, map[string]string{"workflow_id": "wf-kanban"}),
		makeWSMessage(t, ws.ActionMCPListWorkflows, map[string]string{"workspace_id": "ws-kanban"}),
		makeWSMessage(t, ws.ActionMCPGetTaskConversation, map[string]string{"task_id": "ctl"}),
		makeWSMessage(t, ws.ActionMCPListTaskSessions, map[string]string{"task_id": "ctl"}),
		makeWSMessage(t, ws.ActionMCPMessageTask, map[string]string{"task_id": "ctl", "prompt": "hi"}),
	} {
		guarded, replacement, err := h.authorizeOfficeCrossWorkspaceRequest(ctx, msg)
		require.NoError(t, err)
		require.Nil(t, replacement, msg.Action)
		assertWSError(t, guarded, ws.ErrorCodeForbidden)
	}
}

func TestOfficeCrossWorkspaceGuard_AllowsReachableAndOwnWorkspace(t *testing.T) {
	h, fake := seedOfficeAndKanban(t)
	ctx := officePrincipalCtx("ws-office")
	own := makeWSMessage(t, ws.ActionMCPListTasks, map[string]string{"workflow_id": "wf-office"})
	guarded, replacement, err := h.authorizeOfficeCrossWorkspaceRequest(ctx, own)
	require.NoError(t, err)
	assert.Nil(t, guarded)
	assert.Same(t, own, replacement)

	fake.reachable["ws-kanban"] = nil
	foreign := makeWSMessage(t, ws.ActionMCPGetTaskConversation, map[string]string{"task_id": "ctl"})
	guarded, replacement, err = h.authorizeOfficeCrossWorkspaceRequest(ctx, foreign)
	require.NoError(t, err)
	assert.Nil(t, guarded)
	assert.Same(t, foreign, replacement)

	fake.reachable["ws-kanban"] = crossworkspace.ErrNotReachable
	guarded, _, _ = h.authorizeOfficeCrossWorkspaceRequest(ctx, foreign)
	assertWSError(t, guarded, ws.ErrorCodeNotFound)
}

func TestOfficeCrossWorkspaceGuard_PlanReadRequiresReach(t *testing.T) {
	h, fake := seedOfficeAndKanban(t)
	ctx := officePrincipalCtx("ws-office")

	foreign := makeWSMessage(t, ws.ActionMCPGetTaskPlan, map[string]string{"task_id": "ctl"})
	guarded, _, err := h.authorizeOfficeCrossWorkspaceRequest(ctx, foreign)
	require.NoError(t, err)
	assertWSError(t, guarded, ws.ErrorCodeForbidden)

	fake.reachable["ws-kanban"] = nil
	guarded, replacement, err := h.authorizeOfficeCrossWorkspaceRequest(ctx, foreign)
	require.NoError(t, err)
	assert.Nil(t, guarded)
	require.NotNil(t, replacement)

	own := makeWSMessage(t, ws.ActionMCPGetTaskPlan, map[string]string{"task_id": "office-task"})
	guarded, replacement, err = h.authorizeOfficeCrossWorkspaceRequest(ctx, own)
	require.NoError(t, err)
	assert.Nil(t, guarded)
	assert.Same(t, own, replacement)
}

func TestOfficeCrossWorkspaceGuard_ListTasksByWorkspaceUsesReach(t *testing.T) {
	h, _ := seedOfficeAndKanban(t)
	msg := makeWSMessage(t, ws.ActionMCPListTasks, map[string]string{"workspace_id": "ws-kanban"})
	guarded, _, err := h.authorizeOfficeCrossWorkspaceRequest(officePrincipalCtx("ws-office"), msg)
	require.NoError(t, err)
	assertWSError(t, guarded, ws.ErrorCodeForbidden)
}

func TestOfficeCrossWorkspaceGuard_LeavesOtherPrincipalsAlone(t *testing.T) {
	h, _ := seedOfficeAndKanban(t)
	kanbanCtx := mcpscope.WithPrincipal(context.Background(), mcpscope.Principal{
		WorkspaceID: "ws-kanban", CallerTaskID: "ctl", CallerSessionID: "s", Surface: mcpprofile.SurfaceKanbanTask,
	})
	msg := makeWSMessage(t, ws.ActionMCPGetTaskConversation, map[string]string{"task_id": "office-task"})
	guarded, replacement, err := h.authorizeOfficeCrossWorkspaceRequest(kanbanCtx, msg)
	require.NoError(t, err)
	assert.Nil(t, guarded)
	assert.Same(t, msg, replacement)
}

func TestOfficeCrossWorkspaceGuard_ListWorkspacesAnswersDirectory(t *testing.T) {
	h, _ := seedOfficeAndKanban(t)
	guarded, replacement, err := h.authorizeOfficeCrossWorkspaceRequest(officePrincipalCtx("ws-office"),
		makeWSMessage(t, ws.ActionMCPListWorkspaces, map[string]string{}))
	require.NoError(t, err)
	require.Nil(t, replacement)
	require.Equal(t, ws.MessageTypeResponse, guarded.Type)
	var dir crossworkspace.Directory
	require.NoError(t, json.Unmarshal(guarded.Payload, &dir))
	require.Len(t, dir.Workspaces, 2)
	assert.Equal(t, "ctl", dir.Workspaces[1].Coordinator.TaskID)
}

func TestHandleMessageTask_ToOfficeTaskBecomesCommentWhenSettingDelivers(t *testing.T) {
	h, fake := seedOfficeAndKanban(t)
	fake.delivered = true
	resp, err := h.handleMessageTask(context.Background(), makeWSMessage(t, ws.ActionMCPMessageTask, map[string]string{
		"task_id": "office-task", "prompt": "W-12 merged", "sender_task_id": "ctl", "sender_session_id": "ctl-session",
	}))
	require.NoError(t, err)
	require.Equal(t, ws.MessageTypeResponse, resp.Type)
	var payload map[string]string
	require.NoError(t, json.Unmarshal(resp.Payload, &payload))
	assert.Equal(t, "commented", payload["status"])
	assert.Equal(t, "comment-1", payload["comment_id"])
}

func TestHandleReportToHandoffSource(t *testing.T) {
	h, fake := seedOfficeAndKanban(t)
	msg := makeWSMessage(t, ws.ActionMCPReportToHandoffSource, map[string]string{"task_id": "delivery", "body": "done"})
	resp, err := h.handleReportToHandoffSource(context.Background(), msg)
	require.NoError(t, err)
	assertWSError(t, resp, ws.ErrorCodeForbidden)

	kanbanCtx := mcpscope.WithPrincipal(context.Background(), mcpscope.Principal{
		WorkspaceID: "ws-kanban", CallerTaskID: "ctl", CallerSessionID: "s", Surface: mcpprofile.SurfaceKanbanTask,
	})
	resp, err = h.handleReportToHandoffSource(kanbanCtx, msg)
	require.NoError(t, err)
	require.Equal(t, ws.MessageTypeResponse, resp.Type)
	assert.Equal(t, "ws-kanban", fake.reportedWS, "the caller workspace comes from the principal, never the payload")
	assert.Equal(t, "delivery", fake.reportedFor)
}
