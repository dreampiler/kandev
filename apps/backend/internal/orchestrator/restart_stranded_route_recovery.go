package orchestrator

import (
	"context"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/task/models"
)

// A restart can leave a dynamic-profile session with three conditions at once:
// its route parked at action_required (the candidate died first and its policy
// stopped, or the successor launch failed), an open recovery block for the
// restart-interrupted turn (unknown_prompt_outcome / unresolved_durable_work /
// unknown_queue_dispatch_outcome), and an open turn row that no runtime will
// ever settle (each message refused by the block opens one).
//
// Each supported recovery path used to refuse one of the other two:
//   - session.recover (resume, continue_from_history) and the automatic
//     continuation refused every non-active route as dynamic_route_pending;
//   - session.route_action refused the open turn as "route actions require a
//     settled turn";
//   - after the turn was cancelled, the route action re-sent the last prompt,
//     which the open recovery block refused (route_action_launch_failed).
//
// The helpers below remove that mutual block without re-sending a prompt whose
// pre-restart outcome is unknown:
//   - an explicit recovery action may own an action_required route on its
//     persisted candidate when no runtime is live (the "explicit retry" edge);
//   - a route action settles a turn that is orphaned on a parked route;
//   - a manual route action on a session blocked by a restart-interrupted turn
//     hands its successor launch to continue_from_history on the selected
//     candidate instead of re-sending the interrupted prompt.

const (
	// explicitRecoveryRouteReason marks a route an explicit recovery reclaimed.
	explicitRecoveryRouteReason = "explicit_recovery"
	// explicitRecoveryFailedRouteReason returns a reclaimed route to manual
	// recovery after the recovery action itself failed.
	explicitRecoveryFailedRouteReason = "explicit_recovery_failed"
	// routeActionContinuationRouteReason parks a route action's claimed
	// generation while continue_from_history launches it.
	routeActionContinuationRouteReason = "route_action_continuation"
)

// recoveryActionRelaunchesAgent reports whether an explicit recovery action
// launches the session's agent, and so needs an owner for its dynamic route.
func recoveryActionRelaunchesAgent(action string) bool {
	switch action {
	case recoveryActionResume, recoveryActionContinueHistory, recoveryActionFreshStart, recoveryActionResumeNewBranch:
		return true
	default:
		return false
	}
}

// dynamicRouteParked reports whether a route has no launch owner: nothing is
// starting or running on it until a recovery action, a route action, or a
// policy deadline moves it.
func dynamicRouteParked(routeState string) bool {
	switch routeState {
	case dynamicRouteStatusActionRequired, dynamicRouteStatusWaiting, "retry_wait", "waiting_for_reset":
		return true
	default:
		return false
	}
}

func (s *Service) sessionRuntimeLive(ctx context.Context, sessionID string) bool {
	return s.agentManager != nil && s.agentManager.IsAgentRunningForSession(ctx, sessionID)
}

// reclaimActionRequiredRouteForRecovery lets an explicit recovery action own a
// dynamic route that is parked at action_required on a persisted candidate
// while no runtime is live. The durable route and the session projection both
// move to "starting" on the same generation and candidate, which is the state
// explicitRecoveryOwnsStrandedStartingRoute already admits. It returns the
// reclaimed generation; the caller must call restoreRouteAfterFailedRecovery
// when the recovery fails.
func (s *Service) reclaimActionRequiredRouteForRecovery(ctx context.Context, sessionID, action string) (int64, bool) {
	if s.profileExecutionResolver == nil || sessionID == "" || !recoveryActionRelaunchesAgent(action) {
		return 0, false
	}
	// Route actions hold this lock for their whole transaction, so a reclaim
	// can never interleave with a route action's own claim.
	release := s.acquireSessionRouteOperationLock(sessionID)
	defer release()
	session, ok := s.actionRequiredRouteWithoutRuntime(ctx, sessionID)
	if !ok {
		return 0, false
	}
	generation := session.RouteGeneration
	reclaimed, err := s.profileExecutionResolver.ReclaimActionRequiredRoute(ctx, sessionID, generation)
	if err != nil {
		s.logger.Warn("explicit recovery could not reclaim the parked dynamic route",
			zap.String("session_id", sessionID), zap.Int64("route_generation", generation), zap.Error(err))
		return 0, false
	}
	if !reclaimed {
		return 0, false
	}
	s.mirrorDynamicRouteProjection(ctx, session, generation, dynamicRouteStatusStarting, explicitRecoveryRouteReason)
	reloaded, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil || reloaded == nil || reloaded.RouteGeneration != generation ||
		reloaded.RouteState != dynamicRouteStatusStarting {
		// The projection did not follow the durable claim; return the claim so
		// both stores keep describing the same parked route.
		s.markDynamicRouteActionRequired(context.WithoutCancel(ctx), sessionID, generation, session.RouteReason)
		return 0, false
	}
	s.logger.Info("explicit recovery reclaimed a parked dynamic route",
		zap.String("session_id", sessionID), zap.String("action", action),
		zap.Int64("route_generation", generation), zap.String("execution_profile_id", session.ExecutionProfileID))
	return generation, true
}

// actionRequiredRouteWithoutRuntime loads a session whose dynamic route is
// parked at action_required on a persisted candidate while no runtime is live.
func (s *Service) actionRequiredRouteWithoutRuntime(ctx context.Context, sessionID string) (*models.TaskSession, bool) {
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil || session == nil || session.RouteState != dynamicRouteStatusActionRequired ||
		session.RouteGeneration <= 0 || session.ExecutionProfileID == "" {
		return nil, false
	}
	if s.sessionRuntimeLive(ctx, sessionID) {
		return nil, false
	}
	return session, true
}

// restoreRouteAfterFailedRecovery returns a route reclaimed by a failed
// explicit recovery to action_required. A route the launch already carried to
// active, or one whose runtime is live and will settle it through the process
// start callbacks, is left alone.
func (s *Service) restoreRouteAfterFailedRecovery(ctx context.Context, sessionID string, generation int64) {
	ctx = context.WithoutCancel(ctx)
	if s.sessionRuntimeLive(ctx, sessionID) {
		return
	}
	s.markDynamicRouteActionRequired(ctx, sessionID, generation, explicitRecoveryFailedRouteReason)
}

// settleOrphanTurnForRouteAction closes turn rows that no runtime can settle
// before a route action on a parked route. Such a turn is not active work:
// the route has no launch owner, no agent runs for the session, and no
// resume, queued dispatch, or steer is in flight. The caller holds the
// session's cancel-in-flight guard, so no prompt can be admitted meanwhile.
func (s *Service) settleOrphanTurnForRouteAction(ctx context.Context, sessionID string) bool {
	if s.turnService == nil || sessionID == "" {
		return false
	}
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil || session == nil || !dynamicRouteParked(session.RouteState) {
		return false
	}
	if session.State == models.TaskSessionStateRunning || session.State == models.TaskSessionStateStarting {
		return false
	}
	if s.sessionRuntimeLive(ctx, sessionID) {
		return false
	}
	if _, active := s.resumeAttemptStore().current(sessionID); active {
		return false
	}
	if s.isQueuedDispatchInFlight(sessionID) || s.isSteerInFlight(sessionID) {
		return false
	}
	s.activeTurns.Delete(sessionID)
	if err := s.turnService.AbandonOpenTurns(ctx, sessionID); err != nil {
		s.logger.Warn("failed to settle orphaned turn before route action",
			zap.String("session_id", sessionID), zap.Error(err))
		return false
	}
	remaining, err := s.turnService.GetActiveTurn(ctx, sessionID)
	if err != nil || remaining != nil {
		return false
	}
	s.logger.Info("settled orphaned turn on a parked dynamic route before route action",
		zap.String("session_id", sessionID), zap.String("route_state", session.RouteState))
	return true
}

// handOffRouteActionToContinuation keeps a manual route action from re-sending
// a prompt whose pre-restart outcome is unknown. When a restart-interrupted
// turn still blocks the session and no runtime is live, the claimed generation
// is parked at action_required and ApplyRouteAction runs continue_from_history
// on it after releasing the route-action lock. continue_from_history starts
// the selected candidate from the bounded transcript, settles the recovery
// block, and releases parked queue work.
func (s *Service) handOffRouteActionToContinuation(ctx context.Context, session *models.TaskSession) bool {
	if session == nil || session.ID == "" || session.TaskID == "" || session.RouteGeneration <= 0 {
		return false
	}
	block, err := s.GetOpenSessionRecoveryBlock(ctx, session.ID)
	if err != nil || block == nil || !interruptedTurnRecoveryReasons[block.Reason] {
		return false
	}
	if s.sessionRuntimeLive(ctx, session.ID) {
		return false
	}
	if isOfficeTask, officeErr := s.lookupOfficeTask(ctx, session.TaskID); officeErr != nil || isOfficeTask {
		return false
	}
	s.markDynamicRouteActionRequired(ctx, session.ID, session.RouteGeneration, routeActionContinuationRouteReason)
	parked, err := s.repo.GetTaskSession(ctx, session.ID)
	if err != nil || parked == nil || parked.RouteGeneration != session.RouteGeneration ||
		parked.RouteState != dynamicRouteStatusActionRequired {
		return false
	}
	s.routeActionContinuations.Store(session.ID, session.TaskID)
	s.logger.Info("route action hands its successor launch to continue_from_history",
		zap.String("session_id", session.ID), zap.String("recovery_reason", block.Reason),
		zap.Int64("route_generation", session.RouteGeneration))
	return true
}

// finishRouteActionContinuation runs the continuation a route action handed
// off, outside the route-action lock (resume acquires that same lock). A failed
// continuation leaves the route at action_required with the launch-failed
// reason, like a failed successor launch.
func (s *Service) finishRouteActionContinuation(
	ctx context.Context,
	sessionID string,
	result *RouteActionResult,
	actionErr error,
) (*RouteActionResult, error) {
	value, ok := s.routeActionContinuations.LoadAndDelete(sessionID)
	if !ok {
		return result, actionErr
	}
	taskID, _ := value.(string)
	if actionErr != nil || taskID == "" {
		return result, actionErr
	}
	if _, err := s.RecoverSessionWithOptions(
		ctx, taskID, sessionID, recoveryActionContinueHistory, RecoverSessionOptions{},
	); err != nil {
		s.logger.Warn("route action continuation failed",
			zap.String("task_id", taskID), zap.String("session_id", sessionID), zap.Error(err))
		if session, loadErr := s.repo.GetTaskSession(ctx, sessionID); loadErr == nil && session != nil {
			s.markDynamicRouteActionRequired(context.WithoutCancel(ctx), sessionID, session.RouteGeneration, RouteActionLaunchFailedReason)
		}
	}
	return s.refreshRouteActionResult(ctx, sessionID, result), nil
}

func (s *Service) refreshRouteActionResult(ctx context.Context, sessionID string, result *RouteActionResult) *RouteActionResult {
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil || session == nil {
		return result
	}
	if result == nil {
		result = &RouteActionResult{SessionID: sessionID, LogicalProfileID: session.AgentProfileID}
	}
	result.ExecutionProfileID = session.ExecutionProfileID
	result.RouteGeneration = session.RouteGeneration
	result.State = session.RouteState
	result.Reason = session.RouteReason
	return result
}
