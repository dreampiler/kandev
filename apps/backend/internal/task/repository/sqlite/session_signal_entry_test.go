package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/stretchr/testify/require"
)

func TestStepSignalPreLedgerReadersBackfillOneEntry(t *testing.T) {
	repo, task, _ := signalEntryFixture(t)
	assertPreLedgerReadersBackfillOneEntry(t, repo, repo, task)
}

func assertPreLedgerReadersBackfillOneEntry(t *testing.T, repo, secondReader *Repository, task *models.Task) {
	t.Helper()
	ctx := t.Context()
	_, err := repo.db.ExecContext(ctx, repo.db.Rebind(`DELETE FROM task_step_transitions WHERE task_id = ?`), task.ID)
	require.NoError(t, err)
	type result struct {
		entry models.StepCompletionEntry
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, reader := range []*Repository{repo, secondReader} {
		go func() {
			<-start
			entry, readErr := reader.GetTaskStepCompletionEntry(ctx, task.ID)
			results <- result{entry: entry, err: readErr}
		}()
	}
	close(start)
	first := <-results
	second := <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.Positive(t, first.entry.TransitionID)
	require.Equal(t, first.entry.TransitionID, second.entry.TransitionID)
	require.True(t, first.entry.OccurredAt.Equal(task.CreatedAt), "an untracked entry must not exclude work that predates its backfill")
	id, err := repo.EnsureCurrentTaskStepTransition(ctx, task.ID)
	require.NoError(t, err, "rechecking an existing backfill must not commit twice")
	require.Equal(t, first.entry.TransitionID, id)
	rows, err := repo.ListTaskStepTransitions(ctx, task.ID, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestStepSignalPreLedgerEnsureExistingEntryIsIdempotent(t *testing.T) {
	repo, task, entryID := signalEntryFixture(t)
	id, err := repo.EnsureCurrentTaskStepTransition(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, entryID, id)
}

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
