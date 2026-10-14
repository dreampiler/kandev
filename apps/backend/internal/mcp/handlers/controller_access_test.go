package handlers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpscope "github.com/kandev/kandev/internal/mcp/scope"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/service"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	ws "github.com/kandev/kandev/pkg/websocket"
)

// controllerPrincipalCtx installs the trusted MCP principal the dispatcher
// would derive from the caller's own task and session.
func controllerPrincipalCtx(callerTaskID, sessionID, workspaceID string) context.Context {
	return mcpscope.WithPrincipal(context.Background(), mcpscope.Principal{
		CallerTaskID:    callerTaskID,
		CallerSessionID: sessionID,
		WorkspaceID:     workspaceID,
	})
}

func seedControllerWorkspace(t *testing.T, repo interface {
	CreateWorkspace(context.Context, *models.Workspace) error
	CreateTask(context.Context, *models.Task) error
}, ctx context.Context, workspaceID string) {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{
		ID: workspaceID, Name: workspaceID, CreatedAt: now, UpdatedAt: now,
	}))
	// A workspace controller: a root task with at least one child.
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "controller", WorkspaceID: workspaceID,
		Title: "Control", State: v1.TaskStateInProgress, CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "controller-child", WorkspaceID: workspaceID, ParentID: "controller",
		Title: "Child", State: v1.TaskStateCreated, CreatedAt: now, UpdatedAt: now,
	}))
	// An unrelated task in a separate tree, same workspace.
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "unrelated", WorkspaceID: workspaceID,
		Title: "Other tree", State: v1.TaskStateCreated, CreatedAt: now, UpdatedAt: now,
	}))
}

// TestHandleListRelatedTasks_ControllerReadsUnrelatedInWorkspace is the core
// PR-2 case: a controller principal may read an unrelated task's relations when
// the target is in the controller's own workspace.
func TestHandleListRelatedTasks_ControllerReadsUnrelatedInWorkspace(t *testing.T) {
	svc, repo := newTestTaskService(t)
	ctx := controllerPrincipalCtx("controller", "sess-ctl", "ws-ctl")
	seedControllerWorkspace(t, repo, ctx, "ws-ctl")

	h := &Handlers{
		taskSvc:    svc,
		handoffSvc: service.NewHandoffService(repo, nil, nil, nil, nil, testLogger(t)),
		logger:     testLogger(t).WithFields(),
	}

	msg := makeWSMessage(t, ws.ActionMCPListRelatedTasks, map[string]any{
		"task_id":        "unrelated",
		"caller_task_id": "controller",
	})
	resp, err := h.handleListRelatedTasks(ctx, msg)
	require.NoError(t, err)
	require.Equal(t, ws.MessageTypeResponse, resp.Type)

	var payload service.RelatedTasks
	require.NoError(t, json.Unmarshal(resp.Payload, &payload))
	assert.Equal(t, "unrelated", payload.Task.ID)
}

// TestHandleListRelatedTasks_NonControllerDeniedForUnrelated confirms the
// exemption is limited to a controller: a plain root task with no children
// still gets today's relation gate.
func TestHandleListRelatedTasks_NonControllerDeniedForUnrelated(t *testing.T) {
	svc, repo := newTestTaskService(t)
	ctx := controllerPrincipalCtx("solo", "sess-solo", "ws-ctl")
	seedControllerWorkspace(t, repo, ctx, "ws-ctl")
	now := time.Now().UTC()
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "solo", WorkspaceID: "ws-ctl",
		Title: "Solo", State: v1.TaskStateInProgress, CreatedAt: now, UpdatedAt: now,
	}))

	h := &Handlers{
		taskSvc:    svc,
		handoffSvc: service.NewHandoffService(repo, nil, nil, nil, nil, testLogger(t)),
		logger:     testLogger(t).WithFields(),
	}

	msg := makeWSMessage(t, ws.ActionMCPListRelatedTasks, map[string]any{
		"task_id":        "unrelated",
		"caller_task_id": "solo",
	})
	resp, err := h.handleListRelatedTasks(ctx, msg)
	require.NoError(t, err)
	assertWSError(t, resp, ws.ErrorCodeForbidden)
}

// TestHandleListRelatedTasks_ControllerDeniedAcrossWorkspace confirms the
// exemption stays workspace-bounded: a controller cannot read a task in
// another workspace without the Office reach path.
func TestHandleListRelatedTasks_ControllerDeniedAcrossWorkspace(t *testing.T) {
	svc, repo := newTestTaskService(t)
	ctx := controllerPrincipalCtx("controller", "sess-ctl", "ws-ctl")
	seedControllerWorkspace(t, repo, ctx, "ws-ctl")
	now := time.Now().UTC()
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{
		ID: "ws-other", Name: "Other", CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "foreign", WorkspaceID: "ws-other",
		Title: "Foreign", State: v1.TaskStateCreated, CreatedAt: now, UpdatedAt: now,
	}))

	h := &Handlers{
		taskSvc:    svc,
		handoffSvc: service.NewHandoffService(repo, nil, nil, nil, nil, testLogger(t)),
		logger:     testLogger(t).WithFields(),
	}

	msg := makeWSMessage(t, ws.ActionMCPListRelatedTasks, map[string]any{
		"task_id":        "foreign",
		"caller_task_id": "controller",
	})
	resp, err := h.handleListRelatedTasks(ctx, msg)
	require.NoError(t, err)
	assertWSError(t, resp, ws.ErrorCodeForbidden)
}
