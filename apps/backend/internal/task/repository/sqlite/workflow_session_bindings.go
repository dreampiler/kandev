package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/kandev/kandev/internal/task/models"
)

const workflowSessionBindingsTableDDL = `
	CREATE TABLE IF NOT EXISTS task_workflow_session_bindings (
		task_id TEXT NOT NULL,
		target_key TEXT NOT NULL,
		workflow_id TEXT NOT NULL,
		agent_profile_id TEXT NOT NULL,
		session_id TEXT,
		operation_id TEXT NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		PRIMARY KEY (task_id, target_key),
		FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
		FOREIGN KEY (session_id) REFERENCES task_sessions(id) ON DELETE SET NULL
	);
	CREATE INDEX IF NOT EXISTS idx_task_workflow_session_bindings_workflow
		ON task_workflow_session_bindings(workflow_id, target_key);
	CREATE INDEX IF NOT EXISTS idx_task_workflow_session_bindings_session
		ON task_workflow_session_bindings(session_id);
`

func (r *Repository) migrateWorkflowSessionBindings() error {
	return r.migrate.Apply("task_workflow_session_bindings.table", workflowSessionBindingsTableDDL)
}

// GetWorkflowSessionBinding loads the latest committed binding for a source
// step. A missing row is a valid skipped-source state.
func (r *Repository) GetWorkflowSessionBinding(
	ctx context.Context,
	taskID, targetKey string,
) (*models.WorkflowSessionBinding, error) {
	var binding models.WorkflowSessionBinding
	var sessionID sql.NullString
	err := r.ro.QueryRowxContext(ctx, r.ro.Rebind(`
		SELECT task_id, target_key, workflow_id, agent_profile_id,
		       session_id, operation_id, updated_at
		FROM task_workflow_session_bindings
		WHERE task_id = ? AND target_key = ?
	`), taskID, targetKey).Scan(
		&binding.TaskID,
		&binding.TargetKey,
		&binding.WorkflowID,
		&binding.AgentProfileID,
		&sessionID,
		&binding.OperationID,
		&binding.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load workflow session binding: %w", err)
	}
	if sessionID.Valid {
		binding.SessionID = sessionID.String
	}
	return &binding, nil
}

// workflowSessionBindingInsert is the shared insert prefix for both upsert
// shapes; the caller supplies the ON CONFLICT body that decides whether an
// existing row is replaced.
const workflowSessionBindingInsert = `
		INSERT INTO task_workflow_session_bindings
			(task_id, target_key, workflow_id, agent_profile_id, session_id, operation_id, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (task_id, target_key) DO UPDATE SET
`

// workflowSessionBindingOrderedConflict compares immutable entry identities
// (and timestamps for legacy rows) so a superseded step entry cannot overwrite
// a newer one.
const workflowSessionBindingOrderedConflict = `
			workflow_id = EXCLUDED.workflow_id,
			agent_profile_id = EXCLUDED.agent_profile_id,
			session_id = EXCLUDED.session_id,
			operation_id = EXCLUDED.operation_id,
			updated_at = EXCLUDED.updated_at
		WHERE (
			(task_workflow_session_bindings.operation_id LIKE 'workflow-step-entry-v2:%'
				AND (
					task_workflow_session_bindings.operation_id < EXCLUDED.operation_id
					OR (
						task_workflow_session_bindings.operation_id = EXCLUDED.operation_id
						AND task_workflow_session_bindings.updated_at < EXCLUDED.updated_at
					)
				))
			OR (task_workflow_session_bindings.operation_id NOT LIKE 'workflow-step-entry-v2:%'
				AND task_workflow_session_bindings.updated_at < EXCLUDED.updated_at)
		)
	`

// workflowSessionBindingSessionConflict overwrites only the session and
// timestamp, leaving the workflow, profile, and operation identity intact.
const workflowSessionBindingSessionConflict = `
			session_id = EXCLUDED.session_id,
			updated_at = EXCLUDED.updated_at
	`

// UpsertWorkflowSessionBinding commits a source-step session choice. Versioned
// step-entry operations compare their immutable entry identity inside the same
// upsert statement, while legacy operations retain timestamp ordering. The
// bool reports whether this operation won the conditional write.
func (r *Repository) UpsertWorkflowSessionBinding(
	ctx context.Context,
	binding *models.WorkflowSessionBinding,
) (bool, error) {
	return r.upsertWorkflowSessionBinding(ctx, binding, workflowSessionBindingOrderedConflict)
}

// SetWorkflowSessionBindingSession moves a binding's session without the
// step-entry ordering guard UpsertWorkflowSessionBinding applies. It serves an
// explicit session promotion, which re-homes the session that drives a step but
// is not a new workflow entry: the guard would compare equal operation
// identities and equal timestamps, reject the write, and leave the binding on
// the replaced session. An absent row is inserted with the supplied identity; an
// existing row keeps its workflow, profile, and operation identity and only its
// session and timestamp change.
func (r *Repository) SetWorkflowSessionBindingSession(
	ctx context.Context,
	binding *models.WorkflowSessionBinding,
) (bool, error) {
	return r.upsertWorkflowSessionBinding(ctx, binding, workflowSessionBindingSessionConflict)
}

func (r *Repository) upsertWorkflowSessionBinding(
	ctx context.Context,
	binding *models.WorkflowSessionBinding,
	conflictClause string,
) (bool, error) {
	if err := validateWorkflowSessionBinding(binding); err != nil {
		return false, err
	}
	if binding.UpdatedAt.IsZero() {
		binding.UpdatedAt = time.Now().UTC()
	}

	result, err := r.db.ExecContext(ctx, r.db.Rebind(workflowSessionBindingInsert+conflictClause),
		binding.TaskID,
		binding.TargetKey,
		binding.WorkflowID,
		binding.AgentProfileID,
		nullableBindingSessionID(binding.SessionID),
		binding.OperationID,
		binding.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("upsert workflow session binding: %w", err)
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

func validateWorkflowSessionBinding(binding *models.WorkflowSessionBinding) error {
	if binding == nil || binding.TaskID == "" || binding.TargetKey == "" {
		return fmt.Errorf("workflow session binding identity is required")
	}
	if binding.WorkflowID == "" || binding.AgentProfileID == "" || binding.OperationID == "" {
		return fmt.Errorf("workflow session binding identity is required")
	}
	return nil
}

func nullableBindingSessionID(sessionID string) interface{} {
	if sessionID == "" {
		return nil
	}
	return sessionID
}
