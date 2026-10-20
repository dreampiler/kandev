package service_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/kandev/kandev/internal/office/service"
	taskmodels "github.com/kandev/kandev/internal/task/models"
)

// A launch refused because the inherited workspace cannot be safely reused
// is a deterministic ownership verdict, not a transient fault: retrying it on
// the 2m-2h backoff only hides it. The run fails at once and escalates, so the
// reason reaches the run, activity, and inbox surfaces.
func TestHandleRunFailure_WorkspaceReuseUnsafeDoesNotRetry(t *testing.T) {
	svc, _ := newTestServiceWithBus(t)
	ctx := context.Background()

	createTestAgent(t, svc, "ws-1", "agent-reuse-unsafe")
	taskID := "task-reuse-unsafe"
	insertSyntheticTask(t, svc, taskID, "ws-1", "agent-reuse-unsafe")
	run := queueAndReadRun(t, svc, "agent-reuse-unsafe", taskID)

	launchErr := fmt.Errorf("%w: inherited task environment is unavailable: parent task p1 workspace could not be resolved",
		taskmodels.ErrWorkspaceReuseUnsafe)
	if err := svc.HandleRunFailure(ctx, run, launchErr); err != nil {
		t.Fatalf("handle failure: %v", err)
	}

	refreshed, err := svc.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if refreshed.Status != service.RunStatusFailed {
		t.Fatalf("run status = %q, want %q (unsafe workspace reuse must fail, not retry silently)",
			refreshed.Status, service.RunStatusFailed)
	}
	if refreshed.RetryCount != 0 || refreshed.ScheduledRetryAt != nil {
		t.Fatalf("retry_count = %d scheduled_retry_at = %v, want no retry scheduled",
			refreshed.RetryCount, refreshed.ScheduledRetryAt)
	}
}
