package orchestrator

import (
	"context"
	"errors"
	"fmt"
	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/task/models"
)

func (s *Service) withProviderTurnAdmission(ctx context.Context, session *models.TaskSession, claim func() error) error {
	if s.profileExecutionResolver == nil || session == nil || session.RouteGeneration <= 0 || session.ExecutionProfileID == "" {
		return claim()
	}
	current := dynamicruntime.RouteDecision{SessionID: session.ID, LogicalProfileID: session.AgentProfileID, ExecutionProfileID: session.ExecutionProfileID, Generation: session.RouteGeneration}
	err := s.profileExecutionResolver.WithTurnAdmission(ctx, current, claim)
	var wait *dynamicruntime.NoEligibleCandidateError
	if errors.As(err, &wait) && wait.ResourceWait {
		s.recordDynamicRouteResolutionFailure(ctx, session, err)
		return fmt.Errorf("%w: %w", ErrAgentPromptInProgress, err)
	}
	return err
}
