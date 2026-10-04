package handlers

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/auth/authn"
	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
)

// A cleanup obligation that keeps failing must stop re-entering the session queue
// admission; otherwise every other queue operation on that session (remove,
// auto-run, the post-cancel auto-run pause) waits behind a retry loop. The
// durable cleanup row survives for the next recovery pass.
func TestPendingAttachmentCleanupRetryStopsAfterAttemptBudget(t *testing.T) {
	previous := pendingAttachmentCleanupMaxAttempts
	pendingAttachmentCleanupMaxAttempts = 3
	t.Cleanup(func() { pendingAttachmentCleanupMaxAttempts = previous })

	handlers, queue, db := newPersistentCleanupQueue(t, filepath.Join(t.TempDir(), "queue.db"))
	t.Cleanup(func() { _ = db.Close() })
	// This cleanup settles by releasing the superseded attachment, so the
	// release path is the one that must keep failing.
	failing := &controlledCleanupClaimer{releaseErr: errors.New("release unavailable")}
	handlers.SetAttachmentClaimer(failing)
	ctx := authn.WithIdentity(context.Background(), authn.Identity{UserID: "user-attempt-budget"})
	handlers.Start(ctx)

	entry, err := queue.QueueMessage(
		ctx, "session-attempt-budget", "task-attempt-budget", "before", "", messagequeue.QueuedByUser, false,
		[]messagequeue.MessageAttachment{{Type: "resource", AttachmentID: "old", Name: "old.txt", MimeType: "text/plain"}},
	)
	require.NoError(t, err)

	pending, err := handlers.preparePendingAttachmentCleanupWithState(
		ctx,
		wsUpdateMessageRequest{
			SessionID: entry.SessionID, EntryID: entry.ID, OperationID: "operation-attempt-budget",
		},
		"task-attempt-budget",
		[]messagequeue.MessageAttachment{{Type: "resource", AttachmentID: "old", Name: "old.txt", MimeType: "text/plain"}},
		failing,
		true,
		entry,
	)
	require.NoError(t, err)
	require.NotNil(t, pending)
	handlers.queuePendingAttachmentCleanup(pending)

	// The retry loop must stop re-entering the session admission once the budget
	// is spent, so the attempt count settles at the budget.
	require.Eventually(t, func() bool {
		return failing.attempts.Load() >= int32(pendingAttachmentCleanupMaxAttempts)
	}, 10*time.Second, 10*time.Millisecond, "cleanup retry loop never reached its attempt budget")
	settled := failing.attempts.Load()
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, settled, failing.attempts.Load(),
		"exhausted cleanup obligation must stop re-entering the session admission")

	handlers.attachmentCleanupMu.Lock()
	_, queued := handlers.pendingAttachmentCleanup[pending.key]
	handlers.attachmentCleanupMu.Unlock()
	require.False(t, queued, "exhausted cleanup obligation must not keep occupying the admission retry loop")

	cleanups, err := queue.ListAttachmentCleanups(ctx)
	require.NoError(t, err)
	require.Len(t, cleanups, 1, "durable cleanup row must remain for the next recovery pass")

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		handlers.Stop()
	}()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("handler stop blocked behind an unbounded cleanup retry")
	}
}
