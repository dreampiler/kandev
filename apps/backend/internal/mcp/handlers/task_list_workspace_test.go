package handlers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/dto"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/service"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	ws "github.com/kandev/kandev/pkg/websocket"
)

func TestHandleListTasks_WorkspaceScopeIncludesWorkflowlessTasks(t *testing.T) {
	svc, repo := newTestTaskService(t)
	ctx := context.Background()
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws-lt", Name: "LT"}))
	require.NoError(t, repo.CreateWorkflow(ctx, &models.Workflow{ID: "wf-lt", WorkspaceID: "ws-lt", Name: "Board"}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "task-in-board", WorkspaceID: "ws-lt", WorkflowID: "wf-lt", Title: "InBoard", State: v1.TaskStateTODO,
	}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "task-no-board", WorkspaceID: "ws-lt", WorkflowID: "", Title: "NoBoard", State: v1.TaskStateTODO,
	}))
	h := &Handlers{taskSvc: svc, logger: testLogger(t).WithFields()}

	resp, err := h.handleListTasks(kanbanPrincipalCtx("ws-lt", "task-in-board"),
		makeWSMessage(t, ws.ActionMCPListTasks, map[string]string{"workspace_id": "ws-lt"}))
	require.NoError(t, err)
	require.Equal(t, ws.MessageTypeResponse, resp.Type)

	var out dto.ListTasksResponse
	require.NoError(t, json.Unmarshal(resp.Payload, &out))
	titles := map[string]bool{}
	for _, task := range out.Tasks {
		titles[task.Title] = true
	}
	assert.True(t, titles["InBoard"], "workflow-scoped task must be listed")
	assert.True(t, titles["NoBoard"], "workflow-less task must be listed")
}

// TestHandleListTasks_WorkspaceScopeAutomationOriginVisibility pins the
// controller-only automation contract: a workspace controller (a root task with
// at least one child) sees automation-run outputs, while an ordinary task
// principal keeps the provenance-hiding list. Both still see workflow-less
// tasks.
func TestHandleListTasks_WorkspaceScopeAutomationOriginVisibility(t *testing.T) {
	svc, repo := newTestTaskService(t)
	ctx := context.Background()
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws-auto", Name: "Auto"}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "controller", WorkspaceID: "ws-auto", Title: "Control", State: v1.TaskStateInProgress,
	}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "controller-child", WorkspaceID: "ws-auto", ParentID: "controller", Title: "Child", State: v1.TaskStateCreated,
	}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "task-automation-run", WorkspaceID: "ws-auto", WorkflowID: "", Title: "Daily sync",
		State: v1.TaskStateTODO, Origin: models.TaskOriginAutomationRun,
	}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "task-plain", WorkspaceID: "ws-auto", WorkflowID: "", Title: "Plain", State: v1.TaskStateTODO,
	}))
	h := &Handlers{
		taskSvc:    svc,
		handoffSvc: service.NewHandoffService(repo, nil, nil, nil, nil, testLogger(t)),
		logger:     testLogger(t).WithFields(),
	}

	controllerResp, err := h.handleListTasks(kanbanPrincipalCtx("ws-auto", "controller"),
		makeWSMessage(t, ws.ActionMCPListTasks, map[string]string{"workspace_id": "ws-auto"}))
	require.NoError(t, err)
	require.Equal(t, ws.MessageTypeResponse, controllerResp.Type)
	var controllerOut dto.ListTasksResponse
	require.NoError(t, json.Unmarshal(controllerResp.Payload, &controllerOut))
	controllerIDs := map[string]string{}
	for _, task := range controllerOut.Tasks {
		controllerIDs[task.Title] = task.ID
	}
	require.Contains(t, controllerIDs, "Daily sync", "a controller must see an automation-run output")
	assert.Equal(t, "task-automation-run", controllerIDs["Daily sync"])
	assert.Contains(t, controllerIDs, "Plain", "a controller still sees workflow-less tasks")

	plainResp, err := h.handleListTasks(kanbanPrincipalCtx("ws-auto", "task-plain"),
		makeWSMessage(t, ws.ActionMCPListTasks, map[string]string{"workspace_id": "ws-auto"}))
	require.NoError(t, err)
	require.Equal(t, ws.MessageTypeResponse, plainResp.Type)
	var plainOut dto.ListTasksResponse
	require.NoError(t, json.Unmarshal(plainResp.Payload, &plainOut))
	plainTitles := map[string]bool{}
	for _, task := range plainOut.Tasks {
		plainTitles[task.Title] = true
	}
	assert.False(t, plainTitles["Daily sync"], "an ordinary task principal must not see an automation-run output")
	assert.True(t, plainTitles["Plain"], "an ordinary task principal still sees workflow-less tasks")
}

func TestHandleListTasks_WorkspaceScopeRejectsForeignWorkspaceForKanban(t *testing.T) {
	svc, repo := newTestTaskService(t)
	ctx := context.Background()
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws-mine", Name: "Mine"}))
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws-other", Name: "Other"}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "task-mine", WorkspaceID: "ws-mine", WorkflowID: "", Title: "Mine", State: v1.TaskStateTODO,
	}))
	h := &Handlers{taskSvc: svc, logger: testLogger(t).WithFields()}

	resp, err := h.handleListTasks(kanbanPrincipalCtx("ws-mine", "task-mine"),
		makeWSMessage(t, ws.ActionMCPListTasks, map[string]string{"workspace_id": "ws-other"}))
	require.NoError(t, err)
	assertWSError(t, resp, ws.ErrorCodeForbidden)
}
