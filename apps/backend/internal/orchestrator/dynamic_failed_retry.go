package orchestrator

import (
	"context"
	"errors"
	"fmt"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/task/models"
)

// A failed launch can leave a historical starting claim without an owner.
// Reconcile it only for an explicit retry, under lifecycle ownership and the
// durable generation CAS. No timer, live launch or active turn is reclaimed.
func (s *Service) prepareFailedDynamicRouteRetry(ctx context.Context, request RouteActionRequest) error {
	if request.Action != RouteActionRetry || s.profileExecutionResolver == nil || !s.profileExecutionResolver.Enabled() {
		return nil
	}
	release := s.acquireSessionLifecycleLock(request.SessionID)
	defer release()
	session, err := s.repo.GetTaskSession(ctx, request.SessionID)
	if err != nil {
		return err
	}
	if session == nil || session.State != models.TaskSessionStateFailed ||
		session.RouteState != dynamicRouteStatusStarting || session.RouteGeneration <= 0 ||
		session.RouteGeneration != request.ExpectedGeneration {
		return nil
	}
	if s.agentManager == nil || s.agentManager.IsAgentRunningForSession(ctx, session.ID) {
		return dynamicruntime.ErrRecoveryPending
	}
	if _, err := s.repo.GetExecutorRunningBySessionID(ctx, session.ID); !errors.Is(err, models.ErrExecutorRunningNotFound) {
		if err != nil {
			return fmt.Errorf("check failed route runtime owner: %w", err)
		}
		return dynamicruntime.ErrRecoveryPending
	}
	decision, err := s.profileExecutionResolver.MarkRouteActionRequired(ctx, session.ID, session.RouteGeneration, "failed_start_route_retry")
	if err != nil {
		return err
	}
	s.mirrorDynamicRouteProjection(ctx, session, session.RouteGeneration, decision.Status, decision.Reason)
	return nil
}
