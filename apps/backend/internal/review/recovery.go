package review

import (
	"context"
	"fmt"

	"github.com/kandev/kandev/internal/task/models"
)

// reconcileUnownedRun is called only after acquiring the task's in-memory slot.
// Native passes execute in this runner, so a retained native pass without that
// worker cannot finish. Agent-owned and unclassified legacy passes do not have
// that ownership guarantee and must remain untouched.
func (r *Runner) reconcileUnownedRun(ctx context.Context, taskID string) (*models.TaskReviewRun, error) {
	existing, err := r.store.ActiveRun(ctx, taskID)
	if err != nil || existing == nil {
		return nil, err
	}
	if existing.Trigger != models.ReviewTriggerManual && existing.Trigger != models.ReviewTriggerWorkflowStep {
		return existing, nil
	}
	r.settlementMu.Lock()
	defer r.settlementMu.Unlock()
	settled, err := r.store.FailRun(ctx, existing.ID, CodeExecutionFailed,
		"review worker is no longer running", existing.DurationMs)
	if err != nil {
		return nil, fmt.Errorf("settle abandoned review run: %w", err)
	}
	if settled == nil || !settled.Status.IsTerminal() {
		return nil, fmt.Errorf("abandoned review run did not reach a terminal state")
	}
	return nil, nil
}
