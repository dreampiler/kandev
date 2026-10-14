package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/stretchr/testify/require"
)

func signalEntryFixture(t *testing.T) (*Repository, *models.Task, int64) {
	t.Helper()
	repo := newRepoForSessionTests(t)
	task, entryID := seedSignalEntryFixture(t, repo)
	return repo, task, entryID
}

func seedSignalEntryFixture(t *testing.T, repo *Repository) (*models.Task, int64) {
	t.Helper()
	ctx := context.Background()
	seedForMsgTest(t, repo, "task-entry-claim", "session-entry-claim", "turn-entry-claim")
	task, err := repo.GetTask(ctx, "task-entry-claim")
	require.NoError(t, err)
	require.NoError(t, repo.CreateWorkflow(ctx, &models.Workflow{
		ID: "workflow-entry-claim", WorkspaceID: task.WorkspaceID, Name: "Entry claim",
	}))
	task.WorkflowID = "workflow-entry-claim"
	task.WorkflowStepID = "step-a"
	require.NoError(t, repo.UpdateTask(ctx, task))
	entryID, err := repo.GetLatestTaskStepTransitionID(ctx, task.ID)
	require.NoError(t, err)
	require.Positive(t, entryID)
	return task, entryID
}

func entrySignalValue(entryID int64, summary string) map[string]interface{} {
	return map[string]interface{}{
		"step_id": "step-a", "entry_transition_id": entryID,
		"source": models.StepCompletionSourceAgent, "summary": summary,
		"signaled_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func reenterSignalStep(t *testing.T, repo *Repository, task *models.Task) int64 {
	t.Helper()
	for _, stepID := range []string{"step-b", "step-a"} {
		task.WorkflowStepID = stepID
		require.NoError(t, repo.UpdateTask(context.Background(), task))
	}
	entryID, err := repo.GetLatestTaskStepTransitionID(context.Background(), task.ID)
	require.NoError(t, err)
	return entryID
}

func TestStepSignalFreshEntryReplacesSameStepBag(t *testing.T) {
	ctx := context.Background()
	repo, task, oldEntry := signalEntryFixture(t)
	require.NoError(t, repo.SetSessionMetadataKey(ctx, "session-entry-claim", models.SessionMetaKeyPendingStepCompletion, entrySignalValue(oldEntry, "old entry")))
	newEntry := reenterSignalStep(t, repo, task)
	require.Greater(t, newEntry, oldEntry)
	stored, err := repo.SetSessionMetadataKeyIfAbsentOrDifferentStepIfTaskAtStep(
		ctx, task.ID, "session-entry-claim", models.SessionMetaKeyPendingStepCompletion, "step-a", entrySignalValue(newEntry, "new entry"),
	)
	require.NoError(t, err)
	require.True(t, stored, "a fresh entry must replace the earlier entry's same-step signal")
	session, err := repo.GetTaskSession(ctx, "session-entry-claim")
	require.NoError(t, err)
	bag, ok := models.LoadPendingStepSignal(session.Metadata)
	require.True(t, ok)
	require.Equal(t, "new entry", bag.Summary)
}

func TestStepSignalRejectsClaimAfterSameStepABA(t *testing.T) {
	ctx := context.Background()
	repo, task, oldEntry := signalEntryFixture(t)
	newEntry := reenterSignalStep(t, repo, task)
	require.Greater(t, newEntry, oldEntry)
	stored, err := repo.SetSessionMetadataKeyIfAbsentOrDifferentStepIfTaskAtStep(
		ctx, task.ID, "session-entry-claim", models.SessionMetaKeyPendingStepCompletion, "step-a", entrySignalValue(oldEntry, "stale caller"),
	)
	require.Error(t, err, "entry drift must be distinguishable from a duplicate")
	require.False(t, stored)
}

func TestStepSignalTurnWritersStampServerEntry(t *testing.T) {
	for _, receipt := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "receipt"}[receipt], func(t *testing.T) {
			ctx := context.Background()
			repo, task, entryID := signalEntryFixture(t)
			turn := &models.Turn{
				ID: "entry-stamped-turn", TaskID: task.ID, TaskSessionID: "session-entry-claim",
				Metadata: map[string]interface{}{models.TurnMetaKeyWorkflowStepEntryAtStart: int64(99999)},
			}
			if receipt {
				stamped, _, err := repo.CreateTurnWithStepStampConversationReceipt(ctx, turn)
				require.NoError(t, err)
				require.True(t, stamped)
			} else {
				stamped, err := repo.CreateTurnWithStepStamp(ctx, turn)
				require.NoError(t, err)
				require.True(t, stamped)
			}
			stored, err := repo.GetTurn(ctx, turn.ID)
			require.NoError(t, err)
			require.Equal(t, entryID, models.StepCompletionEntryIDFromAny(stored.Metadata[models.TurnMetaKeyWorkflowStepEntryAtStart]))
		})
	}
}

func TestStepSignalClaimDoesNotGuessUndatedLegacyEntry(t *testing.T) {
	ctx := context.Background()
	repo, task, entryID := signalEntryFixture(t)
	require.NoError(t, repo.SetSessionMetadataKey(ctx, "session-entry-claim", models.SessionMetaKeyPendingStepCompletion,
		models.PendingStepCompletionSignal{StepID: "step-a", Summary: "undated legacy"}))
	stored, err := repo.SetSessionMetadataKeyIfAbsentOrDifferentStepIfTaskAtStep(
		ctx, task.ID, "session-entry-claim", models.SessionMetaKeyPendingStepCompletion, "step-a", entrySignalValue(entryID, "replacement"),
	)
	require.ErrorIs(t, err, models.ErrStepCompletionEntryUnknown)
	require.False(t, stored)
	session, err := repo.GetTaskSession(ctx, "session-entry-claim")
	require.NoError(t, err)
	bag, ok := models.LoadPendingStepSignal(session.Metadata)
	require.True(t, ok)
	require.Equal(t, "undated legacy", bag.Summary)
}

func TestStepSignalConsumingMoveRejectsSourceEntryABA(t *testing.T) {
	repo, task, oldID := signalEntryFixture(t)
	ctx := models.WithStepCompletionEntry(context.Background(), task.ID, models.StepCompletionEntry{StepID: "step-a", TransitionID: oldID})
	newID := reenterSignalStep(t, repo, task)
	require.Greater(t, newID, oldID)
	task.WorkflowStepID = "step-b"
	applied, err := repo.UpdateTaskWithWorkflowStepAdmissionIfAtStep(ctx, task, "step-a", "step-b", 0)
	require.NoError(t, err)
	require.False(t, applied, "a signal-consuming move must retain its source entry")
	stored, err := repo.GetTask(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, "step-a", stored.WorkflowStepID)
}
