package handlers

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/stretchr/testify/require"
)

type failedCleanupAcknowledgementQueue struct {
	*messagequeue.Service
	acknowledgements atomic.Int32
}

type observedCleanupLeaseQueue struct {
	*messagequeue.Service
	checked chan struct{}
}

func (q *observedCleanupLeaseQueue) GetEditLease(ctx context.Context, sessionID, entryID string) (*messagequeue.QueueEditLease, error) {
	lease, err := q.Service.GetEditLease(ctx, sessionID, entryID)
	select {
	case q.checked <- struct{}{}:
	default:
	}
	return lease, err
}

func (q *failedCleanupAcknowledgementQueue) DeleteAttachmentCleanup(context.Context, string, string, string) error {
	q.acknowledgements.Add(1)
	return errors.New("cleanup acknowledgement unavailable")
}

func seedCleanupBudgetObligation(t *testing.T, queue *messagequeue.Service) *messagequeue.QueuedMessage {
	t.Helper()
	ctx := context.Background()
	entry, err := queue.QueueMessage(ctx, "session-budget", "task-budget", "saved edit", "", "user", false, nil)
	require.NoError(t, err)
	require.NoError(t, queue.UpsertAttachmentCleanup(ctx, messagequeue.AttachmentCleanup{
		SessionID: entry.SessionID, CurrentSessionID: entry.SessionID,
		EntryID: entry.ID, OperationID: "operation-budget", TaskID: entry.TaskID, OwnerID: "cleanup-owner",
		Attachments: []messagequeue.MessageAttachment{{AttachmentID: "superseded-attachment"}},
		CreatedAt:   time.Now().UTC(),
	}))
	return entry
}

func TestAttachmentCleanupBudgetRetainsDurableObligationAndRecovers(t *testing.T) {
	old := pendingAttachmentCleanupMaxAttempts
	pendingAttachmentCleanupMaxAttempts = 2
	t.Cleanup(func() { pendingAttachmentCleanupMaxAttempts = old })
	path := filepath.Join(t.TempDir(), "queue.db")
	h, queue, db := newPersistentCleanupQueue(t, path)
	seedCleanupBudgetObligation(t, queue)
	claimer := &controlledCleanupClaimer{}
	claimer.failures.Store(10)
	h.SetAttachmentClaimer(claimer)
	h.Start(context.Background())
	h.attachmentCleanupWG.Wait()
	require.Equal(t, int32(2), claimer.attempts.Load())
	require.Empty(t, h.pendingAttachmentCleanup)
	rows, err := queue.ListAttachmentCleanups(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	h.Stop()
	require.NoError(t, db.Close())

	restarted, recoveredQueue, recoveredDB := newPersistentCleanupQueue(t, path)
	t.Cleanup(func() { _ = recoveredDB.Close() })
	recovered := &controlledCleanupClaimer{}
	restarted.SetAttachmentClaimer(recovered)
	restarted.Start(context.Background())
	t.Cleanup(restarted.Stop)
	restarted.attachmentCleanupWG.Wait()
	require.Equal(t, int32(1), recovered.released.Load())
	require.Equal(t, "cleanup-owner", recovered.lastUser.Load())
	rows, err = recoveredQueue.ListAttachmentCleanups(context.Background())
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestAttachmentCleanupAcknowledgementsConsumeBudget(t *testing.T) {
	old := pendingAttachmentCleanupMaxAttempts
	pendingAttachmentCleanupMaxAttempts = 2
	t.Cleanup(func() { pendingAttachmentCleanupMaxAttempts = old })
	h, queue, db := newPersistentCleanupQueue(t, filepath.Join(t.TempDir(), "queue.db"))
	t.Cleanup(func() { _ = db.Close() })
	seedCleanupBudgetObligation(t, queue)
	failed := &failedCleanupAcknowledgementQueue{Service: queue}
	h.queueService = failed
	claimer := &controlledCleanupClaimer{}
	h.SetAttachmentClaimer(claimer)
	h.Start(context.Background())
	t.Cleanup(h.Stop)
	h.attachmentCleanupWG.Wait()
	require.Equal(t, int32(2), failed.acknowledgements.Load())
	require.Empty(t, h.pendingAttachmentCleanup)
	rows, err := queue.ListAttachmentCleanups(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestAttachmentCleanupEditLeaseDoesNotConsumeBudget(t *testing.T) {
	old := pendingAttachmentCleanupMaxAttempts
	pendingAttachmentCleanupMaxAttempts = 1
	t.Cleanup(func() { pendingAttachmentCleanupMaxAttempts = old })
	h, queue, db := newPersistentCleanupQueue(t, filepath.Join(t.TempDir(), "queue.db"))
	t.Cleanup(func() { _ = db.Close() })
	entry := seedCleanupBudgetObligation(t, queue)
	lease, err := queue.BeginEdit(context.Background(), entry.SessionID, entry.ID, "editing-connection")
	require.NoError(t, err)
	claimer := &controlledCleanupClaimer{}
	h.SetAttachmentClaimer(claimer)
	observed := &observedCleanupLeaseQueue{Service: queue, checked: make(chan struct{}, 16)}
	h.queueService = observed
	h.Start(context.Background())
	t.Cleanup(h.Stop)
	<-observed.checked
	// Observe each real lease read before waking the next retry iteration.
	for range 3 {
		h.signalPendingAttachmentCleanup(context.Background(), entry.SessionID, entry.ID)
		<-observed.checked
	}
	require.Zero(t, claimer.attempts.Load())
	h.attachmentCleanupMu.Lock()
	require.Len(t, h.pendingAttachmentCleanup, 1)
	h.attachmentCleanupMu.Unlock()
	require.NoError(t, queue.EndEdit(context.Background(), entry.SessionID, entry.ID, lease.LeaseID, "editing-connection"))
	h.signalPendingAttachmentCleanup(context.Background(), entry.SessionID, entry.ID)
	h.attachmentCleanupWG.Wait()
	require.Equal(t, int32(1), claimer.released.Load())
	rows, err := queue.ListAttachmentCleanups(context.Background())
	require.NoError(t, err)
	require.Empty(t, rows)
}
