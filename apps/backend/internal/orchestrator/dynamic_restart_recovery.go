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
// route transition once a resumed dynamic session's runtime is ready. A resume
// re-claims the persisted route as "starting" but settles the session to
// WAITING_FOR_INPUT. Left "starting", the route makes every later explicit
// recovery report dynamic_route_pending.
func (s *Service) activateResumedDynamicRoute(ctx context.Context, session *models.TaskSession) {
	if session == nil || session.RouteGeneration <= 0 || session.ExecutionProfileID == "" {
		return
	}
	if session.RouteState != dynamicRouteStatusStarting && session.RouteState != dynamicRouteStatusRetrying {
		return
	}
	s.markDynamicRouteActive(context.WithoutCancel(ctx), session.ID, session.RouteGeneration)
}

// explicitRecoveryOwnsStrandedStartingRoute lets an explicit recovery action
// resume a dynamic session whose route claim was left "starting" with no live
// runtime behind it, for example by a restart. The recovery re-claims the same
// persisted candidate. A live launch owner keeps the dynamic_route_pending
// guard, and passive resumes are unchanged.
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
