package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/testutil"
)

// TestPostgresDeleteWorkflowRemovesStepsAndPrunesOrphans runs the workflow
// delete and the one-time orphan prune against PostgreSQL. It is skipped by
// testutil when the isolated PostgreSQL DSN is not configured.
func TestPostgresDeleteWorkflowRemovesStepsAndPrunesOrphans(t *testing.T) {
	conn := testutil.OpenIsolatedPostgres(t, testutil.PostgresDSNFromEnv(t))
	repo, err := NewWithDB(conn, conn, nil)
	require.NoError(t, err)
	ctx := context.Background()

	doomed := createWorkflowDeleteStepsWorkflow(t, repo, "ws-1", "doomed")
	kept := createWorkflowDeleteStepsWorkflow(t, repo, "ws-1", "kept")
	insertWorkflowDeleteStep(t, conn, "pg-doomed-todo", doomed.ID)
	insertWorkflowDeleteStep(t, conn, "pg-kept-todo", kept.ID)
	require.NoError(t, repo.DeleteWorkflow(ctx, doomed.ID))
	require.Equal(t, 0, countWorkflowDeleteSteps(t, conn, doomed.ID))
	require.Equal(t, 1, countWorkflowDeleteSteps(t, conn, kept.ID))

	insertWorkflowDeleteStep(t, conn, "pg-orphan-todo", "deleted-workflow")
	_, err = conn.Exec(conn.Rebind(`DELETE FROM kandev_meta WHERE key = ?`), workflowStepOrphansPrunedMetaKey)
	require.NoError(t, err)
	_, err = NewWithDB(conn, conn, nil)
	require.NoError(t, err)
	require.Equal(t, 0, countWorkflowDeleteSteps(t, conn, "deleted-workflow"))
	require.Equal(t, 1, countWorkflowDeleteSteps(t, conn, kept.ID))
	var marker int
	require.NoError(t, conn.QueryRow(conn.Rebind(`SELECT COUNT(*) FROM kandev_meta WHERE key = ?`),
		workflowStepOrphansPrunedMetaKey).Scan(&marker))
	require.Equal(t, 1, marker)
}
