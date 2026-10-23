package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// workflowStepOrphansPrunedMetaKey is the kandev_meta key written once
// pruneOrphanWorkflowStepsOnce has removed the workflow_steps rows that
// earlier workflow deletes left behind.
const workflowStepOrphansPrunedMetaKey = "workflow_steps_orphans_pruned_at"

// pruneOrphanWorkflowStepsOnce deletes workflow_steps rows whose workflow no
// longer exists, then records workflowStepOrphansPrunedMetaKey so later boots
// skip it.
//
// The rows exist because the task repository initializes before the workflow
// repository: ensureRunnerProjectionTables creates workflow_steps without the
// foreign key the workflow repository's DDL declares, so the ON DELETE
// CASCADE that DeleteWorkflow relied on was never present and every deleted
// workflow left its steps behind. DeleteWorkflow and
// DeleteWorkflowsByWorkspace now delete the steps explicitly; this one-time
// pass removes what was already left. The DELETE is idempotent, so a boot
// interrupted before the marker commits simply runs it again.
func (r *Repository) pruneOrphanWorkflowStepsOnce() error {
	ctx := r.migrationContext()
	// persistence.Provide creates kandev_meta before any repository opens,
	// but repo-level tests build a bare DB through NewWithDB.
	if _, err := r.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS kandev_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		)`); err != nil {
		return fmt.Errorf("ensure kandev_meta: %w", err)
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin workflow step orphan prune: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var value string
	err = tx.QueryRowContext(ctx, r.db.Rebind(`SELECT value FROM kandev_meta WHERE key = ?`),
		workflowStepOrphansPrunedMetaKey).Scan(&value)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read %s: %w", workflowStepOrphansPrunedMetaKey, err)
	}
	result, err := tx.ExecContext(ctx, `
		DELETE FROM workflow_steps
		WHERE workflow_id NOT IN (SELECT id FROM workflows)
	`)
	if err != nil {
		return fmt.Errorf("prune orphan workflow steps: %w", err)
	}
	pruned, _ := result.RowsAffected()
	if _, err := tx.ExecContext(ctx, r.db.Rebind(`
		INSERT INTO kandev_meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO NOTHING
	`), workflowStepOrphansPrunedMetaKey, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("write %s: %w", workflowStepOrphansPrunedMetaKey, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit workflow step orphan prune: %w", err)
	}
	if r.log != nil {
		r.log.Info("migration applied",
			zap.String("name", "workflow_steps.prune_orphans"), zap.Int64("rows", pruned))
	}
	return nil
}
