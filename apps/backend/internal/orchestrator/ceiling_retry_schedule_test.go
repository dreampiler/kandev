package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// TestDeferredRetrySchedule_FailureWaitIsBoundedAndIdentityScoped pins the only
// time-based wait the schedule keeps: a replay that failed for a non-capacity
// reason is not retried again until the base interval elapses, and a replaced
// record does not inherit the wait.
func TestDeferredRetrySchedule_FailureWaitIsBoundedAndIdentityScoped(t *testing.T) {
	schedule := newDeferredRetrySchedule()
	now := time.Now()
	schedule.now = func() time.Time { return now }

	require.False(t, schedule.failureWaiting("task-a", "start|t0"), "an untracked record is never waiting")

	schedule.recordFailure("task-a", "start|t0")
	require.True(t, schedule.failureWaiting("task-a", "start|t0"), "a failed replay waits out the base interval")
	require.False(t, schedule.failureWaiting("task-a", "start|t1"),
		"a replaced record is a different launch and does not inherit the wait")

	now = now.Add(ceilingRetryBaseInterval)
	require.False(t, schedule.failureWaiting("task-a", "start|t0"), "the wait clears once the base interval elapsed")
}

func TestDeferredRetrySchedule_SettleAndPruneForgetFinishedRecords(t *testing.T) {
	schedule := newDeferredRetrySchedule()
	now := time.Now()
	schedule.now = func() time.Time { return now }

	schedule.recordFailure("launched", "k")
	schedule.recordFailure("dropped", "k")
	schedule.recordFailure("still-waiting", "k")

	schedule.settle("launched")
	schedule.prune(map[string]struct{}{"still-waiting": {}})

	schedule.mu.Lock()
	require.Len(t, schedule.entries, 1)
	_, kept := schedule.entries["still-waiting"]
	schedule.mu.Unlock()
	require.True(t, kept)

	// A task that stops waiting and defers again must not inherit the old wait.
	schedule.settle("still-waiting")
	schedule.mu.Lock()
	require.Len(t, schedule.entries, 0)
	schedule.mu.Unlock()
}

func TestDeferredRetrySchedule_ObserveRebindsAReplacedRecord(t *testing.T) {
	schedule := newDeferredRetrySchedule()
	now := time.Now()
	schedule.now = func() time.Time { return now }

	schedule.recordFailure("task-a", "start|t0")
	require.True(t, schedule.failureWaiting("task-a", "start|t0"))

	// The record this entry already describes keeps its wait, however many
	// times the sweep re-reads it.
	schedule.observe("task-a", "start|t0")
	require.True(t, schedule.failureWaiting("task-a", "start|t0"))

	// A record replaced while it waited is a different launch, so the failure
	// wait is cleared rather than inherited.
	schedule.observe("task-a", "start|t1")
	require.False(t, schedule.failureWaiting("task-a", "start|t0"))
	require.False(t, schedule.failureWaiting("task-a", "start|t1"))
}

// TestCeilingSweep_PeriodicBackstopOnlyDispatchesWhenALaneIsFree pins the
// change-gated backstop: while the lane is saturated a periodic pass does not
// re-run admission, and once capacity frees the periodic pass itself retries the
// record, so a release whose signal never reached the sweeper still recovers.
func TestCeilingSweep_PeriodicBackstopOnlyDispatchesWhenALaneIsFree(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "pace-task-a", "pace-session-a", models.TaskSessionStateCreated)
	seedExecutorRunning(t, repo, "pace-session-a", "pace-task-a", "exec-a")
	seedTaskAndSession(t, repo, "pace-task-b", "pace-session-b", models.TaskSessionStateCreated)
	seedExecutorRunning(t, repo, "pace-session-b", "pace-task-b", "exec-b")

	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, "pace-task-a", v1.TaskStateInProgress)
	seedMockTaskState(taskRepo, "pace-task-b", v1.TaskStateInProgress)
	agentMgr := &mockAgentManager{repoForExecutionLookup: repo}
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, agentMgr)
	svc.sessionCeiling = newSessionCeilingController(1, nil, nil)

	decision := svc.sessionCeiling.admit(ctx, admissionRequest{
		taskID: "pace-task-a", sessionID: "pace-session-a", origin: launchOriginAutomatic, seam: "test-setup",
	})
	require.True(t, decision.admitted)

	exec, err := svc.StartCreatedSession(ctx, "pace-task-b", "pace-session-b", "profile-1", "go", false, false, true, nil, nil)
	require.NoError(t, err)
	require.Nil(t, exec)
	require.NotNil(t, deferredLaunchOf(t, svc, "pace-task-b"))

	// Saturated: the periodic backstop must not re-run admission, so no agent
	// is dispatched and the record is preserved.
	svc.ceilingSweepTick(ctx, ceilingSweepPeriodic)
	require.NotNil(t, deferredLaunchOf(t, svc, "pace-task-b"), "a still-refused replay must not clear the record")

	agentMgr.mu.Lock()
	callsWhileSaturated := len(agentMgr.setExecutionDescriptionCalls)
	agentMgr.mu.Unlock()
	require.Zero(t, callsWhileSaturated, "a periodic pass must not re-run admission while the lane is saturated")

	// The slot frees without an explicit signal (release() alone does not
	// signal); the periodic backstop must still see the free capacity and retry.
	svc.sessionCeiling.release("pace-session-a")
	svc.ceilingSweepTick(ctx, ceilingSweepPeriodic)

	agentMgr.mu.Lock()
	callsAfterFree := len(agentMgr.setExecutionDescriptionCalls)
	agentMgr.mu.Unlock()
	require.Equal(t, 1, callsAfterFree, "the periodic backstop must retry once free capacity appears")
	require.False(t, models.HasCeilingDeferredIntent(&models.Task{Metadata: map[string]interface{}{
		models.MetaKeyDeferredLaunch: deferredLaunchOf(t, svc, "pace-task-b"),
	}}), "the admitted replay must clear the ceiling record")
}

// TestCeilingSweep_DirectDrainIsNotPaced keeps a caller that is not the periodic
// sweeper — the send-now path, a bootstrap recovery — outside the capacity gate.
func TestCeilingSweep_DirectDrainIsNotPaced(t *testing.T) {
	require.False(t, isPeriodicCeilingSweep(context.Background()))
	require.False(t, isPeriodicCeilingSweep(withCeilingSweepCause(context.Background(), ceilingSweepSignal)))
	require.True(t, isPeriodicCeilingSweep(withCeilingSweepCause(context.Background(), ceilingSweepPeriodic)))

	// A nil schedule must not gate anything: a Service built without it still
	// retries every record.
	var missing *deferredRetrySchedule
	require.False(t, missing.failureWaiting("task-a", "identity"))
	missing.observe("task-a", "identity")
	missing.recordFailure("task-a", "identity")
	missing.settle("task-a")
	missing.prune(nil)
}
