package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
	tasksqlite "github.com/kandev/kandev/internal/task/repository/sqlite"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// seedCeilingPrecedenceTask creates a task whose destination session exists and
// waits, plus a resume deferral queued at the given time. The session state is
// deliberately WAITING_FOR_INPUT so the record passes destination validation
// and ranks as an eligible queued launch.
func seedCeilingPrecedenceTask(
	t *testing.T, repo *tasksqlite.Repository, taskID, sessionID string,
	state v1.TaskState, sessionState models.TaskSessionState, queuedAt time.Time,
) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	_ = repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws-precedence", Name: "P", CreatedAt: now, UpdatedAt: now})
	_ = repo.CreateWorkflow(ctx, &models.Workflow{
		ID: "wf-precedence", WorkspaceID: "ws-precedence", Name: "P", CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: taskID, WorkflowID: "wf-precedence", Title: taskID,
		State: state, CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, repo.CreateTaskSession(ctx, &models.TaskSession{
		ID: sessionID, TaskID: taskID, State: sessionState, StartedAt: now, UpdatedAt: now,
	}))
	deferral := models.CeilingDeferral{
		Kind:       models.CeilingLaunchResume,
		Payload:    map[string]interface{}{metaKeySessionID: sessionID},
		Origin:     string(launchOriginAutomatic),
		ReasonCode: ceilingReasonRefused,
		QueuedAt:   queuedAt,
	}
	_, _, err := repo.SetTaskDeferredLaunchIfUnchanged(
		ctx, taskID, tasksqlite.AbsentDeferredLaunch(), models.CeilingRecordKeys(deferral),
	)
	require.NoError(t, err)
}

func newCeilingPrecedenceService(t *testing.T, ceiling int) (*Service, *tasksqlite.Repository) {
	t.Helper()
	svc, repo := newServiceWithRealRepo(t)
	svc.sessionCeiling = newSessionCeilingController(ceiling, nil, nil)
	return svc, repo
}

// TestCeilingPrecedence_ClaimedDeferredLaunchIsAdmittedOverLaterRecord pins the
// regression #140 introduced: a dispatcher's own deferred record is excluded
// from candidacy by its in-flight claim, and the scan must not then rank a later
// record ahead of it. The head launch takes its own free slot.
func TestCeilingPrecedence_ClaimedDeferredLaunchIsAdmittedOverLaterRecord(t *testing.T) {
	svc, repo := newCeilingPrecedenceService(t, 10)
	ctx := context.Background()
	seedCeilingPrecedenceTask(t, repo, "task-head", "session-head", v1.TaskStateInProgress, models.TaskSessionStateWaitingForInput, time.Now().Add(-time.Minute))
	seedCeilingPrecedenceTask(t, repo, "task-tail", "session-tail", v1.TaskStateInProgress, models.TaskSessionStateWaitingForInput, time.Now())

	claim, found, err := svc.claimCeilingDeferredLaunch(ctx, "task-head", "session-head", ceilingClaimOwnerReplay)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, claim)

	decision := svc.admitCeilingLaunch(withCeilingDispatchClaim(ctx, claim), admissionRequest{
		taskID: "task-head", sessionID: "session-head", origin: launchOriginAutomatic, seam: "ensureSessionRunning",
	})
	require.True(t, decision.admitted, "the head deferred launch must not yield to a later record")
	require.NotEqual(t, ceilingReasonDeferredPrecedes, decision.reasonCode)
}

// TestCeilingPrecedence_NewLaunchYieldsToEarlierDeferredRecord preserves
// AC-AGENTS-SESSION-CEILING-001.12: a genuinely new automatic launch still
// yields free capacity to the earliest eligible deferred launch.
func TestCeilingPrecedence_NewLaunchYieldsToEarlierDeferredRecord(t *testing.T) {
	svc, repo := newCeilingPrecedenceService(t, 10)
	seedCeilingPrecedenceTask(t, repo, "task-head", "session-head", v1.TaskStateInProgress, models.TaskSessionStateWaitingForInput, time.Now().Add(-time.Minute))
	seedCeilingPrecedenceTask(t, repo, "task-tail", "session-tail", v1.TaskStateInProgress, models.TaskSessionStateWaitingForInput, time.Now())

	decision := svc.admitCeilingLaunch(context.Background(), admissionRequest{
		taskID: "new-task", sessionID: "new-session", origin: launchOriginAutomatic, seam: "startTask",
	})
	require.False(t, decision.admitted)
	require.Equal(t, ceilingReasonDeferredPrecedes, decision.reasonCode)
	require.Equal(t, "task-head", decision.precedesTaskID)
}

// TestCeilingPrecedence_TerminatedRecordDoesNotBlockFreeSlot pins that a
// cancelled task's stale record is not a predecessor, so it cannot hold a free
// unit for itself.
func TestCeilingPrecedence_TerminatedRecordDoesNotBlockFreeSlot(t *testing.T) {
	svc, repo := newCeilingPrecedenceService(t, 10)
	seedCeilingPrecedenceTask(t, repo, "task-cancelled", "session-cancelled", v1.TaskStateCancelled, models.TaskSessionStateWaitingForInput, time.Now().Add(-time.Minute))

	decision := svc.admitCeilingLaunch(context.Background(), admissionRequest{
		taskID: "new-task", sessionID: "new-session", origin: launchOriginAutomatic, seam: "startTask",
	})
	require.True(t, decision.admitted, "a terminated record must not block a free slot")
}

// TestCeilingPrecedence_ClaimedLaunchWithClearedRecordDoesNotYield pins the
// disappeared-own-record case: after its record was cleared, the launch that
// still holds the dispatch claim must not yield to a record ordered after it.
func TestCeilingPrecedence_ClaimedLaunchWithClearedRecordDoesNotYield(t *testing.T) {
	svc, repo := newCeilingPrecedenceService(t, 10)
	ctx := context.Background()
	seedCeilingPrecedenceTask(t, repo, "task-tail", "session-tail", v1.TaskStateInProgress, models.TaskSessionStateWaitingForInput, time.Now())

	claim := &ceilingDeferredLaunchClaim{
		svc: svc, taskID: "task-head", id: "claim-head", owner: ceilingClaimOwnerReplay, held: true,
		deferral: models.CeilingDeferral{
			Kind: models.CeilingLaunchResume, Origin: string(launchOriginAutomatic),
			Payload: map[string]interface{}{metaKeySessionID: "session-head"}, QueuedAt: time.Now().Add(-time.Minute),
		},
	}
	decision := svc.admitCeilingLaunch(withCeilingDispatchClaim(ctx, claim), admissionRequest{
		taskID: "task-head", sessionID: "session-head", origin: launchOriginAutomatic, seam: "ensureSessionRunning",
	})
	require.True(t, decision.admitted, "a cleared own record must not make a later record precede this launch")
}

// TestCeilingPrecedence_RelaunchAndEnsureShareTheClaim reproduces the isolation
// situation the field saw: a deferral remains across a restart, and
// relaunchDynamicTaskAfterFailure and ensureSessionRunning are both consulted
// for the same claimed launch. Neither may refuse it with deferred-precedes.
func TestCeilingPrecedence_RelaunchAndEnsureShareTheClaim(t *testing.T) {
	svc, repo := newCeilingPrecedenceService(t, 10)
	ctx := context.Background()
	seedCeilingPrecedenceTask(t, repo, "task-head", "session-head", v1.TaskStateInProgress, models.TaskSessionStateWaitingForInput, time.Now().Add(-time.Minute))
	seedCeilingPrecedenceTask(t, repo, "task-tail", "session-tail", v1.TaskStateInProgress, models.TaskSessionStateWaitingForInput, time.Now())

	claim, found, err := svc.claimCeilingDeferredLaunch(ctx, "task-head", "session-head", ceilingClaimOwnerReplay)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, claim)
	dispatchCtx := withCeilingDispatchClaim(ctx, claim)

	reservation, deferred, err := svc.admitOrDeferSeam5(dispatchCtx, "task-head", "worker-profile", launchOriginAutomatic,
		map[string]interface{}{metaKeySessionID: "session-head"})
	require.NoError(t, err)
	require.False(t, deferred, "the claimed relaunch must not be deferred")
	require.NotNil(t, reservation)

	ensureReservation, refusal := svc.admitSeam3(dispatchCtx, "task-head", "session-head", "worker-profile", launchOriginAutomatic)
	require.Nil(t, refusal, "the paired ensureSessionRunning must not be refused")
	require.NotNil(t, ensureReservation)
}

// TestCeilingAdmission_DeferredPrecedesRefusalDoesNotSignalSweep pins the
// release-event-driven re-evaluation: a refusal that still had free capacity is
// not itself an admission-input change, so it must not schedule another sweep
// pass (the self-sustaining decision loop that drove the CPU spike).
func TestCeilingAdmission_DeferredPrecedesRefusalDoesNotSignalSweep(t *testing.T) {
	svc, repo := newCeilingPrecedenceService(t, 10)
	svc.ceilingSweeper = newCeilingSweeper()
	seedCeilingPrecedenceTask(t, repo, "task-head", "session-head", v1.TaskStateInProgress, models.TaskSessionStateWaitingForInput, time.Now().Add(-time.Minute))

	decision := svc.admitCeilingLaunch(context.Background(), admissionRequest{
		taskID: "new-task", sessionID: "new-session", origin: launchOriginAutomatic, seam: "startTask",
	})
	require.Equal(t, ceilingReasonDeferredPrecedes, decision.reasonCode)
	require.Empty(t, svc.ceilingSweeper.signal, "a deferred-precedes refusal must not re-signal the sweep")
}
