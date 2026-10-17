package orchestrator

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/jmoiron/sqlx"
	eventbus "github.com/kandev/kandev/internal/events/bus"
	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/stretchr/testify/require"
)

type admissionRecoveryBarrierRepository struct {
	messagequeue.Repository
	attempts atomic.Int32
	entered  chan struct{}
	release  chan struct{}
}

func (r *admissionRecoveryBarrierRepository) RequeuePreservingFIFOForSession(
	ctx context.Context, identity messagequeue.QueueSessionIdentity, msg *messagequeue.QueuedMessage,
) error {
	if r.attempts.Add(1) == 1 {
		return messagequeue.ErrSessionAdmissionTimeout
	}
	close(r.entered)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.release:
		return r.Repository.RequeuePreservingFIFOForSession(ctx, identity, msg)
	}
}

func TestQueuedAdmissionRecoveryRetainsSQLiteReservationUntilCompensation(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "requeue", true: "service-stop"}[stop], func(t *testing.T) {
			ctx := context.Background()
			repo := setupTestRepo(t)
			seedTaskAndSession(t, repo, "t1", "s1", models.TaskSessionStateWaitingForInput)
			seedExecutorRunning(t, repo, "s1", "t1", "exec-1")
			manager := &mockAgentManager{isAgentRunning: true, promptErr: messagequeue.ErrSessionAdmissionTimeout, repoForExecutionLookup: repo}
			log := testLogger()
			bus := eventbus.NewMemoryEventBus(log)
			t.Cleanup(bus.Close)
			svc := NewService(DefaultServiceConfig(), bus, manager, newMockTaskRepo(), repo, nil, nil, nil, log)
			svc.SetTurnService(&repoTurnService{repo: repo})
			queueDB := sqlx.NewDb(repo.DB(), "sqlite3")
			sqlQueue, err := messagequeue.NewSQLiteRepository(queueDB, queueDB)
			require.NoError(t, err)
			barrier := &admissionRecoveryBarrierRepository{Repository: sqlQueue, entered: make(chan struct{}), release: make(chan struct{})}
			svc.messageQueue = messagequeue.NewService(barrier, messagequeue.DefaultMaxPerSession, log)
			svc.messageQueue.SetAutoMergeEnabled(false)
			require.NoError(t, svc.Start(ctx))
			defer func() {
				if svc.running {
					require.NoError(t, svc.Stop())
				}
			}()
			identity, err := svc.messageQueue.ResolveSessionIdentity(ctx, "t1", "s1")
			require.NoError(t, err)
			queued, err := svc.messageQueue.QueueMessageWithMetadataForSession(ctx, identity, "keep exact payload", "", messagequeue.QueuedByUser, false, nil, map[string]interface{}{"evidence": "preserve"})
			require.NoError(t, err)
			taken, ok, _, err := svc.messageQueue.ReserveQueuedForDeliveryWithAutoRunForSession(ctx, identity)
			require.NoError(t, err)
			require.True(t, ok)
			reservation := svc.markQueuedDispatchInFlightWithIdentityLocked(identity, taken.ID, taken)
			done := make(chan struct{})
			go func() { svc.executeQueuedMessageWithReservation("s1", taken, reservation); close(done) }()
			<-barrier.entered
			select {
			case <-done:
				t.Fatal("worker released its reservation before compensation settled")
			default:
			}
			rows, err := sqlQueue.ListBySession(ctx, "s1")
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.True(t, rows[0].IsReservedInFlight())
			require.Equal(t, queued.ID, rows[0].ID)
			if stop {
				require.NoError(t, svc.Stop())
			} else {
				close(barrier.release)
			}
			<-done
			rows, err = sqlQueue.ListBySession(ctx, "s1")
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, queued.ID, rows[0].ID)
			require.Equal(t, queued.Content, rows[0].Content)
			require.Equal(t, "preserve", rows[0].Metadata["evidence"])
			require.Equal(t, stop, rows[0].IsReservedInFlight())
			require.Equal(t, int32(2), barrier.attempts.Load())
		})
	}
}
