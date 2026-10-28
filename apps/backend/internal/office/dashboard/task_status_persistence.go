package dashboard

import (
	"context"
	"fmt"
)

func (s *DashboardService) persistTaskStatus(ctx context.Context, req TaskStatusUpdateRequest, state, expectedStepID string) error {
	if req.ExpectedCurrentState != "" {
		return s.persistAutomaticTaskStatus(ctx, req, state)
	}
	if state != stateCompleted {
		if err := s.repo.UpdateTaskState(ctx, req.TaskID, state); err != nil {
			return fmt.Errorf("update task state: %w", err)
		}
		return nil
	}
	updated, err := s.repo.UpdateTaskStateIfWorkflowStep(ctx, req.TaskID, expectedStepID, state)
	if err != nil {
		return fmt.Errorf("update task state: %w", err)
	}
	if !updated {
		return &WorkflowStepChangedError{TaskID: req.TaskID}
	}
	return nil
}

func (s *DashboardService) persistAutomaticTaskStatus(ctx context.Context, req TaskStatusUpdateRequest, state string) error {
	updated, err := s.repo.UpdateTaskStateIfWorkflowStepAndState(
		ctx, req.TaskID, req.ExpectedWorkflowStepID, req.ExpectedCurrentState, state,
	)
	if err != nil {
		return fmt.Errorf("update automatic task state: %w", err)
	}
	if !updated {
		return ErrAutomaticCompletionStale
	}
	return nil
}
