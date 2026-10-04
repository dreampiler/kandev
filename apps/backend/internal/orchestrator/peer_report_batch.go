package orchestrator

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
)

// reserveAndDispatchQueueHead reserves the FIFO head for identity and dispatches
// it. A consecutive run of ordinary peer reports behind the head is delivered in
// the same turn, so a burst of child reports does not consume one queue slot and
// one turn each. Returns whether a dispatch happened and whether Auto-run is
// still enabled; the second value is false only when Auto-run is OFF.
func (s *Service) reserveAndDispatchQueueHead(
	ctx context.Context,
	identity messagequeue.QueueSessionIdentity,
) (bool, bool, error) {
	batch, autoRun, err := s.messageQueue.ReservePeerReportBatchWithAutoRunForSession(ctx, identity)
	if err != nil || !autoRun {
		// A reserve that stops part-way still owns the rows it took. They are
		// already out of the queue, so they have to go back rather than be
		// dropped with the error.
		if len(batch) > 0 {
			s.requeuePeerReportBatch(ctx, identity, batch)
		}
		return false, autoRun, err
	}
	if len(batch) == 0 {
		return false, autoRun, nil
	}
	if len(batch) == 1 {
		return s.dispatchTakenQueuedMessageForSession(ctx, identity, batch[0], true), true, nil
	}
	return s.dispatchPeerReportBatchForSession(ctx, identity, batch), true, nil
}

// dispatchPeerReportBatchForSession delivers a reserved peer-report batch as one
// prompt. The batch is settled by the ordinary dispatch path under the leading
// entry's identity; a pre-dispatch failure returns every reserved entry to the
// head in its original order.
func (s *Service) dispatchPeerReportBatchForSession(
	ctx context.Context,
	identity messagequeue.QueueSessionIdentity,
	batch []*messagequeue.QueuedMessage,
) bool {
	composed := messagequeue.ComposePeerReportBatch(batch)
	if !s.messageQueue.PeerReportBatchClaimRewriteSupported() {
		// Folding needs the leading claim to be rewritten to the composed
		// prompt, so a repository that cannot rewrite it delivers the batch one
		// row at a time instead of risking a restart that restores only the
		// leading report.
		s.requeuePeerReportBatch(ctx, identity, batch[1:])
		return s.dispatchTakenQueuedMessageForSession(ctx, identity, batch[0], true)
	}
	if !s.peerReportBatchStillDispatchable(ctx, identity, composed, batch) {
		s.requeuePeerReportBatch(ctx, identity, batch)
		return false
	}
	if !s.settlePeerReportBatchClaims(ctx, identity, composed, batch) {
		s.requeuePeerReportBatch(ctx, identity, batch)
		return false
	}
	s.publishQueueStatusEventForIdentity(ctx, identity)
	reservation := s.markQueuedDispatchInFlightWithIdentityLocked(identity, composed.ID, composed)
	if s.agentManager != nil && s.agentManager.IsPassthroughSession(ctx, identity.SessionID) {
		go s.executeQueuedPassthroughMessageWithReservation(identity, composed, reservation)
		return true
	}
	go s.executeQueuedMessageWithReservation(identity.SessionID, composed, reservation)
	return true
}

// settlePeerReportBatchClaims gives the batch exactly one settlement owner before
// its worker starts.
//
// An ordinary reserve deletes its row and keeps at-least-once recovery in a
// dispatch claim, and startup restores a claim that was never accepted. Two
// writes therefore have to happen, in this order:
//
//  1. The leading row's claim is rewritten to the composed prompt. The claim was
//     written at reserve time, when the leading row still held only its own
//     report, so a restart would otherwise restore the batch without the folded
//     reports.
//  2. Every folded row's claim is accepted. A folded row's report now travels
//     inside the leading row, so the leading row's claim alone must decide
//     delivery; leaving a folded claim unsettled would restore that report a
//     second time.
//
// If either write fails the batch returns to the queue with every claim still
// unsettled, so nothing is lost. Accepting the folded claims is deliberately
// earlier than the leading row's acceptance: a crash in that window leaves the
// leading claim unaccepted, so startup restores one row holding the whole batch.
func (s *Service) settlePeerReportBatchClaims(
	ctx context.Context,
	identity messagequeue.QueueSessionIdentity,
	composed *messagequeue.QueuedMessage,
	batch []*messagequeue.QueuedMessage,
) bool {
	settleCtx := context.WithoutCancel(ctx)
	if err := s.messageQueue.RewritePendingQueueDispatchMessage(settleCtx, composed); err != nil {
		s.logger.Error("failed to record the composed peer report batch as the leading dispatch claim",
			zap.String("session_id", identity.SessionID),
			zap.String("task_id", identity.TaskID),
			zap.String("queue_id", composed.ID),
			zap.Int("batch_size", len(batch)),
			zap.Error(err))
		return false
	}
	for _, entry := range batch[1:] {
		if err := s.messageQueue.MarkPendingQueueDispatchAccepted(settleCtx, entry); err != nil {
			s.logger.Error("failed to settle folded peer report dispatch claim; a restart may re-deliver this report",
				zap.String("session_id", identity.SessionID),
				zap.String("task_id", identity.TaskID),
				zap.String("queue_id", entry.ID),
				zap.Error(err))
			return false
		}
	}
	return true
}

// peerReportBatchStillDispatchable reports whether the reserved batch may be
// handed to an agent now: the session incarnation must still match and the
// composed prompt must carry dispatchable input.
func (s *Service) peerReportBatchStillDispatchable(
	ctx context.Context,
	identity messagequeue.QueueSessionIdentity,
	composed *messagequeue.QueuedMessage,
	batch []*messagequeue.QueuedMessage,
) bool {
	if identity.SessionIncarnationID != "" {
		current, err := s.messageQueue.ResolveSessionIdentity(ctx, identity.TaskID, identity.SessionID)
		if err != nil || current != identity {
			s.logger.Warn("peer report batch dropped for replaced session",
				zap.String("session_id", identity.SessionID),
				zap.String("queue_id", composed.ID),
				zap.Int("batch_size", len(batch)),
				zap.Error(err))
			return false
		}
	}
	hasInput, inputErr := s.queuedMessageHasDispatchInput(ctx, composed)
	if inputErr != nil {
		s.logger.Warn("failed to inspect peer report batch input; returning reservation",
			zap.String("session_id", identity.SessionID),
			zap.String("queue_id", composed.ID),
			zap.Error(inputErr))
		return false
	}
	if !hasInput {
		s.logger.Warn("skipping empty peer report batch after transition",
			zap.String("session_id", identity.SessionID),
			zap.String("queue_id", composed.ID))
	}
	return hasInput
}

// requeuePeerReportBatch returns reserved entries to the head in their original
// order. Requeue-at-head inserts ahead of the remaining queue, so the batch is
// replayed from its last entry.
func (s *Service) requeuePeerReportBatch(
	ctx context.Context,
	identity messagequeue.QueueSessionIdentity,
	batch []*messagequeue.QueuedMessage,
) {
	for index := len(batch) - 1; index >= 0; index-- {
		entry := batch[index]
		if identity.SessionIncarnationID == "" {
			s.requeueMessage(ctx, entry, entry.QueuedBy)
			continue
		}
		if err := s.requeueMessageForSession(ctx, identity, entry, entry.QueuedBy); err != nil &&
			errors.Is(err, messagequeue.ErrSessionIdentityMismatch) {
			s.logger.Warn("dropping peer report batch entry for replaced session",
				zap.String("session_id", identity.SessionID),
				zap.String("queue_id", entry.ID),
				zap.Error(err))
		}
	}
	s.publishQueueStatusEventForIdentity(ctx, identity)
}
