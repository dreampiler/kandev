package orchestrator

import (
	"context"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
	"go.uber.org/zap"
)

const acceptedQueueReadFailed = "read_failed"

func (s *Service) evaluateAcceptedQueuedSuccessor(
	ctx context.Context, taskID, sessionID, acceptedQueueID string, reservation *queuedDispatchReservation,
) queueDrainOutcome {
	var identity *messagequeue.QueueSessionIdentity
	if reservation != nil && reservation.identity.SessionIncarnationID != "" {
		if reservation.identity.TaskID != taskID || reservation.identity.SessionID != sessionID {
			return queueDrainSkipped
		}
		captured := reservation.identity
		identity = &captured
	}
	outcome := s.drainQueuedMessageForPromptableSessionWithTaskAdmissionAndIdentity(ctx, taskID, sessionID, identity)
	successorSessionID := sessionID
	disposition := s.acceptedSuccessorDisposition(ctx, sessionID, outcome)
	if outcome == queueDrainSkipped {
		outcome, successorSessionID, disposition = s.recoverAcceptedTerminalSuccessor(ctx, taskID, sessionID, identity)
	}
	s.logger.Info("accepted queued prompt successor evaluated",
		zap.String("task_id", taskID), zap.String("session_id", sessionID),
		zap.String("successor_session_id", successorSessionID),
		zap.String("accepted_queue_id", acceptedQueueID), zap.String("disposition", disposition))
	return outcome
}

func (s *Service) acceptedSuccessorDisposition(ctx context.Context, sessionID string, outcome queueDrainOutcome) string {
	switch outcome {
	case queueDrainDispatched:
		return "dispatched"
	case queueDrainTaskAdmissionReadFailed:
		return acceptedQueueReadFailed
	case queueDrainPaused:
		if !s.messageQueue.GetStatus(ctx, sessionID).AutoRun {
			return "auto_run_off"
		}
	}
	return "not_promptable"
}

func (s *Service) recoverAcceptedTerminalSuccessor(
	ctx context.Context, taskID, sessionID string, identity *messagequeue.QueueSessionIdentity,
) (queueDrainOutcome, string, string) {
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		return queueDrainSkipped, sessionID, acceptedQueueReadFailed
	}
	if session == nil || (session.State != models.TaskSessionStateFailed && session.State != models.TaskSessionStateCancelled) {
		return queueDrainSkipped, sessionID, "not_promptable"
	}
	primaryID, transferred, err := s.transferAcceptedQueueToPrimary(ctx, taskID, session, identity)
	if err != nil {
		s.logger.Warn("failed to transfer accepted queued successor to primary",
			zap.String("task_id", taskID), zap.String("session_id", sessionID), zap.Error(err))
		return queueDrainSkipped, sessionID, acceptedQueueReadFailed
	}
	if !transferred {
		return queueDrainSkipped, sessionID, "recovery_required"
	}
	outcome := s.drainQueuedMessageForPromptableSessionWithTaskAdmissionAndIdentity(ctx, taskID, primaryID, nil)
	return outcome, primaryID, s.acceptedSuccessorDisposition(ctx, primaryID, outcome)
}

func (s *Service) transferAcceptedQueueToPrimary(
	ctx context.Context, taskID string, source *models.TaskSession, identity *messagequeue.QueueSessionIdentity,
) (string, bool, error) {
	if s.messageQueue == nil || s.agentManager == nil || source == nil || source.TaskID != taskID {
		return "", false, nil
	}
	if identity != nil && source.QueueIncarnationID != identity.SessionIncarnationID {
		return "", false, nil
	}
	if s.messageQueue.GetStatus(ctx, source.ID).Count == 0 {
		return "", false, nil
	}
	primary, err := s.eligiblePrimaryForAcceptedQueue(ctx, taskID, source.ID)
	if err != nil {
		return "", false, err
	}
	if primary == nil {
		return "", false, nil
	}
	if err := s.transferQueuedSessionState(ctx, taskID, source.ID, primary.ID); err != nil {
		return "", false, err
	}
	return primary.ID, true, nil
}

func (s *Service) eligiblePrimaryForAcceptedQueue(ctx context.Context, taskID, sourceSessionID string) (*models.TaskSession, error) {
	sessions, err := s.repo.ListTaskSessions(ctx, taskID)
	if err != nil {
		return nil, err
	}
	primary := findPrimarySession(sessions)
	if primary == nil || primary.ID == sourceSessionID || primary.TaskID != taskID ||
		primary.State != models.TaskSessionStateWaitingForInput ||
		!s.agentManager.IsAgentRunningForSession(ctx, primary.ID) {
		return nil, nil
	}
	if err := s.checkSessionPromptable(taskID, primary.ID, primary.State); err != nil {
		return nil, nil
	}
	return primary, nil
}
