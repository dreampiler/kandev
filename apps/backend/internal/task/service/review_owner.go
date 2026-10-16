package service

import (
	"context"
	"fmt"

	"github.com/kandev/kandev/internal/task/models"
)

// SetRunReviewer binds a pending real run to its final concrete selection.
// The repository CAS prevents a concurrent cancel from being resurrected.
func (s *ReviewService) SetRunReviewer(ctx context.Context, runID, agentID, model string) (*models.TaskReviewRun, error) {
	writer, ok := s.repo.(interface {
		SetTaskReviewRunReviewer(context.Context, string, string, string) (bool, error)
	})
	if !ok {
		return nil, fmt.Errorf("review run attribution is unavailable")
	}
	updated, err := writer.SetTaskReviewRunReviewer(ctx, runID, agentID, model)
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, fmt.Errorf("review run %s is no longer pending", runID)
	}
	run, err := s.repo.GetTaskReviewRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != models.ReviewRunPending {
		return nil, fmt.Errorf("review run %s is no longer pending", runID)
	}
	s.publishRun(ctx, run)
	return run, nil
}

// StartOwnedRun is the final pending-state claim for a native route. A cancel
// before the detached worker starts stays terminal and cannot launch inference.
func (s *ReviewService) StartOwnedRun(ctx context.Context, runID string) (bool, error) {
	writer, ok := s.repo.(interface {
		StartTaskReviewRunIfPending(context.Context, string) (bool, error)
	})
	if !ok {
		return false, fmt.Errorf("native review run start claim is unavailable")
	}
	claimed, err := writer.StartTaskReviewRunIfPending(ctx, runID)
	if err != nil || !claimed {
		return false, err
	}
	run, err := s.repo.GetTaskReviewRun(ctx, runID)
	if err != nil {
		return false, err
	}
	s.publishRun(ctx, run)
	return run.Status == models.ReviewRunRunning, nil
}
