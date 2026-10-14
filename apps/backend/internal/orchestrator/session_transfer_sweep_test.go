package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	"github.com/stretchr/testify/require"
)

func transferSweepFixture(t *testing.T, active string) (*Service, *sqliterepo.Repository, *sqlx.DB) {
	t.Helper()
	ctx := context.Background()
	db, _ := orchestratorTestSQLiteTemplate.Open(t)
	repo := sqliterepo.NewWithInitializedDB(db, db, nil)
	seedSession(t, repo, "task-transfer-sweep", "from", "step")
	require.NoError(t, repo.CreateTaskSession(ctx, &models.TaskSession{
		ID: "to", TaskID: "task-transfer-sweep", State: models.TaskSessionStateRunning,
		StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}))
	queueRepo, err := messagequeue.NewSQLiteRepository(db, db)
	require.NoError(t, err)
	queue := messagequeue.NewService(queueRepo, messagequeue.DefaultMaxPerSession, testLogger())
	entry, err := queue.QueueMessage(ctx, "to", "task-transfer-sweep", "handoff", "", messagequeue.QueuedByUser, false, nil)
	require.NoError(t, err)
	switch active {
	case "dispatch":
		_, reserved := queue.ReserveQueued(ctx, "to")
		require.True(t, reserved)
	case "send_now":
		_, err = queue.ClaimSendNow(ctx, "to", []messagequeue.QueuedMessage{*entry})
		require.NoError(t, err)
	}
	for _, id := range []string{"from", "to"} {
		session, getErr := repo.GetTaskSession(ctx, id)
		require.NoError(t, getErr)
		session.State = models.TaskSessionStateCompleted
		if active == "session" && id == "to" {
			session.State = models.TaskSessionStateRunning
		}
		require.NoError(t, repo.UpdateTaskSession(ctx, session))
	}
	require.NoError(t, queue.UpsertSessionTransferCompensation(ctx, messagequeue.SessionTransferCompensation{
		OperationID: "sweep-operation", TaskID: entry.TaskID,
		FromSessionID: "from", ToSessionID: "to", EntryIDs: []string{entry.ID},
	}))
	if active != "lease" {
		expireSessionTransferCompensationLease(t, db)
	}
	return &Service{repo: repo, messageQueue: queue, logger: testLogger()}, repo, db
}

func TestOrphanedSessionTransferSweepClearsTerminalFence(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := transferSweepFixture(t, "")
	_, err := svc.messageQueue.QueueMessage(ctx, "to", "task-transfer-sweep", "before", "", messagequeue.QueuedByUser, false, nil)
	require.ErrorIs(t, err, messagequeue.ErrSessionTransferInProgress)
	require.ErrorIs(t, repo.ArchiveTask(ctx, "task-transfer-sweep"), messagequeue.ErrSessionTransferInProgress)
	svc.reconcileOrphanedSessionTransfersOnce(ctx)
	remaining, err := svc.messageQueue.ListSessionTransferCompensations(ctx)
	require.NoError(t, err)
	require.Empty(t, remaining)
	_, err = svc.messageQueue.QueueMessage(ctx, "to", "task-transfer-sweep", "after", "", messagequeue.QueuedByUser, false, nil)
	require.NoError(t, err)
	require.NoError(t, repo.ArchiveTask(ctx, "task-transfer-sweep"))
	archived, err := repo.GetTask(ctx, "task-transfer-sweep")
	require.NoError(t, err)
	require.NotNil(t, archived.ArchivedAt)
}

func TestOrphanedSessionTransferSweepPreservesActiveTransfer(t *testing.T) {
	for _, active := range []string{"session", "dispatch", "send_now", "lease"} {
		t.Run(active, func(t *testing.T) {
			ctx := context.Background()
			svc, _, _ := transferSweepFixture(t, active)
			svc.reconcileOrphanedSessionTransfersOnce(ctx)
			remaining, err := svc.messageQueue.ListSessionTransferCompensations(ctx)
			require.NoError(t, err)
			require.Len(t, remaining, 1)
			_, err = svc.messageQueue.QueueMessage(ctx, "to", "task-transfer-sweep", "blocked", "", messagequeue.QueuedByUser, false, nil)
			require.ErrorIs(t, err, messagequeue.ErrSessionTransferInProgress)
		})
	}
}
