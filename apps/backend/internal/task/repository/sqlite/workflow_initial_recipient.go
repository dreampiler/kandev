package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/kandev/kandev/internal/db/dialect"
	"github.com/kandev/kandev/internal/task/models"
)

func (r *Repository) SetSessionPrimaryWithInitialRecipientBinding(
	ctx context.Context, sessionID string, binding models.WorkflowInitialRecipientBinding,
) error {
	promoted, err := r.setSessionPrimary(ctx, sessionID, true, false, nil, binding)
	if err == nil && !promoted {
		return fmt.Errorf("initial recovery recipient is no longer eligible")
	}
	return err
}

func (r *Repository) validateInitialRecipientRouteWithExecutor(
	ctx context.Context, exec taskSessionExecutor, taskID string, route *models.WorkflowSessionRoute,
) error {
	if route.TargetKind != "initial" {
		return nil
	}
	var metadataJSON string
	if err := exec.QueryRowContext(ctx, r.db.Rebind(`SELECT metadata FROM tasks WHERE id = ?`), taskID).Scan(&metadataJSON); err != nil {
		return err
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return err
	}
	return models.ValidateInitialRecipientRoute(metadata, route)
}

func (r *Repository) commitInitialRecipientBindingTx(
	ctx context.Context, tx *sqlx.Tx, taskID, sessionID string,
	binding models.WorkflowInitialRecipientBinding, now time.Time,
) error {
	var metadataJSON string
	query := `SELECT metadata FROM tasks WHERE id = ? AND archived_at IS NULL AND NOT (` + isFromOfficeProjection("tasks") + `)`
	if err := tx.QueryRowContext(ctx, r.db.Rebind(query), taskID).Scan(&metadataJSON); err != nil {
		return fmt.Errorf("load initial recipient task: %w", err)
	}
	metadata := make(map[string]interface{})
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return fmt.Errorf("decode initial recipient task metadata: %w", err)
	}
	sessions, err := r.initialRecipientSessionsTx(ctx, tx, taskID)
	if err != nil {
		return err
	}
	if err := validateInitialRecipientSuccession(metadata, sessions, sessionID, binding); err != nil {
		return err
	}
	busy, err := r.initialRecipientSourceBusyTx(ctx, tx, binding.SourceSessionID)
	if err != nil {
		return err
	}
	if busy {
		return fmt.Errorf("initial recovery source still owns execution or an active turn")
	}
	return r.setTaskMetadataKeyWithExecutor(ctx, tx, taskID, models.MetaKeyWorkflowInitialRecipientBinding, binding, now)
}

// The owning task is already locked by primary promotion. Session locks follow
// that task lock, matching the existing state/promotion lock order.
func (r *Repository) initialRecipientSessionsTx(ctx context.Context, tx *sqlx.Tx, taskID string) ([]*models.TaskSession, error) {
	query := `SELECT id, COALESCE(agent_profile_id, ''), state, metadata, started_at FROM task_sessions WHERE task_id = ? ORDER BY id`
	if dialect.IsPostgres(r.db.DriverName()) {
		query += ` FOR UPDATE`
	}
	rows, err := tx.QueryContext(ctx, r.db.Rebind(query), taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var sessions []*models.TaskSession
	for rows.Next() {
		session := &models.TaskSession{TaskID: taskID}
		var metadataJSON string
		if err := rows.Scan(&session.ID, &session.AgentProfileID, &session.State, &metadataJSON, &session.StartedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(metadataJSON), &session.Metadata); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func validateInitialRecipientSuccession(
	metadata map[string]interface{}, sessions []*models.TaskSession, sessionID string,
	binding models.WorkflowInitialRecipientBinding,
) error {
	if binding.SessionID != sessionID || binding.SourceSessionID == sessionID || binding.AgentProfileID == "" {
		return fmt.Errorf("invalid initial recipient recovery identity")
	}
	initialID := initialRecipientProvenance(metadata, sessions)
	if initialID == "" || initialID != binding.InitialSessionID {
		return fmt.Errorf("initial recipient recovery provenance is unavailable or changed")
	}
	if err := validateInitialRecipientExpectedSource(metadata, initialID, binding); err != nil {
		return err
	}
	var source, destination *models.TaskSession
	for _, session := range sessions {
		if session.ID == binding.SourceSessionID {
			source = session
		}
		if session.ID == sessionID {
			destination = session
		}
	}
	if source == nil || destination == nil || destination.AgentProfileID != binding.AgentProfileID {
		return fmt.Errorf("initial recipient recovery session identity mismatch")
	}
	if source.State == models.TaskSessionStateRunning || source.State == models.TaskSessionStateStarting {
		return fmt.Errorf("initial recipient recovery source is active")
	}
	return nil
}

func validateInitialRecipientExpectedSource(metadata map[string]interface{}, initialID string, binding models.WorkflowInitialRecipientBinding) error {
	current, err := models.LoadWorkflowInitialRecipientBinding(metadata)
	if err != nil {
		return err
	}
	expectedSource := initialID
	if current != nil {
		if current.InitialSessionID != initialID {
			return fmt.Errorf("initial recipient binding provenance mismatch")
		}
		expectedSource = current.SessionID
		if *current == binding {
			expectedSource = binding.SourceSessionID
		}
	}
	if binding.SourceSessionID != expectedSource {
		return fmt.Errorf("initial recipient recovery source was superseded")
	}
	return nil
}

func initialRecipientProvenance(metadata map[string]interface{}, sessions []*models.TaskSession) string {
	if snapshot, ok := models.LoadWorkflowInitialSessionSnapshot(metadata); ok {
		return snapshot.SessionID
	}
	original, marked := models.MarkedOriginalTaskSession(sessions)
	if !marked {
		original = models.LegacyOriginalTaskSession(sessions)
	}
	if original != nil {
		return original.ID
	}
	return ""
}

func (r *Repository) initialRecipientSourceBusyTx(ctx context.Context, tx *sqlx.Tx, sessionID string) (bool, error) {
	predicate, _ := currentTurnAuthority(r.db.DriverName(), "turn_row")
	query := `SELECT EXISTS(SELECT 1 FROM task_session_turns turn_row
		WHERE task_session_id = ? AND completed_at IS NULL AND ` + predicate + `)
		OR EXISTS(SELECT 1 FROM executors_running WHERE session_id = ? AND status IN ('starting', 'running', 'ready', 'prepared'))`
	var busy bool
	err := tx.QueryRowContext(ctx, r.db.Rebind(query), sessionID, sessionID).Scan(&busy)
	return busy, err
}
