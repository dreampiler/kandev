package sqlite

import (
	"testing"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestPostgresStepSignalEntryConcurrentClaimsAndConsumption(t *testing.T) {
	dsn := testutil.PostgresDSNFromEnv(t)
	db := testutil.OpenIsolatedPostgres(t, dsn)
	repo, err := NewWithDB(db, db, nil)
	require.NoError(t, err)
	secondDB := openSecondPostgresConnection(t, dsn, db)
	second := NewWithInitializedDB(secondDB, secondDB, nil)
	task, oldID := seedSignalEntryFixture(t, repo)
	ctx := t.Context()
	old := entrySignalValue(oldID, "old entry")
	require.NoError(t, repo.SetSessionMetadataKey(ctx, "session-entry-claim", models.SessionMetaKeyPendingStepCompletion, old))
	newID := reenterSignalStep(t, repo, task)
	type result struct {
		stored bool
		err    error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, writer := range []*Repository{repo, second} {
		go func(writer *Repository) {
			<-start
			stored, claimErr := writer.SetSessionMetadataKeyIfAbsentOrDifferentStepIfTaskAtStep(ctx, task.ID, "session-entry-claim",
				models.SessionMetaKeyPendingStepCompletion, "step-a", entrySignalValue(newID, "current entry"))
			results <- result{stored: stored, err: claimErr}
		}(writer)
	}
	close(start)
	claims := 0
	for range 2 {
		got := <-results
		require.NoError(t, got.err)
		if got.stored {
			claims++
		}
	}
	require.Equal(t, 1, claims)
	cleared, err := repo.SetSessionMetadataKeyIfJSONValue(ctx, "session-entry-claim", models.SessionMetaKeyPendingStepCompletion, old, nil)
	require.NoError(t, err)
	require.False(t, cleared)
	current, err := repo.GetTaskSession(ctx, "session-entry-claim")
	require.NoError(t, err)
	signal, ok := models.LoadPendingStepSignal(current.Metadata)
	require.True(t, ok)
	require.Equal(t, newID, signal.EntryTransitionID)
}
