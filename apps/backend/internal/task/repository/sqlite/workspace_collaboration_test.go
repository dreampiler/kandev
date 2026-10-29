package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
)

func TestWorkspaceCollaborationGrantRoundTripAndCascade(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()

	sourceID := uuid.New().String()
	targetID := uuid.New().String()
	taskID := uuid.New().String()
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: sourceID, Name: "Source"}))
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: targetID, Name: "Target"}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{ID: taskID, WorkspaceID: sourceID, Title: "Caller"}))

	grant := &models.WorkspaceCollaborationGrant{
		CallerTaskID: taskID, SourceWorkspaceID: sourceID, TargetWorkspaceID: targetID,
		Read: true, Report: true, UpdatedBy: "user-1", UpdatedAt: time.Now().UTC(),
	}
	require.NoError(t, repo.UpsertWorkspaceCollaborationGrant(ctx, grant))

	stored, err := repo.GetWorkspaceCollaborationGrant(ctx, taskID, sourceID, targetID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.True(t, stored.Read)
	require.True(t, stored.Report)
	require.False(t, stored.Prompt)
	require.False(t, stored.Control)

	grant.Read, grant.Report = false, false
	grant.Prompt, grant.Control = true, true
	require.NoError(t, repo.UpsertWorkspaceCollaborationGrant(ctx, grant))
	list, err := repo.ListWorkspaceCollaborationGrants(ctx, taskID, sourceID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.True(t, list[0].Prompt)
	require.True(t, list[0].Control)
	require.False(t, list[0].Read)

	require.NoError(t, repo.DeleteTask(ctx, taskID))
	stored, err = repo.GetWorkspaceCollaborationGrant(ctx, taskID, sourceID, targetID)
	require.NoError(t, err)
	require.Nil(t, stored, "deleting the caller task must cascade its grants")
}

func TestWorkspaceCollaborationGrantCascadesOnTargetWorkspaceDelete(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()

	sourceID := uuid.New().String()
	targetID := uuid.New().String()
	taskID := uuid.New().String()
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: sourceID, Name: "Source"}))
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: targetID, Name: "Target"}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{ID: taskID, WorkspaceID: sourceID, Title: "Caller"}))
	require.NoError(t, repo.UpsertWorkspaceCollaborationGrant(ctx, &models.WorkspaceCollaborationGrant{
		CallerTaskID: taskID, SourceWorkspaceID: sourceID, TargetWorkspaceID: targetID,
		Read: true, UpdatedBy: "user-1", UpdatedAt: time.Now().UTC(),
	}))

	require.NoError(t, repo.DeleteWorkspace(ctx, targetID))
	stored, err := repo.GetWorkspaceCollaborationGrant(ctx, taskID, sourceID, targetID)
	require.NoError(t, err)
	require.Nil(t, stored, "deleting the target workspace must cascade its grants")
}
