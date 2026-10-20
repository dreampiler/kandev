package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
)

const (
	guardBlockedID = "session-blocked"
	guardQueued    = "follow-up"
)

// openRecoveryBlock records an open unresolved_durable_work block for the
// session's current incarnation and generation, as the production session
// d03d9c33 carried when it inherited the primary role.
func openRecoveryBlock(t *testing.T, repo *sqliterepo.Repository, sessionID string) {
	t.Helper()
	ctx := context.Background()
	session, err := repo.GetTaskSession(ctx, sessionID)
	require.NoError(t, err)
	require.NoError(t, repo.UpsertSessionRecoveryBlock(ctx, &models.SessionRecoveryBlock{
		ID: "block-" + sessionID, SessionID: sessionID, IncarnationID: session.QueueIncarnationID,
		Reason: "unresolved_durable_work", State: models.RecoveryBlockOpen,
	}))
}

// beginResumeAttempt registers a resume attempt the registry owns, as a resume
// of the session does before it launches.
func beginResumeAttempt(t *testing.T, svc *Service) *resumeAttempt {
	t.Helper()
	attempt, started := svc.resumeAttemptStore().begin(context.Background(), primaryTaskID, primaryStoppedID)
	require.True(t, started)
	t.Cleanup(func() { attempt.finish(svc.resumeAttemptStore()) })
	return attempt
}

// beginPrimaryResume puts the primary into STARTING under a resume attempt the
// registry owns, as a resume of the primary session does before it launches.
func beginPrimaryResume(t *testing.T, svc *Service, repo *sqliterepo.Repository) (*resumeAttempt, string) {
	t.Helper()
	ctx := context.Background()
	attempt := beginResumeAttempt(t, svc)
	attemptID := attempt.identity()
	require.NoError(t, repo.UpdateTaskSessionState(ctx, primaryStoppedID, models.TaskSessionStateStarting, ""))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, primaryStoppedID, models.SessionMetaKeyAgentStartAttemptID, attemptID))
	return attempt, attemptID
}

func queueOnPrimary(t *testing.T, svc *Service) {
	t.Helper()
	_, err := svc.messageQueue.QueueMessage(context.Background(), primaryStoppedID, primaryTaskID, guardQueued, "", "user", false, nil)
	require.NoError(t, err)
}

// requireQueueKept lets any detached hand-off run to completion, without
// cancelling it, and confirms the message kept its session target. The
// hand-off registers on parkedLoopWorkers before SetPrimarySession returns.
func requireQueueKept(t *testing.T, svc *Service, receiverID string) {
	t.Helper()
	svc.parkedLoopWorkers.Wait()
	require.Equal(t, []string{guardQueued}, queuedContents(t, svc, primaryStoppedID))
	require.Empty(t, queuedContents(t, svc, receiverID))
}

// Production 2026-10-20: a resume of the primary failed and its rollback wrote
// FAILED. That state publish promoted an older session with an open recovery
// block and moved the primary's queue onto it. The rollback must leave both
// the role and the queue on the session being restored.
func TestResumeRollbackKeepsPrimaryAndQueueOffBlockedSession(t *testing.T) {
	svc, repo := newPrimaryPromotionFixture(t, "",
		&models.TaskSession{ID: guardBlockedID, State: models.TaskSessionStateWaitingForInput},
	)
	enableTerminalPrimaryQueueHandoff(t, svc)
	openRecoveryBlock(t, repo, guardBlockedID)
	queueOnPrimary(t, svc)
	_, attemptID := beginPrimaryResume(t, svc, repo)

	changed, err := svc.rollbackResumeFailureIfCurrentAttempt(context.Background(), executor.ResumeFailureRollbackRequest{
		TaskID: primaryTaskID, SessionID: primaryStoppedID, AttemptID: attemptID,
		ExpectedState: models.TaskSessionStateStarting, NextState: models.TaskSessionStateFailed,
		ErrorMessage: "native_state_missing",
	})
	require.NoError(t, err)
	require.True(t, changed)

	require.Equal(t, primaryStoppedID, primarySessionID(t, repo))
	requireQueueKept(t, svc, guardBlockedID)
}

// An explicit primary choice while a resume attempt still owns the failed
// primary does not take its queue: the session is being restored even though
// the attempt may finish before the detached hand-off would run.
func TestExplicitPrimaryChoiceKeepsQueueOfSessionUnderResume(t *testing.T) {
	svc, repo := newPrimaryPromotionFixture(t, "",
		&models.TaskSession{ID: "session-next", State: models.TaskSessionStateStarting},
	)
	enableTerminalPrimaryQueueHandoff(t, svc)
	queueOnPrimary(t, svc)
	attempt, _ := beginPrimaryResume(t, svc, repo)
	ctx := context.Background()
	require.NoError(t, repo.UpdateTaskSessionState(ctx, primaryStoppedID, models.TaskSessionStateFailed, ""))

	// Hold the hand-off mutex across the switch so a scheduled worker cannot
	// observe the attempt finishing before SetPrimarySession returns.
	svc.terminalQueueHandOffMu.Lock()
	require.NoError(t, svc.SetPrimarySession(ctx, "session-next"))
	attempt.finish(svc.resumeAttemptStore())
	svc.terminalQueueHandOffMu.Unlock()

	require.Equal(t, "session-next", primarySessionID(t, repo))
	requireQueueKept(t, svc, "session-next")
}

// A hand-off scheduled by a genuine terminal promotion still must not move the
// queue when a retry is admitted after the FAILED write but before the
// detached worker runs: the source is terminal yet a resume attempt owns it.
func TestTerminalPrimaryQueueStaysWhenSourceIsUnderResume(t *testing.T) {
	svc, repo := newPrimaryPromotionFixture(t, "",
		&models.TaskSession{ID: "session-live", State: models.TaskSessionStateRunning},
	)
	enableTerminalPrimaryQueueHandoff(t, svc)
	queueOnPrimary(t, svc)
	ctx := context.Background()

	svc.terminalQueueHandOffMu.Lock()
	require.NoError(t, repo.UpdateTaskSessionState(ctx, primaryStoppedID, models.TaskSessionStateFailed, ""))
	svc.maybePromotePrimary(ctx, primaryTaskID, primaryStoppedID, models.TaskSessionStateFailed)
	require.Equal(t, "session-live", primarySessionID(t, repo))

	beginResumeAttempt(t, svc)
	svc.terminalQueueHandOffMu.Unlock()

	requireQueueKept(t, svc, "session-live")
}

// A genuinely terminal primary whose only successor awaits a recovery action
// still hands over the role, but its queue keeps its session target
// (AC-TASKS-PRIMARY-SESSION-OWNERSHIP-001.4).
func TestTerminalPrimaryQueueStaysWhenSuccessorAwaitsRecovery(t *testing.T) {
	for _, tc := range []struct {
		name       string
		routeState string
	}{
		{name: "open recovery block"},
		{name: "action_required route", routeState: dynamicRouteStatusActionRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := &models.TaskSession{ID: guardBlockedID, State: models.TaskSessionStateRunning, RouteState: tc.routeState}
			if tc.routeState != "" {
				session.RouteGeneration = 1
			}
			svc, repo := newPrimaryPromotionFixture(t, "", session)
			enableTerminalPrimaryQueueHandoff(t, svc)
			if tc.routeState == "" {
				openRecoveryBlock(t, repo, guardBlockedID)
			}
			queueOnPrimary(t, svc)
			ctx := context.Background()

			require.NoError(t, repo.UpdateTaskSessionState(ctx, primaryStoppedID, models.TaskSessionStateFailed, ""))
			svc.maybePromotePrimary(ctx, primaryTaskID, primaryStoppedID, models.TaskSessionStateFailed)

			require.Equal(t, guardBlockedID, primarySessionID(t, repo))
			requireQueueKept(t, svc, guardBlockedID)
		})
	}
}

// AC-TASKS-PRIMARY-SESSION-OWNERSHIP-001.2: a session with an open recovery
// block ranks below a parked session, even when it is running.
func TestPrimaryPromotionRanksRecoveryBlockedSessionBelowParked(t *testing.T) {
	svc, repo := newPrimaryPromotionFixture(t, "",
		&models.TaskSession{ID: guardBlockedID, State: models.TaskSessionStateRunning},
		&models.TaskSession{ID: "session-parked", State: models.TaskSessionStateStarting},
	)
	openRecoveryBlock(t, repo, guardBlockedID)
	parkSession(t, repo, "session-parked")

	stopPrimary(t, svc, repo)

	require.Equal(t, "session-parked", primarySessionID(t, repo))
}

// A session whose dynamic route awaits an operator ranks below a healthy one,
// even when it is running.
func TestPrimaryPromotionRanksActionRequiredRouteLast(t *testing.T) {
	svc, repo := newPrimaryPromotionFixture(t, "",
		&models.TaskSession{ID: "session-route", State: models.TaskSessionStateRunning,
			RouteState: dynamicRouteStatusActionRequired, RouteGeneration: 1},
		&models.TaskSession{ID: "session-live", State: models.TaskSessionStateStarting},
	)
	route, err := repo.GetTaskSession(context.Background(), "session-route")
	require.NoError(t, err)
	require.Equal(t, dynamicRouteStatusActionRequired, route.RouteState)

	stopPrimary(t, svc, repo)

	require.Equal(t, "session-live", primarySessionID(t, repo))
}
