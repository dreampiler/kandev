package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	"github.com/kandev/kandev/internal/workflow/engine"
	"github.com/stretchr/testify/require"
)

func TestStepSignalPreLedgerCompletesAndOldSignalCannotReenter(t *testing.T) {
	for _, path := range []string{"legacy", "engine", "subscriber"} {
		t.Run(path, func(t *testing.T) {
			ctx := context.Background()
			svc, repo := newGatedStepFailureService(t)
			task, err := repo.GetTask(ctx, "t1")
			require.NoError(t, err)
			_, err = repo.DB().ExecContext(ctx, `DELETE FROM task_step_transitions WHERE task_id = ?`, task.ID)
			require.NoError(t, err)
			old := models.PendingStepCompletionSignal{StepID: "step1", Summary: "legacy complete", SignaledAt: task.CreatedAt}
			require.NoError(t, repo.SetSessionMetadataKey(ctx, "s1", models.SessionMetaKeyPendingStepCompletion, old))
			if path != "legacy" {
				svc.initWorkflowEngine()
			}
			complete := func() {
				session, readErr := repo.GetTaskSession(ctx, "s1")
				require.NoError(t, readErr)
				switch path {
				case "legacy":
					current, taskErr := repo.GetTask(ctx, task.ID)
					require.NoError(t, taskErr)
					svc.processOnTurnComplete(ctx, current, session)
				case "engine":
					svc.processOnTurnCompleteViaEngine(ctx, task.ID, session)
				case "subscriber":
					require.NoError(t, repo.UpdateTaskSessionState(ctx, "s1", models.TaskSessionStateWaitingForInput, ""))
					svc.onStepCompletionSignaled(ctx, stepCompletionSignalEvent(task.ID, "s1", "step1"))
				}
			}
			complete()
			moved, err := repo.GetTask(ctx, task.ID)
			require.NoError(t, err)
			require.Equal(t, "step2", moved.WorkflowStepID)
			moved.WorkflowStepID = "step1"
			require.NoError(t, repo.UpdateTask(ctx, moved))
			require.NoError(t, repo.SetSessionMetadataKey(ctx, "s1", models.SessionMetaKeyPendingStepCompletion, old))
			complete()
			current, err := repo.GetTask(ctx, task.ID)
			require.NoError(t, err)
			require.Equal(t, "step1", current.WorkflowStepID, "backfill must not let an earlier signal authorize a later entry")
		})
	}
}

type stepSignalReentryRepo struct {
	*sqliterepo.Repository
	afterTaskRead func()
}

func (r *stepSignalReentryRepo) GetTask(ctx context.Context, taskID string) (*models.Task, error) {
	task, err := r.Repository.GetTask(ctx, taskID)
	if r.afterTaskRead != nil {
		callback := r.afterTaskRead
		r.afterTaskRead = nil
		callback()
	}
	return task, err
}

func TestStepSignalStaleEventPreservesReenteredSignal(t *testing.T) {
	ctx := context.Background()
	svc, repo := newGatedStepFailureService(t)
	require.NoError(t, repo.UpdateTaskSessionState(ctx, "s1", models.TaskSessionStateWaitingForInput, ""))
	task, err := repo.GetTask(ctx, "t1")
	require.NoError(t, err)
	task.WorkflowStepID = "step2"
	require.NoError(t, repo.UpdateTask(ctx, task))
	svc.repo = &stepSignalReentryRepo{Repository: repo, afterTaskRead: func() {
		task.WorkflowStepID = "step1"
		require.NoError(t, repo.UpdateTask(ctx, task))
		entryID, readErr := repo.GetLatestTaskStepTransitionID(ctx, task.ID)
		require.NoError(t, readErr)
		require.NoError(t, repo.SetSessionMetadataKey(ctx, "s1", models.SessionMetaKeyPendingStepCompletion,
			models.PendingStepCompletionSignal{StepID: "step1", EntryTransitionID: entryID, Summary: "replacement", SignaledAt: time.Now().UTC()}))
	}}
	svc.onStepCompletionSignaled(ctx, stepCompletionSignalEvent("t1", "s1", "step1"))
	session, err := repo.GetTaskSession(ctx, "s1")
	require.NoError(t, err)
	signal, ok := models.LoadPendingStepSignal(session.Metadata)
	require.True(t, ok, "a stale event must not clear the reentered step's signal")
	require.Equal(t, "replacement", signal.Summary)
}

func TestStepSignalLegacyEntryConflictLeavesNewTurnRunning(t *testing.T) {
	ctx := context.Background()
	svc, repo := newGatedStepFailureService(t)
	task, err := repo.GetTask(ctx, "t1")
	require.NoError(t, err)
	entryID, err := repo.GetLatestTaskStepTransitionID(ctx, task.ID)
	require.NoError(t, err)
	require.NoError(t, repo.SetSessionMetadataKey(ctx, "s1", models.SessionMetaKeyPendingStepCompletion,
		models.PendingStepCompletionSignal{StepID: "step1", EntryTransitionID: entryID, Summary: "old", SignaledAt: time.Now().UTC()}))
	session, err := repo.GetTaskSession(ctx, "s1")
	require.NoError(t, err)
	svc.repo = &stepSignalReentryRepo{Repository: repo, afterTaskRead: func() {
		for _, stepID := range []string{"step2", "step1"} {
			task.WorkflowStepID = stepID
			require.NoError(t, repo.UpdateTask(ctx, task))
		}
		newID, readErr := repo.GetLatestTaskStepTransitionID(ctx, task.ID)
		require.NoError(t, readErr)
		require.NoError(t, repo.SetSessionMetadataKey(ctx, "s1", models.SessionMetaKeyPendingStepCompletion,
			models.PendingStepCompletionSignal{StepID: "step1", EntryTransitionID: newID, Summary: "new turn", SignaledAt: time.Now().UTC()}))
	}}
	svc.processOnTurnComplete(ctx, task, session)
	stored, err := repo.GetTaskSession(ctx, "s1")
	require.NoError(t, err)
	require.Equal(t, models.TaskSessionStateRunning, stored.State)
	signal, ok := models.LoadPendingStepSignal(stored.Metadata)
	require.True(t, ok)
	require.Equal(t, "new turn", signal.Summary)
}

func TestStepSignalDelayedClearPreservesReplacement(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "task-entry-clear", "session-entry-clear", "step-a")
	oldID, err := repo.GetLatestTaskStepTransitionID(ctx, "task-entry-clear")
	require.NoError(t, err)
	old := models.PendingStepCompletionSignal{StepID: "step-a", EntryTransitionID: oldID, Summary: "old entry", SignaledAt: time.Now().UTC()}
	require.NoError(t, repo.SetSessionMetadataKey(ctx, "session-entry-clear", models.SessionMetaKeyPendingStepCompletion, old))
	stale, err := repo.GetTaskSession(ctx, "session-entry-clear")
	require.NoError(t, err)
	task, err := repo.GetTask(ctx, "task-entry-clear")
	require.NoError(t, err)
	for _, stepID := range []string{"step-b", "step-a"} {
		task.WorkflowStepID = stepID
		require.NoError(t, repo.UpdateTask(ctx, task))
	}
	newID, err := repo.GetLatestTaskStepTransitionID(ctx, task.ID)
	require.NoError(t, err)
	replacement := models.PendingStepCompletionSignal{StepID: "step-a", EntryTransitionID: newID, Summary: "replacement entry", SignaledAt: old.SignaledAt.Add(time.Second)}
	require.NoError(t, repo.SetSessionMetadataKey(ctx, "session-entry-clear", models.SessionMetaKeyPendingStepCompletion, replacement))
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	svc.clearPendingStepSignal(ctx, stale)
	current, err := repo.GetTaskSession(ctx, "session-entry-clear")
	require.NoError(t, err)
	signal, ok := models.LoadPendingStepSignal(current.Metadata)
	require.True(t, ok, "delayed consumption must leave the replacement signal intact")
	require.Equal(t, "replacement entry", signal.Summary)
}

func TestStepSignalFailedCommitRetainsCurrentSignal(t *testing.T) {
	ctx := context.Background()
	svc, repo := newGatedStepFailureService(t)
	entryID, err := repo.GetLatestTaskStepTransitionID(ctx, "t1")
	require.NoError(t, err)
	require.NoError(t, repo.SetSessionMetadataKey(ctx, "s1", models.SessionMetaKeyPendingStepCompletion,
		models.PendingStepCompletionSignal{StepID: "step1", EntryTransitionID: entryID, Summary: "keep me", SignaledAt: time.Now().UTC()}))
	session, err := repo.GetTaskSession(ctx, "s1")
	require.NoError(t, err)
	applied := svc.applyEngineTransitionWithCommitMode(ctx, "t1", session,
		engine.HandleResult{Transitioned: true, FromStepID: "step1", ToStepID: "step2"}, engine.TriggerOnTurnComplete,
		"", transitionLifecycleWithOnEnter, func(context.Context) (bool, error) { return false, errors.New("injected commit failure") })
	require.False(t, applied)
	stored, err := repo.GetTaskSession(ctx, "s1")
	require.NoError(t, err)
	signal, ok := models.LoadPendingStepSignal(stored.Metadata)
	require.True(t, ok)
	require.Equal(t, entryID, signal.EntryTransitionID)
	require.Equal(t, "keep me", signal.Summary)
	task, err := repo.GetTask(ctx, "t1")
	require.NoError(t, err)
	require.Equal(t, "step1", task.WorkflowStepID)
}

func TestStepSignalCarryAuditAndClearUseCapturedBag(t *testing.T) {
	ctx := context.Background()
	svc, repo := newGatedStepFailureService(t)
	svc.initWorkflowEngine()
	recorder := &fakeStepHistoryRecorder{}
	svc.stepHistoryRecorder = recorder
	entryID, err := repo.GetLatestTaskStepTransitionID(ctx, "t1")
	require.NoError(t, err)
	require.NoError(t, repo.SetSessionMetadataKey(ctx, "s1", models.SessionMetaKeyPendingStepCompletion,
		models.PendingStepCompletionSignal{StepID: "step1", EntryTransitionID: entryID, Summary: "captured", Handoff: "source handoff", SignaledAt: time.Now().UTC()}))
	session, err := repo.GetTaskSession(ctx, "s1")
	require.NoError(t, err)
	done := make(chan struct{})
	svc.onProcessOnEnterComplete = func() { close(done) }
	applied := svc.applyEngineTransitionWithCommitMode(ctx, "t1", session,
		engine.HandleResult{Transitioned: true, FromStepID: "step1", ToStepID: "step2"}, engine.TriggerOnTurnComplete,
		"", transitionLifecycleWithOnEnter, func(commitCtx context.Context) (bool, error) {
			require.NoError(t, repo.SetSessionMetadataKey(ctx, "s1", models.SessionMetaKeyPendingStepCompletion,
				models.PendingStepCompletionSignal{StepID: "step1", EntryTransitionID: entryID, Summary: "replacement", Handoff: "replacement handoff", SignaledAt: time.Now().UTC()}))
			err := svc.workflowStore.ApplyTransition(commitCtx, "t1", "s1", "step1", "step2", engine.TriggerOnTurnComplete)
			return err == nil, err
		})
	require.True(t, applied)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("destination callback did not finish")
	}
	token, present := carryToken(t, repo, "t1")
	require.True(t, present)
	require.Equal(t, "source handoff", token.Handoff)
	calls := recorder.Calls()
	require.Len(t, calls, 1)
	require.Equal(t, "captured", calls[0].metadata["signal_summary"])
	require.Equal(t, "source handoff", calls[0].metadata["signal_handoff"])
	stored, err := repo.GetTaskSession(ctx, "s1")
	require.NoError(t, err)
	remaining, ok := models.LoadPendingStepSignal(stored.Metadata)
	require.True(t, ok)
	require.Equal(t, "replacement", remaining.Summary)
}

func TestStepSignalOldEntryCannotAdvance(t *testing.T) {
	for _, path := range []string{"legacy", "engine", "subscriber"} {
		for _, current := range []bool{false, true} {
			name := path + map[bool]string{false: "/old_entry", true: "/current_entry"}[current]
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				svc, repo := newGatedStepFailureService(t)
				task, err := repo.GetTask(ctx, "t1")
				require.NoError(t, err)
				oldID, err := repo.GetLatestTaskStepTransitionID(ctx, task.ID)
				require.NoError(t, err)
				for _, stepID := range []string{"intermediate", "step1"} {
					task.WorkflowStepID = stepID
					require.NoError(t, repo.UpdateTask(ctx, task))
				}
				entryID := oldID
				if current {
					entryID, err = repo.GetLatestTaskStepTransitionID(ctx, task.ID)
					require.NoError(t, err)
				}
				require.NoError(t, repo.SetSessionMetadataKey(ctx, "s1", models.SessionMetaKeyPendingStepCompletion, models.PendingStepCompletionSignal{
					StepID: "step1", EntryTransitionID: entryID, Source: models.StepCompletionSourceAgent,
					Summary: "entry complete", SignaledAt: time.Now().UTC(),
				}))
				if path != "legacy" {
					svc.initWorkflowEngine()
					require.NotNil(t, svc.workflowEngine)
				}
				if path == "subscriber" {
					require.NoError(t, repo.UpdateTaskSessionState(ctx, "s1", models.TaskSessionStateWaitingForInput, ""))
					svc.onStepCompletionSignaled(ctx, stepCompletionSignalEvent("t1", "s1", "step1"))
				} else {
					session, getErr := repo.GetTaskSession(ctx, "s1")
					require.NoError(t, getErr)
					if path == "engine" {
						svc.processOnTurnCompleteViaEngine(ctx, task.ID, session)
					} else {
						svc.processOnTurnComplete(ctx, task, session)
					}
				}
				stored, err := repo.GetTask(ctx, task.ID)
				require.NoError(t, err)
				want := "step1"
				if current {
					want = "step2"
				}
				require.Equal(t, want, stored.WorkflowStepID)
			})
		}
	}
}
