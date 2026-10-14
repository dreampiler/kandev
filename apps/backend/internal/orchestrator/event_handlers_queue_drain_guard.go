package orchestrator

import (
	"context"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
)

// drainQueuedMessageForPromptableSessionUncontended is the same guarded
// take-and-dispatch for a caller that may already hold sessionID's guard: it
// probes the guard instead of waiting for it. The queue trigger that runs right
// after a session settles into WAITING_FOR_INPUT needs that, because the
// handler which drove the transition frequently still holds the guard across its
// own turn settlement, and a blocking acquisition there would wait on itself.
// A contended guard already belongs to a take-decision that delivers,
// supersedes, or retries on its own.
func (s *Service) drainQueuedMessageForPromptableSessionUncontended(
	ctx context.Context,
	taskID, sessionID string,
) queueDrainOutcome {
	lock, release := s.acquireCancelInFlightGuard(sessionID)
	defer release()
	if !lock.TryLock() {
		return queueDrainSkipped
	}
	defer lock.Unlock()
	return s.drainQueuedMessageForPromptableSessionUnderGuard(ctx, taskID, sessionID, nil)
}

// drainQueuedMessageForPromptableSessionUnderGuard holds sessionID's guard for
// every take decision it makes: cancel ownership, a reloaded promptable state,
// the captured queue incarnation, a live clarification, and workflow admission
// are all re-checked here rather than trusted from the caller's earlier check.
func (s *Service) drainQueuedMessageForPromptableSessionUnderGuard(
	ctx context.Context,
	taskID, sessionID string,
	identity *messagequeue.QueueSessionIdentity,
) queueDrainOutcome {
	if s.isCancelInFlight(sessionID) {
		return queueDrainSkipped
	}
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		s.logger.Warn("failed to reload session before admission-gated drain",
			zap.String("session_id", sessionID), zap.Error(err))
		return queueDrainSkipped
	}
	if session == nil {
		return queueDrainSkipped
	}
	if session.TaskID != taskID {
		return queueDrainSkipped
	}
	if identity != nil && session.QueueIncarnationID != identity.SessionIncarnationID {
		return queueDrainSkipped
	}
	if err := s.checkSessionRecoveryBlock(ctx, sessionID); err != nil {
		s.logger.Info("skipping admission-gated queue drain while session recovery is required",
			zap.String("session_id", sessionID), zap.Error(err))
		s.autoRecoverBlockedQueue(ctx, taskID, sessionID, err)
		return queueDrainSkipped
	}
	if err := s.checkSessionPromptable(session.TaskID, sessionID, session.State); err != nil {
		return queueDrainSkipped
	}
	// QueueUserPrompt can race a live clarification between its enqueue-side
	// checks and this guarded drain. Re-read the clarification ownership while
	// the guard is held so a parent question or human clarification still owns
	// the turn when the queue head is reserved. Detached bundles are allowed by
	// sessionHasLiveClarification and can therefore make progress here.
	if s.sessionHasLiveClarification(ctx, sessionID) {
		return queueDrainSkipped
	}
	return s.drainQueuedMessageForPromptableSessionLockedWithTaskAdmissionAndIdentity(
		ctx, taskID, sessionID, identity, workflowEffectFromContext(ctx),
	)
}
