package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/kandev/kandev/internal/task/models"
)

// errDeadOwnerRecoveryPending marks a dead-owner candidate that still has
// restart or recovery evidence. It is an internal guard signal, never
// returned to callers: the entry point converts it to (false, nil).
var errDeadOwnerRecoveryPending = errors.New("dead owner has pending recovery evidence")

// Dead-owner settlement grace: the owner session must have been terminal for
// at least the payload grace before its stranded submission settles. This
// reuses the retention payload grace so a recently terminated owner still has
// its normal recovery window.
const agentDeliveryDeadOwnerGrace = agentDeliveryRetentionPayloadGrace

// agentDeliveryResidualScanLimit bounds one residual work-queue scan so the
// startup/reconcile/session-terminal entry points stay cheap. Repeated passes
// converge as earlier candidates settle.
const agentDeliveryResidualScanLimit = 200

// ListActiveAgentDeliverySubmissionIDs returns a bounded, ordered work queue of
// submission IDs still in an automatic-dispatch state (prepared, dispatching,
// accepted, interrupted_unknown). sessionID == "" scans every session. The
// list is only a work queue: every eligibility guard is re-checked inside
// SettleDeadOwnerAgentDeliverySubmission, so a returned id is never itself a
// settlement decision.
func (r *Repository) ListActiveAgentDeliverySubmissionIDs(
	ctx context.Context,
	sessionID string,
	limit int,
) ([]string, error) {
	if limit <= 0 || limit > agentDeliveryResidualScanLimit {
		limit = agentDeliveryResidualScanLimit
	}
	query := `SELECT id FROM agent_delivery_submissions WHERE state IN (?, ?, ?, ?)`
	args := []any{
		string(models.DeliverySubmissionPrepared),
		string(models.DeliverySubmissionDispatching),
		string(models.DeliverySubmissionAccepted),
		string(models.DeliverySubmissionInterruptedUnknown),
	}
	if sessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	query += ` ORDER BY updated_at, id LIMIT ?`
	args = append(args, limit)
	ids := make([]string, 0, limit)
	if err := r.db.SelectContext(ctx, &ids, r.db.Rebind(query), args...); err != nil {
		return nil, fmt.Errorf("list active agent delivery submissions: %w", err)
	}
	return ids, nil
}

// SettleDeadOwnerAgentDeliverySubmission settles an active-state submission
// whose owner session is dead, through the existing terminal settlement path.
// All guards are checked inside one bounded writer transaction:
//
//   - submission is active (prepared/dispatching/accepted/interrupted_unknown)
//   - owner session is CANCELLED, missing, or COMPLETED/FAILED with no open
//     restart or recovery evidence (open agent_delivery recovery blocks, or
//     queued/dispatch/send-now signals for the session)
//   - the owner session turned terminal at least the payload grace ago
//   - a projected terminal inbox row exists for the submission (the pending
//     terminal effect is derived from that row, never invented)
//
// It returns (false, nil) when any guard fails. No new submission state is
// introduced; the submission reaches its existing terminal outcome.
func (r *Repository) SettleDeadOwnerAgentDeliverySubmission(
	ctx context.Context,
	submissionID string,
) (bool, error) {
	if submissionID == "" {
		return false, fmt.Errorf("submission id is required")
	}
	now := r.nowUTC()
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var sessionID, incarnationID string
	var generation int64
	var state models.DeliverySubmissionState
	err = tx.QueryRowxContext(ctx, r.db.Rebind(`
		SELECT session_id, incarnation_id, harness_generation, state
		FROM agent_delivery_submissions WHERE id = ?`),
		submissionID).Scan(&sessionID, &incarnationID, &generation, &state)
	if err == sql.ErrNoRows {
		return false, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	switch state {
	case models.DeliverySubmissionPrepared, models.DeliverySubmissionDispatching,
		models.DeliverySubmissionAccepted, models.DeliverySubmissionInterruptedUnknown:
	default:
		return false, tx.Commit()
	}
	if err := lockSessionTurnWrites(ctx, tx, r.db.DriverName(), sessionID); err != nil {
		return false, err
	}
	if err := lockAgentDeliveryWrites(ctx, tx, r.db.DriverName(), []string{sessionID}, nil); err != nil {
		return false, err
	}
	eligible, err := deadOwnerSubmissionEligibleTx(ctx, tx, r.db.Rebind, sessionID, incarnationID, generation, submissionID, now)
	if err != nil {
		return false, err
	}
	if !eligible {
		return false, tx.Commit()
	}
	terminal, streamID, sequence, err := deadOwnerTerminalEffectTx(ctx, tx, r.db.Rebind, sessionID, incarnationID, generation, submissionID)
	if err != nil {
		return false, err
	}
	if terminal == nil {
		return false, tx.Commit()
	}
	if _, err := insertDeliveryEffectTx(ctx, tx, r.db.Rebind, terminal); err != nil {
		return false, err
	}
	// settleAgentDeliveryTerminalTx commits the writer transaction on every
	// path that mutates (including the settled path below), so its settled
	// result is final and must not be committed again here.
	return r.settleAgentDeliveryTerminalTx(ctx, tx, streamID, sequence, models.DeliverySubmissionState(terminal.Outcome), now)
}

// deadOwnerSubmissionEligibleTx verifies every Task 01 guard inside the
// caller's writer transaction. A missing owner qualifies on grace alone only
// when no recovery evidence references the submission; CANCELLED owners
// qualify on grace alone; COMPLETED/FAILED owners additionally require zero
// open agent_delivery recovery blocks and zero queued/dispatch/send-now
// signals for the session. Anything else (live, unclear) is ineligible.
func deadOwnerSubmissionEligibleTx(
	ctx context.Context,
	tx *sqlx.Tx,
	rebind func(string) string,
	sessionID, incarnationID string,
	generation int64,
	submissionID string,
	now time.Time,
) (bool, error) {
	var ownerState models.TaskSessionState
	var completedAt sql.NullTime
	var updatedAt time.Time
	err := tx.QueryRowxContext(ctx, rebind(`
		SELECT state, completed_at, updated_at FROM task_sessions WHERE id = ?`),
		sessionID).Scan(&ownerState, &completedAt, &updatedAt)
	if err == sql.ErrNoRows {
		if err := deadOwnerRecoveryQuiescedTx(ctx, tx, rebind, sessionID, incarnationID, generation, submissionID); err != nil {
			if errors.Is(err, errDeadOwnerRecoveryPending) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	switch ownerState {
	case models.TaskSessionStateCancelled:
	case models.TaskSessionStateCompleted, models.TaskSessionStateFailed:
		if err := deadOwnerRecoveryQuiescedTx(ctx, tx, rebind, sessionID, incarnationID, generation, submissionID); err != nil {
			if errors.Is(err, errDeadOwnerRecoveryPending) {
				return false, nil
			}
			return false, err
		}
	default:
		return false, nil
	}
	terminalAt := updatedAt
	if completedAt.Valid {
		terminalAt = completedAt.Time
	}
	if terminalAt.IsZero() || now.Sub(terminalAt) < agentDeliveryDeadOwnerGrace {
		return false, nil
	}
	return true, nil
}

// deadOwnerRecoveryQuiescedTx verifies a dead owner left no restart or
// recovery evidence: no open agent_delivery recovery block for the
// incarnation, and no queued, dispatch-claim or send-now signal for the
// session. Missing optional tables (other engines, partial installs) count as
// quiesced rather than failing, because the writer transaction already holds
// the session and delivery locks.
func deadOwnerRecoveryQuiescedTx(
	ctx context.Context,
	tx *sqlx.Tx,
	rebind func(string) string,
	sessionID, incarnationID string,
	generation int64,
	submissionID string,
) error {
	var openBlocks int
	if err := tx.QueryRowxContext(ctx, rebind(`
		SELECT COUNT(*) FROM session_recovery_blocks
		WHERE session_id = ? AND incarnation_id = ? AND expected_generation = ?
		  AND consumer_reference = 'agent_delivery' AND state = 'open'`),
		sessionID, incarnationID, generation).Scan(&openBlocks); err != nil {
		return err
	}
	if openBlocks > 0 {
		return errDeadOwnerRecoveryPending
	}
	for _, probe := range []struct {
		table, sessionColumn string
	}{
		{"queued_messages", "session_id"},
		{"queue_dispatch_claims", "session_id"},
		{"queue_send_now_claims", "session_id"},
	} {
		pending, err := deadOwnerSignalTableHasSessionTx(ctx, tx, rebind, probe.table, probe.sessionColumn, sessionID)
		if err != nil {
			return err
		}
		if pending {
			return errDeadOwnerRecoveryPending
		}
	}
	_ = submissionID
	return nil
}

// deadOwnerSignalTableHasSessionTx counts session signals in an optional
// table. A missing table is quiesced, not an error.
func deadOwnerSignalTableHasSessionTx(
	ctx context.Context,
	tx *sqlx.Tx,
	rebind func(string) string,
	table, sessionColumn, sessionID string,
) (bool, error) {
	var pending int
	err := tx.QueryRowxContext(ctx, rebind(
		`SELECT COUNT(*) FROM `+table+` WHERE `+sessionColumn+` = ?`),
		sessionID).Scan(&pending)
	if err != nil {
		if isDeadOwnerMissingTableError(err) {
			return false, nil
		}
		return false, err
	}
	return pending > 0, nil
}

// isDeadOwnerMissingTableError reports a missing-table error on either
// engine without matching error text outside the table-not-found case.
func isDeadOwnerMissingTableError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "undefined_table")
}

// deadOwnerTerminalEffectTx loads the oldest projected terminal inbox row for
// the submission and derives its pending terminal effect. A nil effect means
// no terminal row exists and the submission stays untouched: the effect is
// derived from a retained row, never invented.
func deadOwnerTerminalEffectTx(
	ctx context.Context,
	tx *sqlx.Tx,
	rebind func(string) string,
	sessionID, incarnationID string,
	generation int64,
	submissionID string,
) (*models.AgentDeliveryEffect, string, int64, error) {
	var event models.AgentDeliveryEvent
	var terminal int
	var projectedAt sql.NullTime
	err := tx.QueryRowxContext(ctx, rebind(`
		SELECT stream_id, sequence, session_id, incarnation_id, harness_generation,
		       submission_id, event_type, payload, terminal, received_at, projected_at,
		       content_hash, hash_version, payload_pruned_at, payload_bytes
		FROM agent_delivery_inbox
		WHERE session_id = ? AND incarnation_id = ? AND harness_generation = ?
		  AND submission_id = ? AND terminal <> 0 AND projected_at IS NOT NULL
		ORDER BY sequence ASC LIMIT 1`),
		sessionID, incarnationID, generation, submissionID).Scan(
		&event.StreamID, &event.Sequence, &event.SessionID, &event.IncarnationID,
		&event.HarnessGeneration, &event.SubmissionID, &event.EventType, &event.Payload,
		&terminal, &event.ReceivedAt, &projectedAt,
		&event.ContentHash, &event.HashVersion, &event.PayloadPrunedAt, &event.PayloadBytes)
	if err == sql.ErrNoRows {
		return nil, "", 0, nil
	}
	if err != nil {
		return nil, "", 0, err
	}
	event.Terminal = terminal != 0
	terminalEffect, err := deliveryTerminalSettlementEffect(&event)
	if err != nil {
		return nil, "", 0, err
	}
	if terminalEffect == nil {
		return nil, "", 0, nil
	}
	return terminalEffect, event.StreamID, event.Sequence, nil
}
