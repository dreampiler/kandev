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

// completeReadyStrandedStartingRoute completes the durable "starting" ->
// "active" transition for a claim whose generation already has a live,
// prompt-ready runtime. It is the reconciling counterpart of the process-start
// callback: it retries the activation write for the same generation without
// stopping or replacing the runtime, and reports whether the session
// projection now reads "active".
func (s *Service) completeReadyStrandedStartingRoute(ctx context.Context, session *models.TaskSession) bool {
	if s.profileExecutionResolver == nil || !s.profileExecutionResolver.Enabled() {
		return false
	}
	if !isStartingRouteClaim(session) {
		return false
	}
	if !s.hasLivePromptReadyAgent(ctx, session.ID) {
		return false
	}
	if s.markDynamicRouteActive(ctx, session.ID, session.RouteGeneration) != nil {
		return false
	}
	loader, ok := s.repo.(dynamicRouteStateLoader)
	if !ok {
		return false
	}
	state, err := loader.LoadRouteState(ctx, session.ID)
	if err != nil || state == nil || state.Generation != session.RouteGeneration ||
		state.Status != dynamicRouteStatusActive {
		return false
	}
	session.RouteState = dynamicRouteStatusActive
	return true
}

// isStartingRouteClaim reports whether a session carries a claimed dynamic
// route that has not reached "active": a positive generation and execution
// profile behind a "starting" route state.
func isStartingRouteClaim(session *models.TaskSession) bool {
	return session != nil && session.RouteState == dynamicRouteStatusStarting &&
		session.RouteGeneration > 0 && session.ExecutionProfileID != ""
}

// hasLivePromptReadyAgent reports whether the session already has a running
// agent that can accept an immediate prompt, the ownership evidence a
// reconciling completion needs before it retries the durable activation.
func (s *Service) hasLivePromptReadyAgent(ctx context.Context, sessionID string) bool {
	return s.agentManager != nil &&
		s.agentManager.IsAgentRunningForSession(ctx, sessionID) &&
		s.agentManager.IsAgentReadyForPrompt(ctx, sessionID)
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
