package sqlite

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/stretchr/testify/require"
)

func seedRecoveryRecipientSessions(t *testing.T) *Repository {
	t.Helper()
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	require.NoError(t, repo.CreateTask(ctx, &models.Task{ID: "recovery", Title: "Recovery"}))
	for _, id := range []string{"original", "replacement", "later"} {
		require.NoError(t, repo.CreateTaskSession(ctx, &models.TaskSession{
			ID: id, TaskID: "recovery", AgentProfileID: "profile-" + id,
			State: models.TaskSessionStateWaitingForInput, IsPrimary: id == "original",
		}))
	}
	return repo
}

func recoveryRecipientBinding(source, destination string) models.WorkflowInitialRecipientBinding {
	return models.WorkflowInitialRecipientBinding{
		InitialSessionID: "original", SourceSessionID: source, SessionID: destination,
		AgentProfileID: "profile-" + destination,
	}
}

func TestRecoveryRecipientLegacyProvenanceDoesNotCreateSnapshot(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		t.Run(fmt.Sprint(ambiguous), func(t *testing.T) {
			repo := seedRecoveryRecipientSessions(t)
			ctx := context.Background()
			_, err := repo.db.ExecContext(ctx, `UPDATE tasks SET metadata = '{}' WHERE id = 'recovery'`)
			require.NoError(t, err)
			if ambiguous {
				_, err := repo.db.ExecContext(ctx, `UPDATE task_sessions SET metadata = '{}', started_at = ?`, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
				require.NoError(t, err)
			}
			err = repo.SetSessionPrimaryWithInitialRecipientBinding(ctx, "replacement", recoveryRecipientBinding("original", "replacement"))
			if ambiguous {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			task, err := repo.GetTask(ctx, "recovery")
			require.NoError(t, err)
			require.NotContains(t, task.Metadata, models.MetaKeyWorkflowInitialSession)
		})
	}
}

// @covers AC-TASKS-WORKFLOW-PROFILE-SESSIONS-004.3
func TestRecoveryRecipientConcurrentAndStaleSources(t *testing.T) {
	repo := seedRecoveryRecipientSessions(t)
	ctx := context.Background()
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for _, id := range []string{"replacement", "later"} {
		group.Go(func() {
			<-start
			results <- repo.SetSessionPrimaryWithInitialRecipientBinding(ctx, id, recoveryRecipientBinding("original", id))
		})
	}
	close(start)
	group.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		}
	}
	require.Equal(t, 1, winners)
	task, err := repo.GetTask(ctx, "recovery")
	require.NoError(t, err)
	binding, err := models.LoadWorkflowInitialRecipientBinding(task.Metadata)
	require.NoError(t, err)
	winner := binding.SessionID
	require.NoError(t, repo.SetSessionPrimaryWithInitialRecipientBinding(ctx, winner, *binding))
	next := "replacement"
	if winner == next {
		next = "later"
	}
	require.NoError(t, repo.SetSessionPrimaryWithInitialRecipientBinding(ctx, next, recoveryRecipientBinding(winner, next)))
	require.Error(t, repo.SetSessionPrimaryWithInitialRecipientBinding(ctx, winner, *binding))
	primary, err := repo.GetPrimarySessionByTaskID(ctx, "recovery")
	require.NoError(t, err)
	require.Equal(t, next, primary.ID)
}

// @covers AC-TASKS-WORKFLOW-PROFILE-SESSIONS-004.4
func TestRecoveryRecipientRejectedWritesKeepPrimaryAndHistory(t *testing.T) {
	for _, name := range []string{"terminal destination", "active source", "open turn", "write failure", "foreign destination", "corrupt binding"} {
		t.Run(name, func(t *testing.T) {
			repo := seedRecoveryRecipientSessions(t)
			ctx := context.Background()
			before, err := repo.GetTask(ctx, "recovery")
			require.NoError(t, err)
			switch name {
			case "terminal destination":
				require.NoError(t, repo.UpdateTaskSessionState(ctx, "replacement", models.TaskSessionStateFailed, "failed"))
			case "active source":
				require.NoError(t, repo.UpdateTaskSessionState(ctx, "original", models.TaskSessionStateRunning, ""))
			case "open turn":
				require.NoError(t, repo.CreateTurn(ctx, &models.Turn{ID: "active", TaskID: "recovery", TaskSessionID: "original"}))
			case "write failure":
				_, err = repo.db.ExecContext(ctx, `CREATE TRIGGER reject_recovery BEFORE UPDATE OF metadata ON tasks BEGIN SELECT RAISE(ABORT, 'injected metadata failure'); END`)
				require.NoError(t, err)
			case "foreign destination":
				require.NoError(t, repo.CreateTask(ctx, &models.Task{ID: "other", Title: "Other"}))
				_, err = repo.db.ExecContext(ctx, `UPDATE task_sessions SET task_id = 'other' WHERE id = 'replacement'`)
				require.NoError(t, err)
			case "corrupt binding":
				require.NoError(t, repo.SetTaskMetadataKey(ctx, "recovery", models.MetaKeyWorkflowInitialRecipientBinding, "invalid"))
			}
			require.Error(t, repo.SetSessionPrimaryWithInitialRecipientBinding(ctx, "replacement", recoveryRecipientBinding("original", "replacement")))
			primary, err := repo.GetPrimarySessionByTaskID(ctx, "recovery")
			require.NoError(t, err)
			require.Equal(t, "original", primary.ID)
			after, err := repo.GetTask(ctx, "recovery")
			require.NoError(t, err)
			require.Equal(t, before.Metadata[models.MetaKeyWorkflowInitialSession], after.Metadata[models.MetaKeyWorkflowInitialSession])
		})
	}
}

func TestWorkflowSessionBindingRejectsStaleOperationAndRetainsProfileAfterSessionDelete(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	const (
		taskID     = "task-workflow-binding"
		workflowID = "workflow-workflow-binding"
		targetKey  = "step:implement"
	)
	require.NoError(t, repo.CreateTask(ctx, &models.Task{ID: taskID, Title: "Binding"}))
	session := &models.TaskSession{
		ID:             "workflow-binding-session",
		TaskID:         taskID,
		AgentProfileID: "profile-implement",
		State:          models.TaskSessionStateWaitingForInput,
	}
	require.NoError(t, repo.CreateTaskSession(ctx, session))

	newer := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	accepted, err := repo.UpsertWorkflowSessionBinding(ctx, &models.WorkflowSessionBinding{
		TaskID:         taskID,
		TargetKey:      targetKey,
		WorkflowID:     workflowID,
		AgentProfileID: session.AgentProfileID,
		SessionID:      session.ID,
		OperationID:    "operation-new",
		UpdatedAt:      newer,
	})
	require.NoError(t, err)
	require.True(t, accepted)

	accepted, err = repo.UpsertWorkflowSessionBinding(ctx, &models.WorkflowSessionBinding{
		TaskID:         taskID,
		TargetKey:      targetKey,
		WorkflowID:     workflowID,
		AgentProfileID: session.AgentProfileID,
		SessionID:      "stale-session",
		OperationID:    "operation-stale",
		UpdatedAt:      newer.Add(-time.Second),
	})
	require.NoError(t, err)
	require.False(t, accepted)

	binding, err := repo.GetWorkflowSessionBinding(ctx, taskID, targetKey)
	require.NoError(t, err)
	require.Equal(t, session.ID, binding.SessionID)
	require.Equal(t, "operation-new", binding.OperationID)

	require.NoError(t, repo.DeleteTaskSession(ctx, session))
	binding, err = repo.GetWorkflowSessionBinding(ctx, taskID, targetKey)
	require.NoError(t, err)
	require.NotNil(t, binding)
	require.Empty(t, binding.SessionID)
	require.Equal(t, session.AgentProfileID, binding.AgentProfileID)
}

func TestWorkflowSessionBindingRejectsEqualTimestampReplacement(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	const (
		taskID     = "task-workflow-binding-equal"
		workflowID = "workflow-workflow-binding-equal"
		targetKey  = "step:review"
	)
	require.NoError(t, repo.CreateTask(ctx, &models.Task{ID: taskID, Title: "Equal timestamp"}))
	require.NoError(t, repo.CreateTaskSession(ctx, &models.TaskSession{
		ID: "session-first", TaskID: taskID, AgentProfileID: "profile-review",
		State: models.TaskSessionStateWaitingForInput,
	}))
	stamp := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

	accepted, err := repo.UpsertWorkflowSessionBinding(ctx, &models.WorkflowSessionBinding{
		TaskID:         taskID,
		TargetKey:      targetKey,
		WorkflowID:     workflowID,
		AgentProfileID: "profile-review",
		SessionID:      "session-first",
		OperationID:    "operation-first",
		UpdatedAt:      stamp,
	})
	require.NoError(t, err)
	require.True(t, accepted)

	accepted, err = repo.UpsertWorkflowSessionBinding(ctx, &models.WorkflowSessionBinding{
		TaskID:         taskID,
		TargetKey:      targetKey,
		WorkflowID:     workflowID,
		AgentProfileID: "profile-review",
		SessionID:      "session-equal",
		OperationID:    "operation-equal",
		UpdatedAt:      stamp,
	})
	require.NoError(t, err)
	require.False(t, accepted)

	binding, err := repo.GetWorkflowSessionBinding(ctx, taskID, targetKey)
	require.NoError(t, err)
	require.Equal(t, "session-first", binding.SessionID)
	require.Equal(t, "operation-first", binding.OperationID)
}

func TestWorkflowSessionBindingAllowsNewerSessionWithinSameEntry(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	const taskID = "task-workflow-binding-same-entry"
	const targetKey = "step:implement"
	require.NoError(t, repo.CreateTask(ctx, &models.Task{ID: taskID, Title: "Same entry"}))
	first := &models.TaskSession{ID: "session-same-entry-first", TaskID: taskID, AgentProfileID: "profile-a", State: models.TaskSessionStateRunning}
	second := &models.TaskSession{ID: "session-same-entry-second", TaskID: taskID, AgentProfileID: "profile-a", State: models.TaskSessionStateRunning}
	require.NoError(t, repo.CreateTaskSession(ctx, first))
	require.NoError(t, repo.CreateTaskSession(ctx, second))

	entryOperation := "workflow-step-entry-v2:entry:00000000000000000021"
	firstStamp := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	secondStamp := firstStamp.Add(time.Second)
	accepted, err := repo.UpsertWorkflowSessionBinding(ctx, &models.WorkflowSessionBinding{
		TaskID: taskID, TargetKey: targetKey, WorkflowID: "workflow-same-entry", AgentProfileID: "profile-a",
		SessionID: first.ID, OperationID: entryOperation, UpdatedAt: firstStamp,
	})
	require.NoError(t, err)
	require.True(t, accepted)
	accepted, err = repo.UpsertWorkflowSessionBinding(ctx, &models.WorkflowSessionBinding{
		TaskID: taskID, TargetKey: targetKey, WorkflowID: "workflow-same-entry", AgentProfileID: "profile-a",
		SessionID: second.ID, OperationID: entryOperation, UpdatedAt: secondStamp,
	})
	require.NoError(t, err)
	require.True(t, accepted)

	binding, err := repo.GetWorkflowSessionBinding(ctx, taskID, targetKey)
	require.NoError(t, err)
	require.Equal(t, second.ID, binding.SessionID)
}

func TestWorkflowSessionBindingsCascadeWithTask(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	const taskID = "task-workflow-binding-cascade"
	require.NoError(t, repo.CreateTask(ctx, &models.Task{ID: taskID, Title: "Binding cascade"}))
	accepted, err := repo.UpsertWorkflowSessionBinding(ctx, &models.WorkflowSessionBinding{
		TaskID:         taskID,
		TargetKey:      "step:implement",
		WorkflowID:     "workflow-cascade",
		AgentProfileID: "profile-implement",
		OperationID:    "operation-cascade",
	})
	require.NoError(t, err)
	require.True(t, accepted)

	require.NoError(t, repo.DeleteTask(ctx, taskID))
	binding, err := repo.GetWorkflowSessionBinding(ctx, taskID, "step:implement")
	require.NoError(t, err)
	require.Nil(t, binding)
}
