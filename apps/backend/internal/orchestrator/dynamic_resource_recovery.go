package orchestrator

import (
	"context"
	"errors"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"go.uber.org/zap"
)

// DynamicRecoverableResourceWait reports whether err is a dynamic-route
// selection failure whose route state is durably waiting for suspended
// resources, returning the earliest retry deadline the engine recorded.
// Launch entry points use it to surface that wait at the observer's level
// instead of a terminal ERROR, because time alone makes a candidate usable
// again and the scheduled recovery reselects without operator action.
func DynamicRecoverableResourceWait(err error) (time.Time, bool) {
	if err == nil {
		return time.Time{}, false
	}
	var noCandidate *dynamicruntime.NoEligibleCandidateError
	if !errors.As(err, &noCandidate) || !noCandidate.ResourceWait {
		return time.Time{}, false
	}
	return noCandidate.RetryAt, true
}

// recordDynamicResourceOutput clears the suspension history of the profile
// whose current attempt produced its first real output. Each attempt records
// at most once, so streaming chunks do not repeat the write. The success
// belongs to the profile the producing execution runs, which the stream event
// carries; the route profile captured when the attempt began is the fallback
// for an event without it. Neither needs a repository read in the raw stream
// callback.
func (s *Service) recordDynamicResourceOutput(
	ctx context.Context,
	sessionID, executionID, eventExecutionProfileID string,
	promptGeneration uint64,
) {
	if s.profileExecutionResolver == nil || sessionID == "" {
		return
	}
	attempt, ok := s.promptAttemptForSession(sessionID)
	if !ok {
		return
	}
	attempt.mu.Lock()
	executionProfileID := attempt.executionProfileID
	claim := attempt.dynamic && executionProfileID != "" && !attempt.resourceSuccessClaimed &&
		attempt.promptIdentityMatchesForClearLocked(executionID, promptGeneration)
	if claim {
		attempt.resourceSuccessClaimed = true
	}
	attempt.mu.Unlock()
	if !claim {
		return
	}
	// A route that claimed a successor whose launch is deferred keeps the
	// predecessor execution serving prompts, so the captured route profile
	// can name a candidate that never ran.
	if eventExecutionProfileID != "" {
		executionProfileID = eventExecutionProfileID
	}
	s.profileExecutionResolver.RecordResourceSuccess(ctx, executionProfileID)
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
