package orchestrator

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
)

// consumePendingMoveAtCurrentStep records a deferred move whose target equals
// the task's current step. There is no transition to run, but the row and its
// hand-off prompt must still be consumed and the session drained exactly as a
// landed move would.
func (s *Service) consumePendingMoveAtCurrentStep(
	ctx context.Context,
	taskID, sessionID string,
	move *messagequeue.PendingMove,
	record messagequeue.PendingMoveRecord,
	currentStepID string,
) {
	var applyErr error
	if s.messageQueue.SupportsAtomicDeferredMoveTransition() {
		applyErr = s.workflowStore.MarkDeferredMoveApplied(ctx, taskID, move.MoveID, record)
	} else {
		applyErr = s.workflowStore.markDeferredMoveAppliedUnfenced(ctx, taskID, move.MoveID)
	}
	if errors.Is(applyErr, errDeferredMoveAlreadyApplied) {
		s.logger.Info("dropping already-applied pending move at current target",
			zap.String("task_id", taskID), zap.String("move_id", move.MoveID))
	} else if applyErr != nil {
		s.logger.Error("failed to record pending move at current target",
			zap.String("task_id", taskID), zap.String("move_id", move.MoveID), zap.Error(applyErr))
		return
	}
	s.logger.Info("pending move target equals current step; skipping transition",
		zap.String("task_id", taskID),
		zap.String("step_id", currentStepID))
	identity := messagequeue.QueueSessionIdentity{
		TaskID: taskID, SessionID: sessionID, SessionIncarnationID: move.SessionIncarnationID,
	}
	s.removePendingMoveHandoffPromptForSession(ctx, identity, move.MoveID)
	_, _ = s.drainQueuedMessageForPromptableSessionForIdentity(ctx, identity)
	if !s.messageQueue.SupportsAtomicDeferredMoveTransition() {
		s.consumeUnfencedPendingMove(ctx, taskID, sessionID, record)
	}
}

// pendingMoveSupersededByLaterTransition reports whether the task has taken a
// workflow-step transition since the move was armed, which makes the recorded
// source stale even when the card is back on that step. It returns false when
// the ledger is unreadable so an uncertain guard preserves the move rather than
// dropping it (the move then applies as it did before this guard).
func (s *Service) pendingMoveSupersededByLaterTransition(
	ctx context.Context,
	taskID string,
	move *messagequeue.PendingMove,
) bool {
	reader, ok := s.repo.(workflowStepTransitionReader)
	if !ok {
		return false
	}
	latest, err := reader.GetLatestTaskStepTransitionID(ctx, taskID)
	if err != nil {
		s.logger.Warn("failed to read latest step transition for pending move; applying as before",
			zap.String("task_id", taskID), zap.String("move_id", move.MoveID), zap.Error(err))
		return false
	}
	return latest != move.FromTransitionID
}

// dropSupersededPendingMove discards a deferred move whose recorded source step
// no longer matches the task: a later move relocated the card, so replaying this
// one would overwrite it. The row and its hand-off prompt are removed with the
// same compare-and-delete used by the stale-move reaper, so a move that a
// concurrent take/replace swapped out is left to its new owner.
func (s *Service) dropSupersededPendingMove(
	ctx context.Context,
	taskID, sessionID string,
	move *messagequeue.PendingMove,
	record messagequeue.PendingMoveRecord,
) {
	moveID := move.MoveID
	if moveID == "" {
		moveID = normalizedPendingMoveID(sessionID, move)
	}
	handoffEntryID, listed := s.pendingMoveHandoffPromptID(ctx, sessionID, taskID, moveID)
	if !listed {
		s.logger.Warn("superseded pending move preserved after prompt cleanup failure",
			pendingMoveLogFields(sessionID, move, moveID)...)
		return
	}
	removed, err := s.messageQueue.DeletePendingMoveIfMatch(ctx, record, handoffEntryID)
	if err != nil {
		s.logger.Warn("failed to discard superseded pending move",
			append(pendingMoveLogFields(sessionID, move, moveID), zap.Error(err))...)
		return
	}
	if !removed {
		return
	}
	s.logger.Info("dropping superseded pending move",
		pendingMoveLogFields(sessionID, move, moveID)...)
	s.pendingMoveHandoffPromptRemoved(ctx, sessionID, taskID, moveID, handoffEntryID)
}
