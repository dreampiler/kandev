package messagequeue

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type expireTransferAtCompletionRepository struct{ *sqliteRepository }

func (r *expireTransferAtCompletionRepository) DeleteSessionTransferCompensation(
	ctx context.Context, operationID, taskID, fromID, toID string,
) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE queue_session_transfer_compensations
		SET recovery_lease_expires_at = ? WHERE operation_id = ?`,
		time.Now().UTC().Add(-time.Second), operationID); err != nil {
		return err
	}
	return r.sqliteRepository.DeleteSessionTransferCompensation(ctx, operationID, taskID, fromID, toID)
}

func TestCommittedSessionTransferClearsExpiredOwnFence(t *testing.T) {
	ctx := context.Background()
	repo := &expireTransferAtCompletionRepository{newTestSQLiteRepo(t).(*sqliteRepository)}
	svc := newAutoMergeTestServiceWithRepository(t, repo, DefaultMaxPerSession)
	entry, err := svc.QueueMessage(ctx, "old", "task", "handoff", "", QueuedByUser, false, nil)
	require.NoError(t, err)
	require.NoError(t, svc.TransferSessionWithDurablePreparation(ctx, "task", "old", "new", nil, nil))
	stored, err := svc.ListSessionTransferCompensations(ctx)
	require.NoError(t, err)
	require.Empty(t, stored)
	moved, err := svc.FindEntryByID(ctx, entry.ID)
	require.NoError(t, err)
	require.Equal(t, "new", moved.SessionID)
	_, err = svc.QueueMessage(ctx, "new", "task", "after", "", QueuedByUser, false, nil)
	require.NoError(t, err)
}

func TestExpiredSessionTransferOriginalOwnerCannotDeleteRecoveryFence(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLiteRepo(t).(*sqliteRepository)
	c := SessionTransferCompensation{OperationID: "operation", TaskID: "task", FromSessionID: "old", ToSessionID: "new"}
	require.NoError(t, repo.UpsertSessionTransferCompensation(ctx, c))
	_, err := repo.db.Exec(`UPDATE queue_session_transfer_compensations
		SET recovery_owner = 'recovery-owner', recovery_lease_expires_at = ?`, time.Now().UTC().Add(-time.Second))
	require.NoError(t, err)
	require.ErrorIs(t, repo.DeleteSessionTransferCompensation(ctx, c.OperationID, c.TaskID, c.FromSessionID, c.ToSessionID), ErrSessionTransferOwnershipLost)
	stored, err := repo.ListSessionTransferCompensations(ctx)
	require.NoError(t, err)
	require.Len(t, stored, 1)
}
