package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// lazyResumeStartFailure runs ensureSessionRunning, the resume a message or a
// queued dispatch performs on an idle session, against an agent whose start
// fails with startErr.
func lazyResumeStartFailure(t *testing.T, startErr error) error {
	t.Helper()
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "task1", "session1", models.TaskSessionStateWaitingForInput)
	session, err := repo.GetTaskSession(ctx, "session1")
	require.NoError(t, err)
	session.AgentExecutionID = "exec-contaminated"
	session.AgentProfileID = "profile1"
	require.NoError(t, repo.UpdateTaskSession(ctx, session))
	seedExecutorRunning(t, repo, session.ID, session.TaskID, "exec-contaminated")

	agentMgr := &mockAgentManager{
		isAgentRunning:         false,
		repoForExecutionLookup: repo,
		startAgentProcessErr:   startErr,
		launchAgentFunc: func(context.Context, *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
			return &executor.LaunchAgentResponse{AgentExecutionID: "exec-contaminated"}, nil
		},
	}
	taskRepo := newMockTaskRepo()
	taskRepo.tasks["task1"] = &v1.Task{ID: "task1", Title: "Test Task", State: v1.TaskStateReview}
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), taskRepo, agentMgr)
	svc.executor = executor.NewExecutor(agentMgr, repo, testLogger(), executor.ExecutorConfig{})
	wireStartFailureProjection(svc)

	session, err = repo.GetTaskSession(ctx, "session1")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- svc.ensureSessionRunning(ctx, "session1", session, launchOriginManual) }()
	select {
	case err = <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("lazy resume did not report the start failure")
	}
	require.Error(t, err)
	return err
}

// wireStartFailureProjection connects the executor callbacks NewService wires
// for an asynchronous start failure: the FAILED projection and the
// orchestrator's start-failed callback.
func wireStartFailureProjection(svc *Service) {
	svc.executor.SetOnSessionStateChange(func(ctx context.Context, taskID, sessionID string, state models.TaskSessionState, errorMessage string) error {
		svc.updateTaskSessionState(ctx, taskID, sessionID, state, errorMessage, true)
		return nil
	})
	svc.executor.SetOnSessionStateTransition(svc.transitionTaskSessionState)
	svc.executor.SetOnBootstrapFailureTransition(svc.transitionBootstrapFailure)
	svc.executor.SetOnBootstrapFailureMessageRepair(svc.persistBootstrapFailureMessage)
	svc.executor.SetOnSessionStarting(func(
		ctx context.Context, taskID string, session *models.TaskSession,
		expectedState models.TaskSessionState, promoteTask bool,
	) error {
		return svc.setSessionStarting(ctx, taskID, session, expectedState, promoteTask)
	})
	svc.executor.SetOnAgentProcessStartFailed(svc.handleAgentProcessStartFailed)
}

// TestLazyResumeCheckpointMissingRequiresContinuation covers a session whose
// durable delivery stream never reached a checkpoint: native resume can never
// succeed, so the lazy resume must report the same recovery-required signal
// the message and queue paths answer with automatic continue_from_history.
func TestLazyResumeCheckpointMissingRequiresContinuation(t *testing.T) {
	err := lazyResumeStartFailure(t, fmt.Errorf("failed to initialize ACP: %w", repoerrors.ErrAgentDeliveryCheckpointMissing))
	require.ErrorIs(t, err, ErrSessionRecoveryRequired)
	require.Equal(t, durableDeliveryUnresolvedReason, recoveryReasonFromError(err))
	require.True(t, interruptedTurnRecoveryReasons[recoveryReasonFromError(err)],
		"the reason must be one automatic continuation accepts")
}

func TestLazyResumeOrdinaryStartFailureStaysAFailure(t *testing.T) {
	err := lazyResumeStartFailure(t, errors.New("agent binary crashed"))
	require.NotErrorIs(t, err, ErrSessionRecoveryRequired)
	require.Empty(t, recoveryReasonFromError(err))
}
