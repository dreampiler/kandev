package orchestrator

import (
	"context"

	"github.com/kandev/kandev/internal/task/models"
	"go.uber.org/zap"
)

// sessionAwaitsRecoveryAction reports whether a session needs an operator's
// recovery decision before it can take prompts. An unreadable block remains
// unavailable to automatic promotion, just like an open block.
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

// resumeAttemptInFlight reports whether a live resume attempt still owns the
// session's startup. Its FAILED rollback must not hand away the primary role.
func (s *Service) resumeAttemptInFlight(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	attempt, ok := s.resumeAttemptStore().current(sessionID)
	return ok && attempt != nil && attempt.ctx != nil && attempt.ctx.Err() == nil
}
