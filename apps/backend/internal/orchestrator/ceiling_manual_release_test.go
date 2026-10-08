package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/automation"
	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// TestDiscardDeferredCeilingLaunch_ReleasesTheHoldAndFailsTheRun proves the
// escape hatch: a task whose automatic start is stuck deferred by the session
// ceiling can have that record discarded on demand. Discarding fails the
// automation run the launch served (nothing will ever launch for it) and clears
// the durable record so later automatic launches stop being refused.
func TestDiscardDeferredCeilingLaunch_ReleasesTheHoldAndFailsTheRun(t *testing.T) {
	ctx := context.Background()
	f := deferAutomationStartAtCeiling(t, "t-manual-release")

	discarded, err := f.svc.DiscardDeferredCeilingLaunch(ctx, f.taskID)
	require.NoError(t, err)
	require.True(t, discarded, "a present record must be reported as cleared")
	require.False(t, f.ceilingDeferred(t), "the durable record must be gone")
	require.Equal(t, automation.RunStatusFailed, f.run(t).Status,
		"the release fails the run whose start can no longer launch")
	require.Zero(t, f.activeRuns(t), "the failed run releases its max_concurrent_runs slot")
	require.Zero(t, f.launches, "the release must not launch")
}

// TestDiscardDeferredCeilingLaunch_IsIdempotent proves a retried release of a
// task that holds no ceiling record is a no-op, so an operator can safely repeat
// the call.
func TestDiscardDeferredCeilingLaunch_IsIdempotent(t *testing.T) {
	ctx := context.Background()
	f := deferAutomationStartAtCeiling(t, "t-manual-release-idempotent")

	first, err := f.svc.DiscardDeferredCeilingLaunch(ctx, f.taskID)
	require.NoError(t, err)
	require.True(t, first)

	second, err := f.svc.DiscardDeferredCeilingLaunch(ctx, f.taskID)
	require.NoError(t, err)
	require.False(t, second, "a task with no ceiling record is a no-op")
}

// TestDiscardDeferredCeilingLaunch_NoRecordIsNoOp covers the ordinary task that
// has never deferred a launch.
func TestDiscardDeferredCeilingLaunch_NoRecordIsNoOp(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "t-manual-release-none", "s-manual-release-none", models.TaskSessionStateWaitingForInput)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, "t-manual-release-none", v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, &mockAgentManager{})

	discarded, err := svc.DiscardDeferredCeilingLaunch(ctx, "t-manual-release-none")
	require.NoError(t, err)
	require.False(t, discarded)
}
