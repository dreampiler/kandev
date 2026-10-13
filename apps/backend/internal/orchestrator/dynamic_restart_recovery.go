package orchestrator

import (
	"context"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
)

// Route states that a resume re-claims before the runtime is ready.
const (
	dynamicRouteStatusStarting = "starting"
	dynamicRouteStatusRetrying = "retrying"
)

// activateResumedDynamicRoute completes the durable "starting" -> "active"
// route transition for a resumed dynamic session whose runtime is ready. It
// returns the transition error so the resume path does not report success while
// the claimed route is still "starting".
func (s *Service) activateResumedDynamicRoute(ctx context.Context, session *models.TaskSession) error {
	if session == nil || session.RouteGeneration <= 0 || session.ExecutionProfileID == "" {
		return nil
	}
	if session.RouteState != dynamicRouteStatusStarting && session.RouteState != dynamicRouteStatusRetrying {
		return nil
	}
	return s.markDynamicRouteActive(context.WithoutCancel(ctx), session.ID, session.RouteGeneration)
}

// explicitRecoveryOwnsStrandedStartingRoute reports whether an explicit
// recovery action may resume a dynamic session whose route claim is still
// "starting" with no live runtime behind it. A live launch owner keeps the
// dynamic_route_pending guard; passive resumes are unchanged.
func (s *Service) explicitRecoveryOwnsStrandedStartingRoute(
	ctx context.Context,
	session *models.TaskSession,
	options executor.ResumeOptions,
) bool {
	if session == nil || options.RecoveryAction == "" ||
		session.RouteState != dynamicRouteStatusStarting ||
		session.RouteGeneration <= 0 || session.ExecutionProfileID == "" {
		return false
	}
	return s.agentManager == nil || !s.agentManager.IsAgentRunningForSession(ctx, session.ID)
}
