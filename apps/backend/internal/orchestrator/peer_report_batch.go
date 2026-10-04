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
	if err != nil {
		return false, true, err
	}
	if !autoRun || len(batch) == 0 {
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
	if !s.peerReportBatchStillDispatchable(ctx, identity, composed, batch) {
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
