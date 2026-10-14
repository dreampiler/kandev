package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/kandev/kandev/internal/db/dialect"
	"github.com/kandev/kandev/internal/task/models"
)

type stepCompletionEntryQueryer interface {
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

func (r *Repository) GetTaskStepCompletionEntry(ctx context.Context, taskID string) (models.StepCompletionEntry, error) {
	return r.readStepCompletionEntry(ctx, r.ro, taskID)
}

func (r *Repository) readStepCompletionEntry(ctx context.Context, queryer stepCompletionEntryQueryer, taskID string) (models.StepCompletionEntry, error) {
	var entry models.StepCompletionEntry
	var occurredAt sql.NullTime
	err := queryer.QueryRowContext(ctx, r.db.Rebind(`
		SELECT COALESCE(t.workflow_step_id, ''), COALESCE(e.id, 0), e.occurred_at
		FROM tasks t LEFT JOIN task_step_transitions e ON e.id = (
			SELECT id FROM task_step_transitions WHERE task_id = t.id ORDER BY id DESC LIMIT 1
		) WHERE t.id = ?
	`), taskID).Scan(&entry.StepID, &entry.TransitionID, &occurredAt)
	if occurredAt.Valid {
		entry.OccurredAt = occurredAt.Time
	}
	return entry, err
}

func (r *Repository) stampTurnCompletionEntry(ctx context.Context, tx stepTransitionTx, turn *models.Turn) {
	entryID, err := r.getLatestTaskStepTransitionIDTx(ctx, tx, turn.TaskID)
	if err == nil && entryID > 0 {
		turn.Metadata[models.TurnMetaKeyWorkflowStepEntryAtStart] = entryID
	}
}

func (r *Repository) claimStepCompletionAtEntry(ctx context.Context, taskID, sessionID string, signal models.PendingStepCompletionSignal) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	_, stepID, found, err := r.readTaskStepInTx(ctx, tx, taskID)
	if err != nil {
		return false, err
	}
	if !found || stepID != signal.StepID {
		return false, models.ErrStepCompletionEntryChanged
	}
	entry, err := r.readStepCompletionEntry(ctx, tx, taskID)
	if err != nil {
		return false, err
	}
	if entry.TransitionID != signal.EntryTransitionID {
		return false, models.ErrStepCompletionEntryChanged
	}
	metadata, err := r.sessionSignalMetadataInTx(ctx, tx, taskID, sessionID)
	if err != nil {
		return false, err
	}
	claimed, err := completionSignalAlreadyClaimed(metadata, entry)
	if err != nil || claimed {
		return false, err
	}
	payload, err := json.Marshal(signal)
	if err != nil {
		return false, err
	}
	query := metadataKeyUpdateQuery("task_sessions", r.db.DriverName())
	args := metadataKeyUpdateArgs(r.db.DriverName(), models.SessionMetaKeyPendingStepCompletion, string(payload), r.nowUTC(), sessionID)
	if _, err := tx.ExecContext(ctx, r.db.Rebind(query), args...); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func completionSignalAlreadyClaimed(metadata map[string]interface{}, entry models.StepCompletionEntry) (bool, error) {
	pending, ok := models.LoadPendingStepSignal(metadata)
	if !ok || pending.StepID != entry.StepID {
		return false, nil
	}
	if pending.EntryTransitionID == 0 && (pending.SignaledAt.IsZero() || entry.OccurredAt.IsZero()) {
		return false, fmt.Errorf("pending completion signal has no entry identity or usable timestamp: %w", models.ErrStepCompletionEntryUnknown)
	}
	return models.PendingStepSignalMatchesEntry(pending, entry), nil
}

func (r *Repository) completionEntryStillCurrentInTx(ctx context.Context, tx *sql.Tx, taskID string) (bool, error) {
	expected, guarded := models.StepCompletionEntryFromContext(ctx, taskID)
	if !guarded {
		return true, nil
	}
	current, err := r.readStepCompletionEntry(ctx, tx, taskID)
	return current.StepID == expected.StepID && current.TransitionID == expected.TransitionID, err
}

func (r *Repository) sessionSignalMetadataInTx(ctx context.Context, tx *sql.Tx, taskID, sessionID string) (map[string]interface{}, error) {
	query := `SELECT task_id, metadata FROM task_sessions WHERE id = ?`
	if dialect.IsPostgres(r.db.DriverName()) {
		query += ` FOR UPDATE`
	}
	var owner string
	var raw sql.NullString
	if err := tx.QueryRowContext(ctx, r.db.Rebind(query), sessionID).Scan(&owner, &raw); err != nil {
		return nil, err
	}
	if owner != taskID {
		return nil, fmt.Errorf("completion session does not belong to task")
	}
	metadata := map[string]interface{}{}
	if raw.Valid && raw.String != "" {
		if err := json.Unmarshal([]byte(raw.String), &metadata); err != nil {
			return nil, fmt.Errorf("decode completion metadata: %w", err)
		}
	}
	return metadata, nil
}
