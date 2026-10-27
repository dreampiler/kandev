package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	"github.com/stretchr/testify/require"
)

type continuationUnreadableQueue struct{ messagequeue.Repository }

func (continuationUnreadableQueue) ListBySession(context.Context, string) ([]messagequeue.QueuedMessage, error) {
	return nil, errors.New("queue read unavailable")
}

func TestInterruptionContinuationOwnershipQueuedHumanWorkTakesPriority(t *testing.T) {
	svc, _, data := continuationFailureFixture(t)
	mgr := installContinuationRestoreFixture(t, svc)
	svc.messageQueue = newAuthoritativeMemoryQueue(svc.repo.(*sqliterepo.Repository), testLogger())
	require.True(t, svc.handleTransientFailure(context.Background(), data))
	queued, err := svc.messageQueue.QueueMessage(context.Background(), "s1", "t1", "new human request", "", "", false, nil)
	require.NoError(t, err)
	value, _ := svc.transientRetries.Load("s1")
	entry := value.(*transientRetryEntry)
	require.True(t, entry.claim())
	svc.retryTransientPrompt(entry.retryCtx, "t1", "s1", "execution-1")
	require.Empty(t, mgr.capturedPrompts, "automatic continuation cannot jump ahead of human work")
	require.Equal(t, 1, svc.messageQueue.GetStatus(context.Background(), "s1").Count)
	require.Equal(t, queued.ID, svc.messageQueue.GetStatus(context.Background(), "s1").Entries[0].ID)
	_, owned := svc.transientRetries.Load("s1")
	require.False(t, owned)
}

func TestInterruptionContinuationOwnershipUnknownQueueRefusesDispatch(t *testing.T) {
	svc, _, data := continuationFailureFixture(t)
	require.True(t, svc.handleTransientFailure(context.Background(), data))
	value, _ := svc.transientRetries.Load("s1")
	svc.messageQueue = messagequeue.NewService(continuationUnreadableQueue{Repository: messagequeue.NewMemoryRepository()}, messagequeue.DefaultMaxPerSession, testLogger())
	require.Error(t, svc.validateContinuationOwner(context.Background(), "t1", "s1", value.(*transientRetryEntry)), "a failed queue read cannot attest that no human work is waiting")
}

func TestDynamicContinuationExcludesTranscriptRecordedAfterReservation(t *testing.T) {
	for _, restore := range []bool{false, true} {
		t.Run(map[bool]string{false: "reserved", true: "restored-from-stale-claim"}[restore], func(t *testing.T) {
			ctx := context.Background()
			repo := setupTestRepo(t)
			seedSession(t, repo, "late-task", "late-session", "step-1")
			queue, db := newWorkflowTransferQueue(t, t.TempDir()+"/queue.db")
			t.Cleanup(func() { _ = db.Close() })
			queued, err := queue.QueueMessage(ctx, "late-session", "late-task", "recorded after reservation", "", "", false, nil)
			require.NoError(t, err)
			reserved, ok := queue.ReserveQueued(ctx, queued.SessionID)
			require.True(t, ok)
			now := time.Now().UTC()
			require.NoError(t, repo.CreateTurn(ctx, &models.Turn{ID: "late-turn", TaskID: queued.TaskID, TaskSessionID: queued.SessionID, StartedAt: now}))
			svc := &Service{
				repo: repo, messageQueue: queue, logger: testLogger(),
				messageCreator: &repositoryBackedMessageCreator{mockMessageCreator: &mockMessageCreator{}, repo: repo},
			}
			svc.activeTurns.Store(queued.SessionID, "late-turn")
			require.NoError(t, svc.recordQueuedUserMessage(ctx, reserved, nil))
			require.Equal(t, true, reserved.Metadata[metaKeyUserMessageRecorded])
			claims, err := queue.ListPendingQueueDispatches(ctx)
			require.NoError(t, err)
			require.Len(t, claims, 1)
			require.NotEqual(t, true, claims[0].Message.Metadata[metaKeyUserMessageRecorded], "the durable claim predates transcript recording")
			if restore {
				_, err = queue.RestoreMessage(ctx, &claims[0].Message)
				require.NoError(t, err)
			}
			before, _, err := queue.SnapshotSession(ctx, queued.SessionID)
			require.NoError(t, err)
			claimsBefore, err := queue.ListPendingQueueDispatches(ctx)
			require.NoError(t, err)
			input := &dynamicruntime.ContinuationInput{}
			require.NoError(t, svc.addDynamicConversation(ctx, queued.SessionID, input))
			require.Empty(t, input.UserMessages, "the real writer's row must remain owned by the queued delivery")
			_, ok = svc.dynamicRelaunchPrompt(ctx, queued.SessionID)
			require.False(t, ok, "a queue-only transcript cannot become a recovery prompt")
			after, _, err := queue.SnapshotSession(ctx, queued.SessionID)
			require.NoError(t, err)
			require.Equal(t, before, after)
			claimsAfter, err := queue.ListPendingQueueDispatches(ctx)
			require.NoError(t, err)
			require.Equal(t, claimsBefore, claimsAfter)
		})
	}
}
