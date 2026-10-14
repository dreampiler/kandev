package service

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
	"github.com/stretchr/testify/require"
)

func TestCreateTaskExecutorProfileReferences(t *testing.T) {
	for _, source := range []string{"typed", "metadata", "deferred"} {
		for _, start := range []bool{false, true} {
			t.Run(source+map[bool]string{false: "_deferred", true: "_start"}[start], func(t *testing.T) {
				svc, _, repo := createTestService(t)
				ctx := context.Background()
				require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: "profile-ws", Name: "Workspace"}))
				require.NoError(t, repo.CreateWorkflow(ctx, &models.Workflow{ID: "profile-wf", WorkspaceID: "profile-ws", Name: "Workflow"}))
				req := &CreateTaskRequest{WorkspaceID: "profile-ws", WorkflowID: "profile-wf", Title: "Task", StartAgent: start}
				switch source {
				case "typed":
					req.ExecutorProfileID = "missing-profile"
				case "metadata":
					req.Metadata = map[string]interface{}{models.MetaKeyExecutorProfileID: "missing-profile"}
				case "deferred":
					req.DeferredLaunch = map[string]interface{}{models.MetaKeyExecutorProfileID: "missing-profile"}
				}
				_, err := svc.CreateTask(ctx, req)
				require.ErrorIs(t, err, repoerrors.ErrExecutorProfileNotFound)
				require.Contains(t, err.Error(), "missing-profile")
				tasks, err := svc.ListTasks(ctx, "profile-wf")
				require.NoError(t, err)
				require.Empty(t, tasks)
				require.NoError(t, repo.CreateExecutor(ctx, &models.Executor{ID: "profile-exec", Name: "Local", Type: models.ExecutorTypeLocal}))
				require.NoError(t, repo.CreateExecutorProfile(ctx, &models.ExecutorProfile{ID: "valid-profile", ExecutorID: "profile-exec", Name: "Valid"}))
				req.ExecutorProfileID = "valid-profile"
				req.Metadata = nil
				req.DeferredLaunch = nil
				result, err := svc.CreateTask(ctx, req)
				require.NoError(t, err)
				require.NotNil(t, result.Task)
			})
		}
	}
}
