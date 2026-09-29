package automation

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// A launch refused at the session ceiling is not a dispatch failure: the task
// already owns the run, so DispatchRun must leave the row open at task_created
// (not failed) and return the distinguishable ErrRunDeferred so the
// orchestrator skips task cleanup. The eventual turn settles the run by task.
func TestDispatchRunKeepsDeferredRunOpenAndSettleable(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	a := &Automation{WorkspaceID: "ws-1", Name: "deferred", Enabled: true, MaxConcurrentRuns: 1}
	require.NoError(t, svc.store.CreateAutomation(ctx, a))

	run := &AutomationRun{AutomationID: a.ID, TriggerType: TriggerTypeScheduled, Status: RunStatusTriggered}
	require.NoError(t, svc.store.CreateRun(ctx, run))
	// The orchestrator creates and binds the task before dispatch.
	require.NoError(t, svc.store.BindRunTask(ctx, run.ID, "task-1", ""))

	err := svc.DispatchRun(ctx, run.ID, ThreadActionCreated, "created", func() (RunDispatch, error) {
		return RunDispatch{}, ErrRunDeferred
	})
	require.ErrorIs(t, err, ErrRunDeferred)

	got, err := svc.store.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusTaskCreated, got.Status,
		"a deferred launch must be left open, not marked failed")
	require.Equal(t, "task-1", got.TaskID)

	require.NoError(t, svc.store.MarkRunSucceededByTaskID(ctx, "task-1"))
	got, err = svc.store.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusSucceeded, got.Status,
		"the sweep's eventual turn must still be able to settle the deferred run")
}

// An ordinary dispatch error must still fail the run, so the deferral branch
// cannot swallow real failures.
func TestDispatchRunStillFailsOnAnOrdinaryDispatchError(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	a := &Automation{WorkspaceID: "ws-1", Name: "hard-fail", Enabled: true, MaxConcurrentRuns: 1}
	require.NoError(t, svc.store.CreateAutomation(ctx, a))

	run := &AutomationRun{AutomationID: a.ID, TriggerType: TriggerTypeScheduled, Status: RunStatusTriggered}
	require.NoError(t, svc.store.CreateRun(ctx, run))

	boom := errors.New("executor unavailable")
	err := svc.DispatchRun(ctx, run.ID, ThreadActionCreated, "created", func() (RunDispatch, error) {
		return RunDispatch{}, boom
	})
	require.ErrorIs(t, err, boom)

	got, err := svc.store.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusFailed, got.Status)
}
