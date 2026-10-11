package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/kandev/kandev/internal/task/models"
)

const (
	defaultRetirementStreams = 25
	defaultRetirementRows    = 200
)

// RetireAgentDeliveryPrefix removes the contiguous projected-and-settled prefix
// of eligible streams and advances each stream's durable retired_sequence in
// the same transaction, together with the retired prefix's completed generic
// event receipts. Unprojected rows, rows younger than the grace window,
// submissions that are not settled, and open delivery recovery blocks stop a
// prefix early; nothing past a gap or a protected row is removed.
//
// Recovery authority is preserved: the received/projected cursors, submission
// identity, terminal settlement recordings and canonical conversation are
// untouched. Only redundant projected transport payloads and their generic
// receipts are deleted, and the retired_sequence fence prevents expired replay
// from being re-inserted or re-projected.
func (r *Repository) RetireAgentDeliveryPrefix(
	ctx context.Context,
	grace time.Duration,
	now time.Time,
	budget models.AgentDeliveryRetirementBudget,
) (models.AgentDeliveryRetirementResult, error) {
	var result models.AgentDeliveryRetirementResult
	if budget.Streams <= 0 {
		budget.Streams = defaultRetirementStreams
	}
	if budget.Rows <= 0 {
		budget.Rows = defaultRetirementRows
	}
	if now.IsZero() {
		now = r.nowUTC()
	}
	cutoff := now.Add(-grace)

	streams, err := r.listAgentDeliveryRetirementStreams(ctx, budget.Streams)
	if err != nil {
		return result, err
	}
	for _, streamID := range streams {
		removed, bytes, err := r.retireAgentDeliveryStream(ctx, streamID, cutoff, now, budget.Rows)
		if err != nil {
			return result, err
		}
		result.StreamsVisited++
		result.RowsRemoved += removed
		result.BytesRemoved += bytes
		if result.RowsRemoved >= int64(budget.Rows) {
			break
		}
	}
	return result, nil
}

// listAgentDeliveryRetirementStreams picks the streams with the oldest updated
// cursors that still have a projected prefix ahead of their retired sequence.
// Ordering by updated_at revisits a stream that keeps advancing only after less
// recently touched streams, so one busy stream cannot starve the others.
func (r *Repository) listAgentDeliveryRetirementStreams(ctx context.Context, limit int) ([]string, error) {
	rows, err := r.ro.QueryxContext(ctx, r.ro.Rebind(`
		SELECT stream_id FROM agent_delivery_cursors
		WHERE retired_sequence < projected_sequence
		ORDER BY updated_at ASC, stream_id ASC
		LIMIT ?`), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var streams []string
	for rows.Next() {
		var streamID string
		if err := rows.Scan(&streamID); err != nil {
			return nil, err
		}
		streams = append(streams, streamID)
	}
	return streams, rows.Err()
}

func (r *Repository) retireAgentDeliveryStream(
	ctx context.Context,
	streamID string,
	cutoff, now time.Time,
	maxRows int,
) (int64, int64, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	retired, projected, err := loadAgentDeliveryRetirementFrontierTx(ctx, tx, r.db.Rebind, streamID)
	if err != nil {
		return 0, 0, err
	}
	if retired >= projected {
		return 0, 0, nil
	}
	sequence, removed, bytes, err := r.retireAgentDeliveryPrefixTx(ctx, tx, streamID, retired, projected, cutoff, maxRows)
	if err != nil {
		return removed, bytes, err
	}
	if sequence == retired {
		return 0, 0, nil
	}
	if _, err := tx.ExecContext(ctx, r.db.Rebind(`
		UPDATE agent_delivery_cursors SET retired_sequence = ?, updated_at = ?
		WHERE stream_id = ?`), sequence, now, streamID); err != nil {
		return removed, bytes, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return removed, bytes, nil
}

func loadAgentDeliveryRetirementFrontierTx(
	ctx context.Context, tx *sqlx.Tx, rebind func(string) string, streamID string,
) (int64, int64, error) {
	var retired, projected int64
	err := tx.QueryRowxContext(ctx, rebind(`
		SELECT retired_sequence, projected_sequence FROM agent_delivery_cursors
		WHERE stream_id = ?`), streamID).Scan(&retired, &projected)
	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	return retired, projected, nil
}

// retireAgentDeliveryPrefixTx advances the contiguous prefix as far as the
// eligibility rules allow and returns the new retired sequence plus the rows and
// bytes removed. It stops at the first row that is not projected, is inside the
// grace window, or has unsettled protected dependencies.
func (r *Repository) retireAgentDeliveryPrefixTx(
	ctx context.Context,
	tx *sqlx.Tx,
	streamID string,
	retired, projected int64,
	cutoff time.Time,
	maxRows int,
) (int64, int64, int64, error) {
	sequence := retired
	var removed, bytes int64
	for sequence < projected && removed < int64(maxRows) {
		next := sequence + 1
		candidate, err := r.loadAgentDeliveryRetirementCandidateTx(ctx, tx, streamID, next)
		if err == sql.ErrNoRows {
			break // gap: the prefix must stay contiguous
		}
		if err != nil {
			return sequence, removed, bytes, err
		}
		if !candidate.eligible(cutoff) {
			break
		}
		if err := removeAgentDeliveryRetirementRowTx(ctx, tx, r.db.Rebind, streamID, next); err != nil {
			return sequence, removed, bytes, err
		}
		sequence = next
		removed++
		bytes += candidate.payloadBytes
	}
	return sequence, removed, bytes, nil
}

func removeAgentDeliveryRetirementRowTx(
	ctx context.Context, tx *sqlx.Tx, rebind func(string) string, streamID string, sequence int64,
) error {
	if _, err := tx.ExecContext(ctx, rebind(`
		DELETE FROM agent_delivery_inbox WHERE stream_id = ? AND sequence = ?`), streamID, sequence); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, rebind(`
		DELETE FROM agent_delivery_effects WHERE effect_key = ? AND state = ?`),
		genericAgentDeliveryEffectKey(streamID, sequence), models.DeliveryEffectCompleted)
	return err
}

type agentDeliveryRetirementCandidate struct {
	projected    bool
	projectedAt  time.Time
	payloadBytes int64
	settled      bool
}

// eligible reports whether a candidate may be removed: projected, past the
// grace window, and free of unsettled protected dependencies.
func (c agentDeliveryRetirementCandidate) eligible(cutoff time.Time) bool {
	return c.projected && !c.projectedAt.After(cutoff) && c.settled
}

func (r *Repository) loadAgentDeliveryRetirementCandidateTx(
	ctx context.Context, tx *sqlx.Tx, streamID string, sequence int64,
) (agentDeliveryRetirementCandidate, error) {
	var projectedAt sql.NullTime
	var sessionID, incarnationID, submissionID string
	var terminal int
	var payload []byte
	err := tx.QueryRowxContext(ctx, r.db.Rebind(`
		SELECT projected_at, session_id, incarnation_id, submission_id, terminal, payload
		FROM agent_delivery_inbox WHERE stream_id = ? AND sequence = ?`), streamID, sequence).Scan(
		&projectedAt, &sessionID, &incarnationID, &submissionID, &terminal, &payload)
	if err != nil {
		return agentDeliveryRetirementCandidate{}, err
	}
	candidate := agentDeliveryRetirementCandidate{
		projected:    projectedAt.Valid,
		payloadBytes: int64(len(payload)),
	}
	if projectedAt.Valid {
		candidate.projectedAt = projectedAt.Time
		candidate.settled = r.retirementDependencySettledTx(ctx, tx, sessionID, incarnationID, submissionID, terminal != 0)
	}
	return candidate, nil
}

// retirementDependencySettledTx reports whether a projected inbox row's
// protected dependencies have finished. Any open delivery recovery block on the
// row's session/incarnation protects all of that session's rows. Rows without a
// submission identity (session metadata, pre-submission transport events) are
// otherwise eligible; rows that reference a submission also require the
// submission to be settled. Any read failure is treated as unsettled (fail
// closed) so an unproven row is never removed.
func (r *Repository) retirementDependencySettledTx(
	ctx context.Context, tx *sqlx.Tx, sessionID, incarnationID, submissionID string, terminal bool,
) bool {
	var open int
	if err := tx.QueryRowxContext(ctx, r.db.Rebind(`
		SELECT COUNT(*) FROM session_recovery_blocks
		WHERE session_id = ? AND incarnation_id = ? AND state = ?`),
		sessionID, incarnationID, models.RecoveryBlockOpen).Scan(&open); err != nil {
		return false
	}
	if open != 0 {
		return false
	}
	if submissionID == "" {
		// A terminal without a submission identity cannot be proven settled.
		return !terminal
	}
	var state models.DeliverySubmissionState
	err := tx.QueryRowxContext(ctx, r.db.Rebind(`
		SELECT state FROM agent_delivery_submissions WHERE id = ?`), submissionID).Scan(&state)
	if err == sql.ErrNoRows {
		// The submission record was already removed with its session; the
		// surviving terminal evidence no longer has a protected dependency.
		return true
	}
	if err != nil {
		return false
	}
	return isSettledDeliverySubmissionState(state)
}

func isSettledDeliverySubmissionState(state models.DeliverySubmissionState) bool {
	switch state {
	case models.DeliverySubmissionCompleted, models.DeliverySubmissionFailed, models.DeliverySubmissionCancelled:
		return true
	default:
		return false
	}
}

// genericAgentDeliveryEffectKey is the per-event generic receipt key written by
// deliveryEffectForEvent; it mirrors the lifecycle key format.
func genericAgentDeliveryEffectKey(streamID string, sequence int64) string {
	return fmt.Sprintf("agent_delivery.event:%s:%d", streamID, sequence)
}
