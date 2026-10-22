package orchestrator

import (
	"context"
	"errors"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/task/models"
	"go.uber.org/zap"
)

// Explicit context recovery may claim the already-selected failed route without
// invoking the route-action launcher or replaying its predecessor prompt.
func (s *Service) claimContextContinuationRoute(ctx context.Context, session *models.TaskSession) (bool, error) {
	if session.RouteState != dynamicRouteStatusActionRequired || session.RouteGeneration <= 0 ||
		session.ExecutionProfileID == "" || s.profileExecutionResolver == nil {
		return false, nil
	}
	releaseRoute := s.acquireSessionRouteOperationLock(session.ID)
	defer releaseRoute()
	releaseLifecycle := s.acquireSessionLifecycleLock(session.ID)
	defer releaseLifecycle()
	current, err := s.loadContextContinuationRoute(ctx, session)
	if err != nil {
		return false, err
	}
	resolved, err := s.profileExecutionResolver.ResolveRouteAction(ctx, current.ID, current.AgentProfileID,
		current.ExecutionProfileID, current.RouteGeneration, "retry")
	if err != nil {
		return false, err
	}
	if resolved.Generation != current.RouteGeneration || resolved.ExecutionProfileID != current.ExecutionProfileID ||
		resolved.Decision.Status != dynamicRouteStatusRetrying {
		return false, dynamicruntime.ErrStaleGeneration
	}
	selected := *current
	selected.RouteState, selected.RouteReason = resolved.Decision.Status, resolved.Decision.Reason
	changed, err := s.PersistDynamicRouteSelection(ctx, current, &selected)
	if err != nil || !changed {
		if err == nil {
			err = dynamicruntime.ErrStaleGeneration
		}
		return false, errors.Join(err, s.profileExecutionResolver.MarkRouteRecoveryActionRequired(context.WithoutCancel(ctx), current.ID, current.RouteGeneration))
	}
	*session = selected
	return true, nil
}

func (s *Service) loadContextContinuationRoute(ctx context.Context, expected *models.TaskSession) (*models.TaskSession, error) {
	current, err := s.repo.GetTaskSession(ctx, expected.ID)
	if err != nil {
		return nil, err
	}
	if !sameFailedContextRoute(current, expected) {
		return nil, dynamicruntime.ErrStaleGeneration
	}
	if s.agentManager != nil && s.agentManager.IsAgentRunningForSession(ctx, current.ID) {
		return nil, &sessionOpenRecoveryBlockedError{reason: autoResumeBlockedDynamicRoute}
	}
	loader, ok := s.repo.(dynamicRouteStateLoader)
	if !ok {
		return nil, errors.New("context recovery route state is unavailable")
	}
	state, err := loader.LoadRouteState(ctx, current.ID)
	if err != nil {
		return nil, err
	}
	if state == nil || state.Status != dynamicRouteStatusActionRequired || state.Generation != current.RouteGeneration ||
		state.ExecutionProfileID != current.ExecutionProfileID || state.LogicalProfileID != current.AgentProfileID {
		return nil, dynamicruntime.ErrStaleGeneration
	}
	return current, nil
}

func sameFailedContextRoute(current, expected *models.TaskSession) bool {
	return current != nil && current.RouteState == dynamicRouteStatusActionRequired &&
		current.RouteGeneration == expected.RouteGeneration && current.ExecutionProfileID == expected.ExecutionProfileID &&
		current.TaskID == expected.TaskID
}

func (s *Service) releaseAbandonedContextContinuationRoute(ctx context.Context, sessionID string, generation int64, succeeded *bool) {
	if *succeeded {
		return
	}
	if err := s.profileExecutionResolver.MarkRouteRecoveryActionRequired(context.WithoutCancel(ctx), sessionID, generation); err != nil {
		s.logger.Warn("failed to release abandoned context recovery route", zap.String("session_id", sessionID), zap.Error(err))
	}
}
