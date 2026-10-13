package orchestrator

import (
	"context"
	"errors"

	"github.com/kandev/kandev/internal/task/models"
)

func (s *Service) workflowEntryLaunchSession(ctx context.Context, session *models.TaskSession) (*models.TaskSession, error) {
	if session.State != models.TaskSessionStateWaitingForInput || session.AgentExecutionID != "" ||
		session.DownstreamACPSessionID != "" {
		return session, nil
	}
	running, err := s.repo.GetExecutorRunningBySessionID(ctx, session.ID)
	if err != nil && !errors.Is(err, models.ErrExecutorRunningNotFound) {
		return nil, err
	}
	if running != nil {
		return session, nil
	}
	hasHistory, err := s.repo.HasUserPromptHistory(ctx, session.ID)
	if err != nil {
		return nil, err
	}
	unselectedRoute := session.RouteState == dynamicRouteStatusWaiting &&
		session.ExecutionProfileID == "" && session.RouteReason == "no_eligible_candidate"
	if hasHistory && !unselectedRoute {
		return session, nil
	}
	// Workflow moves can idle a prepared session before it has any executor.
	// Keep persistence unchanged; the existing created-session start admits it.
	launchSession := *session
	launchSession.State = models.TaskSessionStateCreated
	return &launchSession, nil
}
