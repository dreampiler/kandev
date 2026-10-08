package orchestrator

import (
	"context"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/orchestrator/watcher"
	"github.com/kandev/kandev/internal/task/models"
	"go.uber.org/zap"
)

// dynamicFailureSession returns the routed session a failure may advance: the
// failed execution must be the session's execution and must have been launched
// for the candidate the route holds now.
func (s *Service) dynamicFailureSession(
	ctx context.Context,
	data watcher.AgentEventData,
) (*models.TaskSession, bool) {
	session, ok := s.dynamicRouteSession(ctx, data)
	if !ok || failureFromSupersededCandidate(data, session) {
		return nil, false
	}
	return session, true
}

func (s *Service) dynamicRouteSession(
	ctx context.Context,
	data watcher.AgentEventData,
) (*models.TaskSession, bool) {
	if s.profileExecutionResolver == nil || data.SessionID == "" {
		return nil, false
	}
	session, err := s.repo.GetTaskSession(ctx, data.SessionID)
	if err != nil || session == nil || session.RouteGeneration <= 0 || session.ExecutionProfileID == "" {
		return nil, false
	}
	if session.AgentExecutionID != "" && data.AgentExecutionID != "" &&
		session.AgentExecutionID != data.AgentExecutionID {
		return nil, false
	}
	return session, true
}

// failureFromSupersededCandidate reports whether the failed execution runs a
// different concrete profile than the route's current candidate. The route
// claims a successor before the predecessor's process is replaced, and a
// deferred successor launch leaves the predecessor serving the session, so a
// later failure of that execution belongs to the predecessor's profile.
func failureFromSupersededCandidate(data watcher.AgentEventData, session *models.TaskSession) bool {
	return data.ExecutionProfileID != "" && session != nil &&
		session.ExecutionProfileID != data.ExecutionProfileID
}

// recordSupersededCandidateFailure attributes a resource failure to the profile
// the failed execution actually ran when the route already moved past it. The
// route is not advanced again and the current candidate is not suspended.
func (s *Service) recordSupersededCandidateFailure(
	ctx context.Context,
	data watcher.AgentEventData,
	classified *routingerr.Error,
) {
	if classified == nil || !classified.FallbackAllowed || classified.Confidence == routingerr.ConfLow {
		return
	}
	session, ok := s.dynamicRouteSession(ctx, data)
	if !ok || !failureFromSupersededCandidate(data, session) {
		return
	}
	s.logger.Info("dynamic failure belongs to a superseded candidate; route left unchanged",
		zap.String("task_id", data.TaskID),
		zap.String("session_id", data.SessionID),
		zap.String("agent_execution_id", data.AgentExecutionID),
		zap.String("failed_execution_profile_id", data.ExecutionProfileID),
		zap.String("route_execution_profile_id", session.ExecutionProfileID),
		zap.Int64("route_generation", session.RouteGeneration),
		zap.String("routing_code", string(classified.Code)))
	s.profileExecutionResolver.RecordResourceFailure(ctx, data.ExecutionProfileID, classified)
}
