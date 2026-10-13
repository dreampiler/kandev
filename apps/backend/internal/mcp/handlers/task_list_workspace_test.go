package handlers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/dto"
	"github.com/kandev/kandev/internal/task/models"
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
