package orchestrator

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// SetPrimarySessionTransferringQueue promotes a session to primary and moves
// any queue still bound to the previous primary onto it. Without this, a queue
// left behind on a demoted primary is stranded when that session later ends.
func (s *Service) SetPrimarySessionTransferringQueue(ctx context.Context, sessionID string) error {
	if err := s.authorizeSession(ctx, sessionID); err != nil {
		return err
	}
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to load session for primary promotion: %w", err)
	}
	if session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	previousPrimaryID, err := s.primarySessionID(ctx, session.TaskID)
	if err != nil {
		s.logger.Warn("failed to resolve previous primary before promotion; queue will not transfer",
			zap.String("task_id", session.TaskID),
			zap.String("session_id", sessionID),
			zap.Error(err))
		previousPrimaryID = ""
	}
	if err := s.SetPrimarySession(ctx, sessionID); err != nil {
		return err
	}
	if previousPrimaryID == "" || previousPrimaryID == sessionID {
		s.drainQueuedAfterWaitingTransition(ctx, session.TaskID, sessionID)
		return nil
	}
	if err := s.transferQueuedSessionState(ctx, session.TaskID, previousPrimaryID, sessionID); err != nil {
		// Fail-open: the promotion stands. A queue left behind is recovered
		// when its dispatch observes the stranded session
		// (see recoverStrandedQueueToPrimary).
		s.logger.Warn("failed to transfer queue to new primary session",
			zap.String("task_id", session.TaskID),
			zap.String("old_primary", previousPrimaryID),
			zap.String("new_primary", sessionID),
			zap.Error(err))
		return nil
	}
	// The transferred queue belongs to the new primary now, and nothing else
	// will deliver it: the demoted primary is no longer a trigger for this task.
	s.drainQueuedAfterWaitingTransition(ctx, session.TaskID, sessionID)
	return nil
}

// primarySessionID returns the current primary session for a task, or "" when
// none is set.
func (s *Service) primarySessionID(ctx context.Context, taskID string) (string, error) {
	sessions, err := s.repo.ListTaskSessions(ctx, taskID)
	if err != nil {
		return "", err
	}
	for _, sess := range sessions {
		if sess != nil && sess.IsPrimary {
			return sess.ID, nil
		}
	}
	return "", nil
}
