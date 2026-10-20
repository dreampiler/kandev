package orchestrator

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
)

// dynamicRouteOrphanedStartingReason is the route reason recorded for a
// WAITING_FOR_INPUT session whose "starting" route has no executors_running row.
const dynamicRouteOrphanedStartingReason = "orphaned_starting_route"

// dynamicRouteAwaitsUserAction reports whether queued work for the session can
// only fail until the user acts on its dynamic route. The route is
// action_required and no executors_running row exists, so ensureSessionRunning
// has nothing to resume and no launch owner will create one. The session's
// route recovery banner (Retry / Try next / Stop) is the visible block; a
// successful route action launches the session again and drains its queue.
func (s *Service) dynamicRouteAwaitsUserAction(ctx context.Context, session *models.TaskSession) bool {
	if session == nil || session.RouteState != dynamicRouteStatusActionRequired {
		return false
	}
	_, err := s.repo.GetExecutorRunningBySessionID(ctx, session.ID)
	return errors.Is(err, models.ErrExecutorRunningNotFound)
}

// queueDrainBlockedByDynamicRoute keeps automatic queue drains from taking an
// entry that the session cannot run until its route action is applied.
// Without it every drain trigger reserves the head, fails in
// ensureSessionRunning, and returns the entry, which repeats indefinitely.
func (s *Service) queueDrainBlockedByDynamicRoute(ctx context.Context, sessionID string) bool {
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil || !s.dynamicRouteAwaitsUserAction(ctx, session) {
		return false
	}
	s.logger.Info("skipping queue drain while the dynamic route awaits a recovery action",
		zap.String("session_id", sessionID),
		zap.String("route_state", session.RouteState),
		zap.String("route_reason", session.RouteReason))
	return true
}

// retainQueuedMessageForDynamicRouteAction settles a queued dispatch that
// failed because the session has no executor record while its dynamic route
// has no launch owner. A "starting" route stranded this way is marked
// action_required at once, the same release the restart sweep performs, so
// the session shows its route recovery actions instead of an endless
// requeue. The entry is restored for that recovery rather than requeued. It
// returns false for every other failure, including a session whose launch is
// still pending, so the existing retry path keeps handling those.
func (s *Service) retainQueuedMessageForDynamicRouteAction(
	ctx context.Context,
	callerSessionID string,
	queuedMsg *messagequeue.QueuedMessage,
	reservation *queuedDispatchReservation,
	userMessageRecorded bool,
	err error,
) bool {
	if queuedMsg == nil || !errors.Is(err, errSessionAwaitingRuntimeLaunch) {
		return false
	}
	session, loadErr := s.repo.GetTaskSession(ctx, queuedMsg.SessionID)
	if loadErr != nil || session == nil {
		return false
	}
	if isStartingRouteClaim(session) && s.dynamicStartingRouteIsStranded(ctx, session) {
		s.markDynamicRouteActionRequired(ctx, session.ID, session.RouteGeneration, dynamicRouteOrphanedStartingReason)
		if reloaded, reloadErr := s.repo.GetTaskSession(ctx, session.ID); reloadErr == nil && reloaded != nil {
			session = reloaded
		}
	}
	if !s.dynamicRouteAwaitsUserAction(ctx, session) {
		return false
	}
	if userMessageRecorded {
		markQueuedUserMessageRecorded(queuedMsg)
	}
	s.logger.Warn("queued message retained until the dynamic route recovery action runs",
		zap.String("session_id", callerSessionID),
		zap.String("task_id", queuedMsg.TaskID),
		zap.String("queue_id", queuedMsg.ID),
		zap.String("route_state", session.RouteState),
		zap.String("route_reason", session.RouteReason))
	if reservation != nil && reservation.identity.SessionIncarnationID != "" {
		s.restoreQueuedMessageForSession(ctx, reservation.identity, queuedMsg)
	} else {
		s.restoreQueuedMessage(ctx, queuedMsg)
	}
	return true
}
