package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

func TestDeferredRetrySchedule_SpacesAttemptsAndCapsTheWait(t *testing.T) {
	schedule := newDeferredRetrySchedule()
	now := time.Now()
	schedule.now = func() time.Time { return now }

	require.True(t, schedule.beginAttempt("task-a"), "the first attempt of a waiting record is due now")
	require.False(t, schedule.beginAttempt("task-a"), "a repeat refusal must not be retried on the next tick")

	// Each granted attempt doubles the wait, bounded by the cap.
	for attempt := 2; attempt <= 8; attempt++ {
		now = now.Add(ceilingRetryMaxInterval)
		require.True(t, schedule.beginAttempt("task-a"), "attempt %d is due once the wait elapsed", attempt)
	}
	schedule.mu.Lock()
	refusals := schedule.entries["task-a"].refusals
	schedule.mu.Unlock()
	require.Equal(t, 8, refusals)

	wait := schedule.delay(refusals)
	require.LessOrEqual(t, wait, ceilingRetryMaxInterval)
	require.Greater(t, wait, time.Duration(0))
}

func TestDeferredRetrySchedule_SettleAndPruneForgetFinishedRecords(t *testing.T) {
	schedule := newDeferredRetrySchedule()
	now := time.Now()
	schedule.now = func() time.Time { return now }

	require.True(t, schedule.beginAttempt("launched"))
	require.True(t, schedule.beginAttempt("dropped"))
	require.True(t, schedule.beginAttempt("still-waiting"))

	schedule.settle("launched")
	schedule.prune(map[string]struct{}{"still-waiting": {}})
	schedule.settle("still-waiting")

	schedule.mu.Lock()
	require.Len(t, schedule.entries, 0)
	schedule.mu.Unlock()

	// A task that stops waiting and defers again must not inherit the old wait.
	require.True(t, schedule.beginAttempt("still-waiting"))
}

func TestDeferredRetrySchedule_ObserveRebindsAReplacedRecord(t *testing.T) {
	schedule := newDeferredRetrySchedule()
	now := time.Now()
	schedule.now = func() time.Time { return now }

	require.True(t, schedule.beginAttempt("task-a"))
	require.False(t, schedule.beginAttempt("task-a"))

	// The record this entry already describes keeps its wait, however many
	// times the sweep re-reads it.
	schedule.observe("task-a", "start|2026-10-03T00:00:00Z")
	require.False(t, schedule.beginAttempt("task-a"))
	schedule.observe("task-a", "start|2026-10-03T00:00:00Z")
	require.False(t, schedule.beginAttempt("task-a"))

	// A record replaced while it waited is a different launch, so it is due now
	// rather than inheriting the previous launch's backoff.
	schedule.observe("task-a", "start|2026-10-03T00:05:00Z")
	require.True(t, schedule.beginAttempt("task-a"))
}

// TestCeilingSweep_PeriodicPassIsPacedAndSignalPassIsNot pins the two halves of
// the pacing contract: the periodic backstop does not repeat the same refusal
// on every tick, and a pass that exists because capacity freed up retries the
// record immediately rather than waiting out the backoff.
func TestCeilingSweep_PeriodicPassIsPacedAndSignalPassIsNot(t *testing.T) {
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

	// Freeze the retry schedule's clock so "the wait has not elapsed" is a
	// statement about pacing rather than about wall-clock timing.
	frozen := time.Now()
	svc.deferredRetrySchedule.now = func() time.Time { return frozen }

	decision := svc.sessionCeiling.admit(ctx, admissionRequest{
		taskID: "pace-task-a", sessionID: "pace-session-a", origin: launchOriginAutomatic, seam: "test-setup",
	})
	require.True(t, decision.admitted)

	exec, err := svc.StartCreatedSession(ctx, "pace-task-b", "pace-session-b", "profile-1", "go", false, false, true, nil, nil)
	require.NoError(t, err)
	require.Nil(t, exec)
	require.NotNil(t, deferredLaunchOf(t, svc, "pace-task-b"))

	// First periodic pass: the record is due, so it is attempted and refused.
	svc.ceilingSweepTick(ctx, ceilingSweepPeriodic)
	require.NotNil(t, deferredLaunchOf(t, svc, "pace-task-b"), "a still-refused replay must not clear the record")

	// The slot frees, but the periodic backstop has already counted a refusal
	// for this record, so the next tick within the wait does not repeat it.
	svc.sessionCeiling.release("pace-session-a")
	svc.ceilingSweepTick(ctx, ceilingSweepPeriodic)

	agentMgr.mu.Lock()
	callsWhileBackingOff := len(agentMgr.setExecutionDescriptionCalls)
	agentMgr.mu.Unlock()
	require.Zero(t, callsWhileBackingOff, "a periodic pass must not retry a record whose backoff has not elapsed")
	require.NotNil(t, deferredLaunchOf(t, svc, "pace-task-b"))

	// A release-driven pass exists because capacity changed, so it retries now.
	svc.ceilingSweepTick(ctx, ceilingSweepSignal)

	agentMgr.mu.Lock()
	callsAfterSignal := len(agentMgr.setExecutionDescriptionCalls)
	agentMgr.mu.Unlock()
	require.Equal(t, 1, callsAfterSignal, "a release-driven pass must retry a waiting record immediately")
	require.False(t, models.HasCeilingDeferredIntent(&models.Task{Metadata: map[string]interface{}{
		models.MetaKeyDeferredLaunch: deferredLaunchOf(t, svc, "pace-task-b"),
	}}), "the admitted replay must clear the ceiling record")
}

// TestCeilingSweep_DirectDrainIsNotPaced keeps a caller that is not the periodic
// sweeper — the send-now path, a bootstrap recovery — outside the schedule.
func TestCeilingSweep_DirectDrainIsNotPaced(t *testing.T) {
	require.False(t, isPeriodicCeilingSweep(context.Background()))
	require.False(t, isPeriodicCeilingSweep(withCeilingSweepCause(context.Background(), ceilingSweepSignal)))
	require.True(t, isPeriodicCeilingSweep(withCeilingSweepCause(context.Background(), ceilingSweepPeriodic)))

	// A nil schedule must not gate anything: a Service built without the pacing
	// field still admits every record.
	var missing *deferredRetrySchedule
	require.True(t, missing.beginAttempt("task-a"))
	missing.observe("task-a", "identity")
	missing.settle("task-a")
	missing.prune(nil)
}
