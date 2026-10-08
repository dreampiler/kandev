package orchestrator

import (
	"context"
	"github.com/kandev/kandev/internal/events"
	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestRollbackPromptClaimRestoresUnsentMissingOwnTurn(t *testing.T) {
	ctx := context.Background()
	const tid, sid = "missing-own-turn-task", "missing-own-turn-session"
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, tid, sid, models.TaskSessionStateWaitingForInput)
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	svc.turnService = &repoTurnService{repo: repo}
	session, err := repo.GetTaskSession(ctx, sid)
	require.NoError(t, err)
	identity := messagequeue.QueueSessionIdentity{TaskID: tid, SessionID: sid, SessionIncarnationID: session.QueueIncarnationID}
	_, rollback, err := svc.claimPromptDispatch(ctx, tid, sid, "", false, false, nil, nil, nil, "", false, &identity)
	require.NoError(t, err)
	require.NotEmpty(t, rollback.turnID)
	// The real repository orphan cleanup closes only the claimed turn. No provider
	// has accepted this dispatch and no successor state write has occurred.
	require.NoError(t, repo.AbandonTurn(ctx, rollback.turnID))
	eventBus := &recordingEventBus{}
	svc.eventBus = eventBus
	svc.rollbackPromptClaim(ctx, tid, sid, rollback)
	require.Len(t, eventBus.events, 1)
	require.Equal(t, events.TaskSessionStateChanged, eventBus.events[0].subject)
	payload := eventBus.events[0].event.Data.(map[string]interface{})
	require.Equal(t, string(models.TaskSessionStateWaitingForInput), payload["new_state"])
	persisted, err := repo.GetTaskSession(ctx, sid)
	require.NoError(t, err)
	require.Equal(t, models.TaskSessionStateWaitingForInput, persisted.State, "lost own turn must not strand an unsent, still-owned RUNNING claim")
}

type missingTurnCASBarrier struct {
	*sqliterepo.Repository
	beforeCAS func()
	changed   bool
}

func (r *missingTurnCASBarrier) UpdateTaskSessionIfCurrentSnapshot(ctx context.Context, session *models.TaskSession, expected models.TaskSessionState, revision time.Time, metadata map[string]interface{}) (bool, error) {
	r.beforeCAS()
	changed, err := r.Repository.UpdateTaskSessionIfCurrentSnapshot(ctx, session, expected, revision, metadata)
	r.changed = changed
	return changed, err
}

func TestRollbackMissingOwnTurnPreservesReplacementOwner(t *testing.T) {
	for _, scenario := range []string{"accepted_provider", "same_state_progress", "new_state", "new_incarnation", "successor_turn", "new_execution", "same_tick_error_progress", "new_cached_turn", "CAS_zero_rows"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			const tid, sid = "replacement-task", "replacement-session"
			repo := setupTestRepo(t)
			seedTaskAndSession(t, repo, tid, sid, models.TaskSessionStateWaitingForInput)
			svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
			svc.turnService = &repoTurnService{repo: repo}
			initial, err := repo.GetTaskSession(ctx, sid)
			require.NoError(t, err)
			identity := messagequeue.QueueSessionIdentity{TaskID: tid, SessionID: sid, SessionIncarnationID: initial.QueueIncarnationID}
			_, rollback, err := svc.claimPromptDispatch(ctx, tid, sid, "", false, false, nil, nil, nil, "", false, &identity)
			require.NoError(t, err)
			require.NoError(t, repo.AbandonTurn(ctx, rollback.turnID))
			expected := models.TaskSessionStateRunning
			var successorID string
			var barrier *missingTurnCASBarrier
			switch scenario {
			case "same_state_progress":
				require.NoError(t, repo.UpdateTaskSessionState(ctx, sid, models.TaskSessionStateRunning, "new provider activity"))
				// Give the competing writer an explicit distinct revision. Windows clocks
				// can return the same tick for adjacent writes; this case tests revision CAS.
				_, err = repo.DB().ExecContext(ctx, "UPDATE task_sessions SET updated_at = ? WHERE id = ?", rollback.claimedSessionUpdatedAt.Add(time.Second), sid)
				require.NoError(t, err)
			case "new_state":
				expected = models.TaskSessionStateCancelled
				require.NoError(t, repo.UpdateTaskSessionState(ctx, sid, expected, "owner cancelled"))
			case "new_incarnation":
				_, err = repo.DB().ExecContext(ctx, "UPDATE task_sessions SET queue_incarnation_id = ? WHERE id = ?", "replacement-incarnation", sid)
				require.NoError(t, err)
			case "successor_turn":
				successor, err := svc.turnService.StartTurn(ctx, sid)
				require.NoError(t, err)
				successorID = successor.ID
			case "same_tick_error_progress":
				_, err = repo.DB().ExecContext(ctx, "UPDATE task_sessions SET error_message = ? WHERE id = ?", "replacement error", sid)
				require.NoError(t, err)
			case "new_cached_turn":
				svc.activeTurns.Store(sid, "replacement-live-turn")
			case "new_execution":
				seedExecutorRunning(t, repo, sid, tid, "replacement-execution")
			case "CAS_zero_rows":
				barrier = &missingTurnCASBarrier{Repository: repo, beforeCAS: func() {
					require.NoError(t, repo.UpdateTaskSessionState(ctx, sid, models.TaskSessionStateRunning, "concurrent CAS winner"))
					_, err := repo.DB().ExecContext(ctx, "UPDATE task_sessions SET updated_at = ? WHERE id = ?", rollback.claimedSessionUpdatedAt.Add(time.Second), sid)
					require.NoError(t, err)
				}}
				svc.repo = barrier
			}
			before, err := repo.GetTaskSession(ctx, sid)
			require.NoError(t, err)
			if scenario == "accepted_provider" {
				// Exercise the real accepted-result boundary, not an independently set
				// rollback flag. A missing DB turn does not invalidate accepted work.
				_, err = svc.handlePromptDispatchFailure(ctx, tid, sid, "accepted long task", false, false, nil, rollback, true, true, errPromptAdmissionRejected, false, "", "", "", false, nil, false)
				require.ErrorIs(t, err, errPromptAdmissionRejected)
			} else {
				svc.rollbackPromptClaim(ctx, tid, sid, rollback)
			}
			after, err := repo.GetTaskSession(ctx, sid)
			require.NoError(t, err)
			require.Equal(t, expected, after.State)
			if scenario != "CAS_zero_rows" {
				require.Equal(t, before.UpdatedAt, after.UpdatedAt)
			}
			if scenario == "same_state_progress" {
				require.Equal(t, "new provider activity", after.ErrorMessage)
			}
			if barrier != nil {
				require.False(t, barrier.changed)
				require.Equal(t, "concurrent CAS winner", after.ErrorMessage)
			}
			if successorID != "" {
				active, err := svc.turnService.GetActiveTurn(ctx, sid)
				require.NoError(t, err)
				require.NotNil(t, active)
				require.Equal(t, successorID, active.ID)
			}
		})
	}
}

func TestMissingOwnTurnQueueRetryKeepsFIFOAndLongProvider(t *testing.T) {
	ctx := context.Background()
	const tid, sid = "fifo-missing-task", "fifo-missing-session"
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, tid, sid, models.TaskSessionStateWaitingForInput)
	seedExecutorRunning(t, repo, sid, tid, "fifo-execution")
	failFirst := true
	accepted := make(chan string, 2)
	manager := &mockAgentManager{isAgentRunning: true, repoForExecutionLookup: repo}
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), manager)
	svc.turnService = &repoTurnService{repo: repo}
	svc.executor = executor.NewExecutor(manager, repo, testLogger(), executor.ExecutorConfig{})
	svc.messageCreator = &mockMessageCreator{}
	manager.promptAgentFunc = func(ctx context.Context, _ string, prompt string, _ []v1.MessageAttachment, _ bool) (*executor.PromptResult, error) {
		if failFirst {
			failFirst = false
			turn, err := svc.turnService.GetActiveTurn(ctx, sid)
			if err != nil {
				return nil, err
			}
			require.NotNil(t, turn)
			require.NoError(t, repo.AbandonTurn(ctx, turn.ID))
			return nil, ErrSessionResetInProgress
		}
		accepted <- prompt
		return &executor.PromptResult{}, nil
	}
	svc.messageQueue.SetAutoMergeEnabled(false)
	head, _, ok, err := svc.messageQueue.QueueLifecycleMessageWithCoalesceKey(ctx, sid, tid, "head prompt", "", messagequeue.QueuedByWorkflow, false, nil, map[string]interface{}{"origin": githubPRAutomationOrigin}, "ownership-head", true)
	require.NoError(t, err)
	require.True(t, ok)
	tail, err := svc.messageQueue.QueueMessage(ctx, sid, tid, "tail prompt", "", messagequeue.QueuedByUser, false, nil)
	require.NoError(t, err)
	identity, err := svc.messageQueue.ResolveSessionIdentity(ctx, tid, sid)
	require.NoError(t, err)
	reserved, ok, _, err := svc.messageQueue.ReserveQueuedForDeliveryWithAutoRunForSession(ctx, identity)
	require.NoError(t, err)
	require.True(t, ok)
	guard := svc.lockCancelInFlightGuard(sid)
	reservation := svc.markQueuedDispatchInFlightWithIdentityLocked(identity, reserved.ID, reserved)
	guard.release()
	svc.executeQueuedMessageWithReservation(sid, reserved, reservation)
	state, err := repo.GetTaskSession(ctx, sid)
	require.NoError(t, err)
	require.Equal(t, models.TaskSessionStateWaitingForInput, state.State)
	status := svc.messageQueue.GetStatus(ctx, sid)
	require.Len(t, status.Entries, 2)
	require.Equal(t, head.ID, status.Entries[0].ID)
	require.Equal(t, tail.ID, status.Entries[1].ID)
	require.NoError(t, svc.messageQueue.SetAutoRunForSession(ctx, identity, false))
	svc.CheckQueueAdmissionReadiness(ctx, identity)
	require.Empty(t, accepted)
	require.NoError(t, svc.messageQueue.SetAutoRunForSession(ctx, identity, true))
	done := make(chan struct{}, 2)
	svc.onQueuedMessageExecutionComplete = func() { done <- struct{}{} }
	svc.CheckQueueAdmissionReadiness(ctx, identity)
	select {
	case prompt := <-accepted:
		require.Equal(t, "head prompt", prompt)
	case <-time.After(3 * time.Second):
		t.Fatal("head retry not accepted")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("head retry not settled")
	}
	status = svc.messageQueue.GetStatus(ctx, sid)
	require.Len(t, status.Entries, 1)
	require.Equal(t, tail.ID, status.Entries[0].ID)
	// A legitimately accepted, long-running head stays RUNNING. A new readiness
	// signal cannot replay it or start the tail while that provider owns the turn.
	svc.CheckQueueAdmissionReadiness(ctx, identity)
	require.Empty(t, accepted)
	state, err = repo.GetTaskSession(ctx, sid)
	require.NoError(t, err)
	require.Equal(t, models.TaskSessionStateRunning, state.State)
	turn, err := svc.turnService.GetActiveTurn(ctx, sid)
	require.NoError(t, err)
	require.NotNil(t, turn)
	require.NoError(t, svc.turnService.CompleteTurn(ctx, turn.ID))
	svc.activeTurns.CompareAndDelete(sid, turn.ID)
	require.NoError(t, repo.UpdateTaskSessionState(ctx, sid, models.TaskSessionStateWaitingForInput, ""))
	svc.CheckQueueAdmissionReadiness(ctx, identity)
	select {
	case prompt := <-accepted:
		require.Equal(t, "tail prompt", prompt)
	case <-time.After(3 * time.Second):
		t.Fatal("tail not accepted after head completion")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("tail not settled")
	}
	require.Zero(t, svc.messageQueue.GetStatus(ctx, sid).Count)
	require.Empty(t, accepted)
}

func TestPromptClaimDoesNotAdoptSuccessorSnapshotAfterCallback(t *testing.T) {
	for _, scenario := range []string{"successor_revision", "successor_execution"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			const tid, sid = "post-claim-owner-task", "post-claim-owner-session"
			repo := setupTestRepo(t)
			seedTaskAndSession(t, repo, tid, sid, models.TaskSessionStateWaitingForInput)
			seedExecutorRunning(t, repo, sid, tid, "original-execution")
			svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
			svc.turnService = &repoTurnService{repo: repo}
			initial, err := repo.GetTaskSession(ctx, sid)
			require.NoError(t, err)
			identity := messagequeue.QueueSessionIdentity{TaskID: tid, SessionID: sid, SessionIncarnationID: initial.QueueIncarnationID}
			var successor *models.TaskSession
			_, rollback, err := svc.claimPromptDispatch(ctx, tid, sid, "", false, false, nil, func() error {
				turn, err := svc.turnService.GetActiveTurn(ctx, sid)
				if err != nil {
					return err
				}
				require.NotNil(t, turn)
				if err = repo.AbandonTurn(ctx, turn.ID); err != nil {
					return err
				}
				if scenario == "successor_execution" {
					seedExecutorRunning(t, repo, sid, tid, "successor-execution")
				}
				// A competing owner commits after our RUNNING CAS and before the
				// callback returns. Its revision must never become our rollback token.
				_, err = repo.DB().ExecContext(ctx, "UPDATE task_sessions SET updated_at = ? WHERE id = ?", initial.UpdatedAt.Add(time.Hour), sid)
				if err != nil {
					return err
				}
				successor, err = repo.GetTaskSession(ctx, sid)
				return err
			}, nil, "", false, &identity)
			require.NoError(t, err)
			require.NotNil(t, successor)
			svc.rollbackPromptClaim(ctx, tid, sid, rollback)
			after, err := repo.GetTaskSession(ctx, sid)
			require.NoError(t, err)
			require.Equal(t, models.TaskSessionStateRunning, after.State, "post-claim read must not adopt successor ownership")
			require.Equal(t, successor.UpdatedAt, after.UpdatedAt)
			require.Equal(t, successor.AgentExecutionID, after.AgentExecutionID)
		})
	}
}
