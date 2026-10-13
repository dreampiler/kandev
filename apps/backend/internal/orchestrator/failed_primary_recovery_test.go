package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
)

func TestFailedPrimaryContinuationEligibilityUsesCanonicalClassification(t *testing.T) {
	for _, tc := range []struct {
		name                string
		state               models.TaskSessionState
		reason              string
		primary, live, want bool
	}{
		{"agent termination unproven", models.TaskSessionStateFailed, unknownPromptOutcome, true, false, true},
		{"recoverable delivery", models.TaskSessionStateFailed, durableDeliveryUnresolvedReason, true, false, true},
		{"recoverable queued dispatch", models.TaskSessionStateFailed, unknownQueueDispatchRecoveryReason, true, false, true},
		{"ordinary failure", models.TaskSessionStateFailed, "", true, false, false},
		{"unclassified failure", models.TaskSessionStateFailed, "runtime_installation_failed", true, false, false},
		{"old nonprimary", models.TaskSessionStateFailed, unknownPromptOutcome, false, false, false},
		{"live owner", models.TaskSessionStateFailed, unknownPromptOutcome, true, true, false},
		{"user cancelled", models.TaskSessionStateCancelled, unknownPromptOutcome, true, false, false},
		{"completed", models.TaskSessionStateCompleted, unknownPromptOutcome, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo := setupTestRepo(t)
			seedTaskAndSession(t, repo, "task", "session", tc.state)
			if tc.primary {
				require.NoError(t, repo.SetSessionPrimary(ctx, "session"))
			}
			session, err := repo.GetTaskSession(ctx, "session")
			require.NoError(t, err)
			if tc.reason != "" {
				require.NoError(t, repo.UpsertSessionRecoveryBlock(ctx, &models.SessionRecoveryBlock{
					SessionID: session.ID, IncarnationID: session.QueueIncarnationID,
					Reason: tc.reason, State: models.RecoveryBlockOpen,
				}))
			}
			agent := &mockAgentManager{isAgentRunningFn: func(context.Context, string) bool { return tc.live }}
			svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), agent)
			require.Equal(t, tc.want, svc.CanAutoRecoverFailedSession(ctx, "task", "session"))
			if !tc.want {
				require.False(t, svc.AutoRecoverBlockedPrompt(ctx, "task", "session", nil))
			}
		})
	}
}

func TestFailedPrimaryKeepsReleaseDCheckpointContinuationClassification(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "task", "failed", models.TaskSessionStateFailed)
	require.NoError(t, repo.SetSessionPrimary(ctx, "failed"))
	session, err := repo.GetTaskSession(ctx, "failed")
	require.NoError(t, err)
	session.ErrorMessage = "failed to initialize ACP: " + repoerrors.ErrAgentDeliveryCheckpointMissing.Error()
	require.NoError(t, repo.UpdateTaskSession(ctx, session))
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	require.True(t, svc.CanAutoRecoverFailedSession(ctx, "task", "failed"),
		"release d's classified startup refusal can require continuation before a block is persisted")
}

type failedPrimaryCheckpointRepo struct {
	*sqliterepo.Repository
	prepared chan models.ContinuationSnapshot
}

func (r *failedPrimaryCheckpointRepo) CreateContinuationSnapshot(_ context.Context, snapshot *models.ContinuationSnapshot) error {
	r.prepared <- *snapshot
	return errors.New("checkpoint storage unavailable")
}

func TestQueueForFailedPrimaryUsesHistoryContinuationWithoutLosingWork(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "task", "failed", models.TaskSessionStateFailed)
	require.NoError(t, repo.SetSessionPrimary(ctx, "failed"))
	session, err := repo.GetTaskSession(ctx, "failed")
	require.NoError(t, err)
	require.NoError(t, repo.UpsertSessionRecoveryBlock(ctx, &models.SessionRecoveryBlock{
		SessionID: session.ID, IncarnationID: session.QueueIncarnationID,
		Reason: unknownPromptOutcome, State: models.RecoveryBlockOpen,
	}))
	require.NoError(t, repo.CreateTurn(ctx, &models.Turn{
		ID: "recorded-turn", TaskID: "task", TaskSessionID: "failed", StartedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.CreateMessage(ctx, &models.Message{
		ID: "recorded-message", TurnID: "recorded-turn", TaskID: "task", TaskSessionID: "failed",
		AuthorType: models.MessageAuthorUser, Content: "recorded controller instruction", CreatedAt: time.Now().UTC(),
	}))
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	checkpointRepo := &failedPrimaryCheckpointRepo{Repository: repo, prepared: make(chan models.ContinuationSnapshot, 1)}
	svc.repo = checkpointRepo
	err = svc.QueueUserPrompt(ctx, "task", "failed", "pending queue instruction", "", false, nil, nil, false)
	require.NoError(t, err)

	select {
	case snapshot := <-checkpointRepo.prepared:
		require.Equal(t, "failed", snapshot.SessionID)
		require.Contains(t, snapshot.Content, "recorded controller instruction")
		require.Equal(t, int64(1), snapshot.TargetGeneration)
	case <-time.After(5 * time.Second):
		t.Fatal("FAILED primary queue did not prepare the existing history continuation")
	}
	require.Eventually(t, func() bool {
		svc.interruptedTurnAutoRecoveryMu.Lock()
		defer svc.interruptedTurnAutoRecoveryMu.Unlock()
		return !svc.interruptedTurnAutoRecovery["failed"].running
	}, 5*time.Second, time.Millisecond)
	entries, _, err := svc.messageQueue.SnapshotSession(ctx, "failed")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "pending queue instruction", entries[0].Content)
	block, err := svc.GetOpenSessionRecoveryBlock(ctx, "failed")
	require.NoError(t, err)
	require.NotNil(t, block, "a failed continuation must preserve the unresolved outcome")
}

func TestQueueUserPromptDoesNotReviveUnrecoverableTerminalSessions(t *testing.T) {
	for _, state := range []models.TaskSessionState{
		models.TaskSessionStateFailed, models.TaskSessionStateCancelled, models.TaskSessionStateCompleted,
	} {
		t.Run(string(state), func(t *testing.T) {
			ctx := context.Background()
			repo := setupTestRepo(t)
			seedTaskAndSession(t, repo, "task", "ended", state)
			require.NoError(t, repo.SetSessionPrimary(ctx, "ended"))
			session, err := repo.GetTaskSession(ctx, "ended")
			require.NoError(t, err)
			reason := unknownPromptOutcome
			if state == models.TaskSessionStateFailed {
				reason = "native_state_missing"
			}
			require.NoError(t, repo.UpsertSessionRecoveryBlock(ctx, &models.SessionRecoveryBlock{
				SessionID: session.ID, IncarnationID: session.QueueIncarnationID,
				Reason: reason, State: models.RecoveryBlockOpen,
			}))
			svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
			err = svc.QueueUserPrompt(ctx, "task", "ended", "must not restart", "", false, nil, nil, false)
			require.ErrorIs(t, err, ErrSessionNotPromptable)
			require.Zero(t, svc.messageQueue.GetStatus(ctx, "ended").Count)
		})
	}
}
