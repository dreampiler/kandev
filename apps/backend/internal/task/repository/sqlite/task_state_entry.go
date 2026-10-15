package sqlite

import (
	"context"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// UpdateTaskStateIfCurrentEntry writes state only while the observed workflow
// arrival, task revision and prior state still own the row. The task lock is
// acquired before reading the ledger so a concurrent move cannot change entry
// identity between validation and mutation.
func (r *Repository) UpdateTaskStateIfCurrentEntry(
	ctx context.Context, observed *models.Task, state v1.TaskState,
) (bool, time.Time, error) {
	if observed == nil || observed.UpdatedAt.IsZero() {
		return false, time.Time{}, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, time.Time{}, err
	}
	defer func() { _ = tx.Rollback() }()
	workflowID, stepID, found, err := r.readTaskStepInTx(ctx, tx, observed.ID)
	if err != nil || !found || workflowID != observed.WorkflowID || stepID != observed.WorkflowStepID {
		return false, time.Time{}, err
	}
	entryID, err := r.getLatestTaskStepTransitionIDTx(ctx, tx, observed.ID)
	if err != nil || entryID != observed.WorkflowStepTransitionID {
		return false, time.Time{}, err
	}
	if err := r.guardTaskCompletionTransitionTx(ctx, tx, observed.ID, observed.State, state,
		workflowID, stepID, workflowID, stepID); err != nil {
		return false, time.Time{}, err
	}
	updatedAt := r.nowUTC()
	result, err := tx.ExecContext(ctx, r.db.Rebind(`
		UPDATE tasks SET state = ?, updated_at = ?
		WHERE id = ? AND state = ? AND updated_at = ? AND archived_at IS NULL
	`), state, updatedAt, observed.ID, observed.State, observed.UpdatedAt)
	if err != nil {
		return false, time.Time{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows == 0 {
		return false, time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return false, time.Time{}, err
	}
	return true, updatedAt, nil
}
