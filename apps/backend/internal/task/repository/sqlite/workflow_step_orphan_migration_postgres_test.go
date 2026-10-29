package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/testutil"
)

// TestPostgresWorkflowStepOrphanPruneSerializesAcrossConnections proves that
// concurrent startup cleanups take the transaction-scoped advisory lock before
// reading the one-time marker, so only the first starter prunes and both
// observe a committed marker.
func TestPostgresWorkflowStepOrphanPruneSerializesAcrossConnections(t *testing.T) {
	db := openIsolatedPostgresMultiConn(t, testutil.PostgresDSNFromEnv(t), 4)
	repo, err := NewWithDB(db, db, nil)
	require.NoError(t, err)
	ctx := context.Background()

	seedWorkspace(t, repo, "ws-pg-workflow-orphan-prune")
	insertWorkflowDeleteTestStep(t, repo, "step-pg-workflow-prune-orphan", "wf-pg-workflow-prune-orphan")
	_, err = db.ExecContext(ctx, db.Rebind(`
		DELETE FROM kandev_meta WHERE key = ?
	`), workflowStepOrphansPrunedMetaKey)
	require.NoError(t, err)

	lock, err := db.BeginTxx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = lock.Rollback() }()
	lockKey := "workflow_step_orphan_migration:" + workflowStepOrphansPrunedMetaKey
	_, err = lock.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey)
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			results <- repo.pruneOrphanWorkflowStepsOnce()
		}()
	}
	close(start)
	if err := waitForWorkflowPruneLockWaiters(ctx, db, 2); err != nil {
		_ = lock.Rollback()
		<-results
		<-results
		t.Fatalf("wait for both cleanups to block on the advisory lock: %v", err)
	}
	require.NoError(t, lock.Commit())
	errs := []error{<-results, <-results}
	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 0, countWorkflowDeleteTestSteps(t, repo, "wf-pg-workflow-prune-orphan"))
	require.Equal(t, 1, countWorkflowStepOrphanMarker(t, repo))
}

func waitForWorkflowPruneLockWaiters(ctx context.Context, db *sqlx.DB, want int) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiters int
		if err := db.GetContext(ctx, &waiters, `
			SELECT COUNT(*) FROM pg_stat_activity
			WHERE pid <> pg_backend_pid()
				AND datname = current_database()
				AND wait_event_type = 'Lock'
				AND query LIKE '%pg_advisory_xact_lock%'
		`); err != nil {
			return err
		}
		if waiters >= want {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
