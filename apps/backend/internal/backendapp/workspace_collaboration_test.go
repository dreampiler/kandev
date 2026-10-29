package backendapp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	mcpscope "github.com/kandev/kandev/internal/mcp/scope"
	"github.com/kandev/kandev/internal/task/models"
	taskservice "github.com/kandev/kandev/internal/task/service"
)

type fakeCollaborationLookup struct {
	task       *models.Task
	session    *models.TaskSession
	workspace  *models.Workspace
	grant      *models.WorkspaceCollaborationGrant
	grantErr   error
	sessionErr error
}

func (f *fakeCollaborationLookup) GetTask(context.Context, string) (*models.Task, error) {
	return f.task, nil
}

func (f *fakeCollaborationLookup) GetTaskSession(context.Context, string) (*models.TaskSession, error) {
	return f.session, f.sessionErr
}

func (f *fakeCollaborationLookup) GetWorkspace(context.Context, string) (*models.Workspace, error) {
	return f.workspace, nil
}

func (f *fakeCollaborationLookup) GetWorkspaceCollaborationGrant(context.Context, string, string, string) (*models.WorkspaceCollaborationGrant, error) {
	return f.grant, f.grantErr
}

func collaborationTestBoundary(enabled bool, grant *models.WorkspaceCollaborationGrant) (*workspaceCollaborationBoundary, context.Context) {
	lookup := &fakeCollaborationLookup{
		task:      &models.Task{ID: "caller-task", WorkspaceID: "source-ws"},
		session:   &models.TaskSession{ID: "caller-session", TaskID: "caller-task"},
		workspace: &models.Workspace{ID: "source-ws"},
		grant:     grant,
	}
	boundary := &workspaceCollaborationBoundary{enabled: enabled, store: lookup}
	ctx := mcpscope.WithPrincipal(context.Background(), mcpscope.Principal{
		WorkspaceID: "source-ws", CallerTaskID: "caller-task", CallerSessionID: "caller-session",
	})
	return boundary, ctx
}

func TestWorkspaceCollaborationDisabledAndNativeCallersKeepReach(t *testing.T) {
	boundary, ctx := collaborationTestBoundary(false, nil)
	require.NoError(t, boundary.Authorize(ctx, "target-ws", models.CollaborationControl))
	require.NoError(t, boundary.Authorize(context.Background(), "target-ws", models.CollaborationRead))

	boundary, ctx = collaborationTestBoundary(true, nil)
	require.NoError(t, boundary.Authorize(context.Background(), "target-ws", models.CollaborationControl))
	require.NoError(t, boundary.Authorize(ctx, "source-ws", models.CollaborationControl))
}

func TestWorkspaceCollaborationGrantsAreIndependent(t *testing.T) {
	boundary, ctx := collaborationTestBoundary(true, &models.WorkspaceCollaborationGrant{Read: true, Report: true})
	require.NoError(t, boundary.Authorize(ctx, "target-ws", models.CollaborationRead))
	require.NoError(t, boundary.Authorize(ctx, "target-ws", models.CollaborationReport))
	require.True(t, taskservice.IsForbidden(boundary.Authorize(ctx, "target-ws", models.CollaborationPrompt)))
	require.True(t, taskservice.IsForbidden(boundary.Authorize(ctx, "target-ws", models.CollaborationControl)))
}

func TestWorkspaceCollaborationFailsClosed(t *testing.T) {
	boundary, ctx := collaborationTestBoundary(true, nil)
	require.True(t, taskservice.IsForbidden(boundary.Authorize(ctx, "target-ws", models.CollaborationRead)))

	lookup := boundary.store.(*fakeCollaborationLookup)
	lookup.grantErr = errors.New("store unavailable")
	require.ErrorIs(t, boundary.Authorize(ctx, "target-ws", models.CollaborationRead), taskservice.ErrWorkspaceCollaborationUnavailable)

	lookup = &fakeCollaborationLookup{
		task:    &models.Task{ID: "caller-task", WorkspaceID: "source-ws"},
		session: &models.TaskSession{ID: "caller-session", TaskID: "other-task"},
	}
	boundary = &workspaceCollaborationBoundary{enabled: true, store: lookup}
	require.ErrorIs(t, boundary.Authorize(ctx, "target-ws", models.CollaborationRead), taskservice.ErrWorkspaceCollaborationUnavailable)

	lookup.session = &models.TaskSession{ID: "caller-session", TaskID: "caller-task"}
	lookup.sessionErr = errors.New("session unavailable")
	require.ErrorIs(t, boundary.Authorize(ctx, "target-ws", models.CollaborationRead), taskservice.ErrWorkspaceCollaborationUnavailable)
}

func TestWorkspaceCollaborationPluginCallRequiresConversationCaller(t *testing.T) {
	boundary, ctx := collaborationTestBoundary(true, nil)
	require.NoError(t, boundary.validatePluginCall(context.Background(), false))
	require.Error(t, boundary.validatePluginCall(context.Background(), true))
	require.NoError(t, boundary.validatePluginCall(ctx, true))
	require.NoError(t, boundary.validatePluginCall(ctx, false))
}
