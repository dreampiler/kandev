package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/kandev/kandev/internal/db/dialect"
)

func primarySessionNotPromoted(sessionID string, requireNonterminal bool) (bool, error) {
	if requireNonterminal {
		return false, nil
	}
	return false, fmt.Errorf("session not found: %s", sessionID)
}

func (r *Repository) lockNonterminalPrimarySession(ctx context.Context, tx *sqlx.Tx, sessionID string) (bool, error) {
	query := `SELECT id FROM task_sessions WHERE id = ? AND state IN ('CREATED', 'STARTING', 'RUNNING', 'IDLE', 'WAITING_FOR_INPUT')`
	if dialect.IsPostgres(r.db.DriverName()) {
		query += ` FOR UPDATE`
	}
	var lockedSessionID string
	err := tx.QueryRowContext(ctx, r.db.Rebind(query), sessionID).Scan(&lockedSessionID)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return true, err
}

// SetSessionPrimaryIfCurrentPrimaryTerminal marks a session primary only while
// the task has no live primary: every current primary is terminal
// (FAILED/CANCELLED/COMPLETED) or none is set. It is the atomic succession
// boundary for a spawned successor, so a concurrent spawn whose successor is
// already nonterminal cannot be overwritten. The target itself must remain
// nonterminal, so a session that turned terminal between creation and this
// call is never installed as primary. It reports whether it promoted.
func (r *Repository) SetSessionPrimaryIfCurrentPrimaryTerminal(
	ctx context.Context,
	sessionID string,
) (bool, error) {
	return r.setSessionPrimary(ctx, sessionID, true, true, nil)
}

// hasLivePrimarySession reports whether the task already holds a nonterminal
// primary session other than excludeSessionID. It runs inside the promotion
// transaction while the owning task lock is held, so the answer is stable for
// the demote/promote pair that follows on both SQLite's single writer and
// Postgres' task-row lock.
func (r *Repository) hasLivePrimarySession(
	ctx context.Context, tx *sqlx.Tx, taskID, excludeSessionID string,
) (bool, error) {
	query := `SELECT 1 FROM task_sessions
		WHERE task_id = ? AND is_primary = 1 AND id <> ?
		AND state IN ('CREATED', 'STARTING', 'RUNNING', 'IDLE', 'WAITING_FOR_INPUT')
		LIMIT 1`
	var one int
	err := tx.QueryRowContext(ctx, r.db.Rebind(query), taskID, excludeSessionID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
