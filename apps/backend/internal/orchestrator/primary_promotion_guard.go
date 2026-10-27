package orchestrator

import (
	"context"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/task/models"
)

// sessionAwaitsRecoveryAction reports whether a session cannot take prompts
// until an operator resolves it: it has an open session recovery block for its
// current incarnation and generation, or its dynamic route is action_required.
// Such a session must not inherit the primary role ahead of a healthy one and
// must not receive another session's queue. A block that cannot be read counts
// as open, so an unreadable session never wins over a readable healthy one.
func (s *Service) sessionAwaitsRecoveryAction(ctx context.Context, session *models.TaskSession) bool {
	if session == nil {
		return false
	}
	if session.RouteState == dynamicRouteStatusActionRequired {
		return true
	}
	block, err := s.GetOpenSessionRecoveryBlock(ctx, session.ID)
	if err != nil {
		s.logger.Debug("cannot read session recovery block for primary selection",
			zap.String("session_id", session.ID), zap.Error(err))
		return true
	}
	return block != nil
}

// resumeAttemptInFlight reports whether a live, unaccepted attempt still owns
// startup. Provider acceptance transfers that ownership before deferred finish.
func (s *Service) resumeAttemptInFlight(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	registry := s.resumeAttemptStore()
	registry.mu.Lock()
	defer registry.mu.Unlock()
	attempt := registry.attempts[sessionID]
	return attempt != nil && attempt.ctx != nil && attempt.ctx.Err() == nil && !attempt.accepted
}

// terminalQueueSourceRetired reports whether the session whose queue a
// hand-off would move is still terminal and no unaccepted resume attempt owns startup.
func (s *Service) terminalQueueSourceRetired(ctx context.Context, sessionID string) bool {
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil || session == nil {
		return false
	}
	return isTerminalSessionState(session.State) && !s.resumeAttemptInFlight(sessionID)
}

// terminalQueueReceiverAwaitsRecovery reports whether the session a hand-off
// would deliver to awaits a recovery action. A receiver that cannot be read
// counts as awaiting one, so the messages keep their session target.
func (s *Service) terminalQueueReceiverAwaitsRecovery(ctx context.Context, sessionID string) bool {
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil || session == nil {
		return true
	}
	return s.sessionAwaitsRecoveryAction(ctx, session)
}
