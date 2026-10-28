package sqlite

import (
	"context"
	"fmt"

	"github.com/kandev/kandev/internal/office/models"
)

// GetRoutineTrigger reads the current trigger from the writer connection so
// schedule changes do not start from a delayed replica snapshot.
func (r *Repository) GetRoutineTrigger(ctx context.Context, id string) (*models.RoutineTrigger, error) {
	var trigger models.RoutineTrigger
	if err := r.db.QueryRowxContext(ctx, r.db.Rebind(
		`SELECT * FROM office_routine_triggers WHERE id = ?`), id).StructScan(&trigger); err != nil {
		return nil, err
	}
	return &trigger, nil
}

// UpdateRoutineTriggerIfCurrent only changes editable schedule fields. The
// scheduler's claim clears next_run_at, causing this comparison to lose without
// changing the claimed fire or its eventual re-arm.
func (r *Repository) UpdateRoutineTriggerIfCurrent(
	ctx context.Context, before, after *models.RoutineTrigger,
) (bool, error) {
	if before == nil || after == nil {
		return false, fmt.Errorf("routine trigger update requires current and proposed rows")
	}
	query := `UPDATE office_routine_triggers
		SET cron_expression = ?, timezone = ?, enabled = ?, next_run_at = ?, updated_at = ?
		WHERE id = ? AND routine_id = ? AND kind = ?
		  AND cron_expression = ? AND timezone = ? AND enabled = ?`
	args := []any{after.CronExpression, after.Timezone, boolToInt(after.Enabled), after.NextRunAt, after.UpdatedAt,
		before.ID, before.RoutineID, before.Kind, before.CronExpression, before.Timezone, boolToInt(before.Enabled)}
	if before.NextRunAt == nil {
		query += ` AND next_run_at IS NULL`
	} else {
		query += ` AND next_run_at = ?`
		args = append(args, *before.NextRunAt)
	}
	result, err := r.db.ExecContext(ctx, r.db.Rebind(query), args...)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}
