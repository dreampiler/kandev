package sqlite

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/testutil"
	"github.com/stretchr/testify/require"
)

// PostgreSQL runs the failed-materialization reclaim against real row locks.
// It skips unless KANDEV_TEST_POSTGRES_DSN is configured.
func TestPostgresReclaimFailedTaskEnvironmentMaterialization(t *testing.T) {
	db := openIsolatedPostgresMultiConn(t, testutil.PostgresDSNFromEnv(t), 3)
	repo, err := NewWithDB(db, db, nil)
	if err != nil {
		t.Fatalf("init postgres schema: %v", err)
	}
	ctx := context.Background()

	const taskID, environmentID = "task-reclaim-pg", "environment-reclaim-pg"
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: "workspace-reclaim-pg", Name: "Reclaim PG"}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{ID: taskID, WorkspaceID: "workspace-reclaim-pg", Title: "Reclaim PG"}))
	require.NoError(t, repo.CreateTaskEnvironment(ctx, &models.TaskEnvironment{
		ID: environmentID, TaskID: taskID, ExecutorType: string(models.ExecutorTypeWorktree),
		Status: models.TaskEnvironmentStatusCreating,
	}))

	// Reach the failed state the way the ownership closure writes do: a full
	// row update from creating to failed with the owner cleared.
	seed, err := repo.GetTaskEnvironment(ctx, environmentID)
	require.NoError(t, err)
	seed.Status = models.TaskEnvironmentStatusFailed
	seed.MaterializationSessionID = ""
	require.NoError(t, repo.UpdateTaskEnvironment(ctx, seed))

	reclaimed, err := repo.ReclaimFailedTaskEnvironmentMaterialization(ctx, environmentID, "session-reclaim-pg")
	require.NoError(t, err)
	require.True(t, reclaimed)
	persisted, err := repo.GetTaskEnvironment(ctx, environmentID)
	require.NoError(t, err)
	require.Equal(t, models.TaskEnvironmentStatusCreating, persisted.Status)
	require.Equal(t, "session-reclaim-pg", persisted.MaterializationSessionID)

	// A second reclaim loses against the re-elected owner.
	reclaimed, err = repo.ReclaimFailedTaskEnvironmentMaterialization(ctx, environmentID, "session-reclaim-pg-2")
	require.NoError(t, err)
	require.False(t, reclaimed)
}
