package maintenance

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
)

// fakeDeliveryRetentionRepo records which steps run and injects per-step
// results so tests can exercise the continue-on-timeout and abort-on-error
// behavior of executeDeliveryRetention without a real database.
type fakeDeliveryRetentionRepo struct {
	retentionFn  func(context.Context, models.AgentDeliveryRetentionBatchOptions) (models.AgentDeliveryRetentionBatchResult, error)
	orphanFn     func(context.Context, models.AgentDeliveryOrphanBatchOptions) (models.AgentDeliveryOrphanBatchResult, error)
	retirementFn func(context.Context, models.AgentDeliveryRetirementBatchOptions) (models.AgentDeliveryRetirementBatchResult, error)
}

func (f *fakeDeliveryRetentionRepo) RunAgentDeliveryRetentionBatch(ctx context.Context, o models.AgentDeliveryRetentionBatchOptions) (models.AgentDeliveryRetentionBatchResult, error) {
	return f.retentionFn(ctx, o)
}

func (f *fakeDeliveryRetentionRepo) RunAgentDeliveryOrphanBatch(ctx context.Context, o models.AgentDeliveryOrphanBatchOptions) (models.AgentDeliveryOrphanBatchResult, error) {
	return f.orphanFn(ctx, o)
}

func (f *fakeDeliveryRetentionRepo) RunAgentDeliveryRetirementBatch(ctx context.Context, o models.AgentDeliveryRetirementBatchOptions) (models.AgentDeliveryRetirementBatchResult, error) {
	return f.retirementFn(ctx, o)
}

// TestExecuteDeliveryRetentionContinuesPastOrphanTimeout proves a step that
// exceeds its budget is recorded as partial and the remaining steps (the
// retired-session purge) still run, rather than aborting the run.
func TestExecuteDeliveryRetentionContinuesPastOrphanTimeout(t *testing.T) {
	retirementCalled := false
	repo := &fakeDeliveryRetentionRepo{
		retentionFn: func(context.Context, models.AgentDeliveryRetentionBatchOptions) (models.AgentDeliveryRetentionBatchResult, error) {
			return models.AgentDeliveryRetentionBatchResult{EventPayloadsPruned: 3}, nil
		},
		orphanFn: func(context.Context, models.AgentDeliveryOrphanBatchOptions) (models.AgentDeliveryOrphanBatchResult, error) {
			return models.AgentDeliveryOrphanBatchResult{}, context.DeadlineExceeded
		},
		retirementFn: func(context.Context, models.AgentDeliveryRetirementBatchOptions) (models.AgentDeliveryRetirementBatchResult, error) {
			retirementCalled = true
			return models.AgentDeliveryRetirementBatchResult{RowsPurged: 5, Completed: 1}, nil
		},
	}

	result, err := executeDeliveryRetention(context.Background(), repo, models.AgentDeliveryRetentionBatchOptions{})
	if err != nil {
		t.Fatalf("executeDeliveryRetention = %v, want nil (timeout is partial, not fatal)", err)
	}
	if !result.Partial {
		t.Fatal("result.Partial = false, want true after a step timeout")
	}
	if !retirementCalled {
		t.Fatal("retirement step did not run after the orphan step timed out")
	}
	if result.DeletedDeliveryEventPayloads != 3 || result.PurgedRetiredDeliveryRows != 5 || result.DeliveryRetirementJobsCompleted != 1 {
		t.Fatalf("result = %+v, want retention and retirement work recorded", result)
	}
}

// TestExecuteDeliveryRetentionAbortsOnNonTimeoutError proves a step that
// fails for a non-timeout reason is fatal: the run stops and the remaining
// steps do not run.
func TestExecuteDeliveryRetentionAbortsOnNonTimeoutError(t *testing.T) {
	injected := errors.New("injected storage failure")
	retirementCalled := false
	repo := &fakeDeliveryRetentionRepo{
		retentionFn: func(context.Context, models.AgentDeliveryRetentionBatchOptions) (models.AgentDeliveryRetentionBatchResult, error) {
			return models.AgentDeliveryRetentionBatchResult{}, nil
		},
		orphanFn: func(context.Context, models.AgentDeliveryOrphanBatchOptions) (models.AgentDeliveryOrphanBatchResult, error) {
			return models.AgentDeliveryOrphanBatchResult{}, injected
		},
		retirementFn: func(context.Context, models.AgentDeliveryRetirementBatchOptions) (models.AgentDeliveryRetirementBatchResult, error) {
			retirementCalled = true
			return models.AgentDeliveryRetirementBatchResult{}, nil
		},
	}

	_, err := executeDeliveryRetention(context.Background(), repo, models.AgentDeliveryRetentionBatchOptions{})
	if !errors.Is(err, injected) {
		t.Fatalf("error = %v, want the injected failure", err)
	}
	if retirementCalled {
		t.Fatal("retirement step ran after a fatal orphan failure")
	}
}
