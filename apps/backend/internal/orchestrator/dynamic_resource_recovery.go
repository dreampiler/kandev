package orchestrator

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/task/models"
)

// recordDynamicResourceSuccess clears the suspension history of the candidate
// whose current attempt produced real output. It runs where the deferred
// streak reset has already loaded the session, so raw stream callbacks stay
// free of repository reads. Each attempt records at most once.
func (s *Service) recordDynamicResourceSuccess(
	ctx context.Context,
	session *models.TaskSession,
	snapshot pendingDynamicStreakResetSnapshot,
) {
	attempt := snapshot.attempt
	if s.profileExecutionResolver == nil || attempt == nil || session == nil ||
		session.ExecutionProfileID == "" || session.RouteGeneration <= 0 {
		return
	}
	attempt.mu.Lock()
	claim := attempt.dynamic && !attempt.resourceSuccessClaimed &&
		attempt.promptIdentityMatchesForClearLocked(snapshot.event.AgentExecutionID, snapshot.event.PromptGeneration) &&
		attempt.outputObservedLocked(snapshot.event)
	if claim {
		attempt.resourceSuccessClaimed = true
	}
	attempt.mu.Unlock()
	if claim {
		s.profileExecutionResolver.RecordResourceSuccess(ctx, session.ExecutionProfileID)
	}
}

// observeDynamicResourceWait schedules a fresh selection for a route that is
// waiting because every candidate is suspended.
func (s *Service) observeDynamicResourceWait(sessionID string, generation int64, deadline time.Time) {
	s.logger.Info("dynamic route waits for suspended resources",
		zap.String("session_id", sessionID),
		zap.Int64("generation", generation),
		zap.Time("retry_at", deadline))
	s.scheduleDynamicRecoveryAt(sessionID, generation, deadline)
}

// runDynamicResourceWaitRecovery retries a resource wait through the same
// route action an operator's retry uses, so selection, attribution and the
// successor launch follow one path. A selection that still finds every
// candidate suspended records a new wait, which schedules the next attempt.
func (s *Service) runDynamicResourceWaitRecovery(ctx context.Context, sessionID string, generation int64) {
	if s.routeActionHandler == nil {
		return
	}
	releaseRouteAction := s.acquireRouteActionOperationLock(sessionID)
	defer releaseRouteAction()
	if err := s.rejectRouteActionDuringActiveTurn(ctx, sessionID); err != nil {
		s.logger.Info("dynamic resource wait recovery skipped",
			zap.String("session_id", sessionID), zap.Error(err))
		return
	}
	result, err := s.routeActionHandler(ctx, RouteActionRequest{
		SessionID: sessionID, Action: RouteActionRetry, ExpectedGeneration: generation,
	})
	if err != nil {
		s.logger.Warn("dynamic resource wait recovery failed",
			zap.String("session_id", sessionID), zap.Int64("generation", generation), zap.Error(err))
		return
	}
	if result == nil {
		return
	}
	s.logger.Info("dynamic resource wait recovered",
		zap.String("session_id", sessionID),
		zap.String("execution_profile_id", result.ExecutionProfileID),
		zap.Int64("generation", result.RouteGeneration),
		zap.String("state", result.State))
}
