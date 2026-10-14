package messagequeue

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/kandev/kandev/internal/task/models"
)

func (r *sqliteRepository) claimOrphanedSessionTransferCompensationRecovery(
	ctx context.Context, compensation SessionTransferCompensation,
) (string, error) {
	if err := r.ensureQueueDispatchRecoverySchema(ctx); err != nil {
		return "", err
	}
	if err := r.ensureSendNowClaimRecoverySchema(ctx); err != nil {
		return "", err
	}
	return r.claimSessionTransferCompensation(ctx, compensation, true)
}

// sessionTransferCompensationOrphanedTx is checked while both queue locks are
// held. Expiry alone never makes a live session or an in-flight delivery orphaned.
func (r *sqliteRepository) sessionTransferCompensationOrphanedTx(
	ctx context.Context, tx *sqlx.Tx, c SessionTransferCompensation,
) (bool, error) {
	if !r.taskSessionsTablePresent || c.FromSessionID == c.ToSessionID {
		return false, nil
	}
	query := `SELECT state FROM task_sessions WHERE task_id = ? AND id IN (?, ?) ORDER BY id`
	if r.db.DriverName() == postgresDriverName {
		query += postgresForUpdateSuffix
	}
	var states []models.TaskSessionState
	if err := tx.SelectContext(ctx, &states, tx.Rebind(query), c.TaskID, c.FromSessionID, c.ToSessionID); err != nil {
		return false, fmt.Errorf("read session transfer endpoint states: %w", err)
	}
	if len(states) != 2 {
		return false, nil
	}
	for _, state := range states {
		switch state {
		case models.TaskSessionStateCompleted, models.TaskSessionStateFailed, models.TaskSessionStateCancelled:
		default:
			return false, nil
		}
	}
	var inFlight bool
	if err := tx.GetContext(ctx, &inFlight, tx.Rebind(`SELECT
		EXISTS (SELECT 1 FROM queue_dispatch_claims WHERE session_id IN (?, ?)) OR
		EXISTS (SELECT 1 FROM queue_send_now_claims WHERE session_id IN (?, ?))`),
		c.FromSessionID, c.ToSessionID, c.FromSessionID, c.ToSessionID); err != nil {
		return false, fmt.Errorf("read session transfer in-flight claims: %w", err)
	}
	if inFlight {
		return false, nil
	}
	for _, sessionID := range []string{c.FromSessionID, c.ToSessionID} {
		entries, err := r.listBySessionTx(ctx, tx, sessionID)
		if err != nil {
			return false, err
		}
		for _, entry := range entries {
			if entry.IsReservedInFlight() {
				return false, nil
			}
		}
	}
	return true, nil
}
