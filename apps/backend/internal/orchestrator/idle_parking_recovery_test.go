package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/common/logger"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// @covers AC-EXECUTORS-IDLE-PARKING-001.11
func TestIdleParkingRecoveryIgnoresPassivePersistenceTimes(t *testing.T) {
	svc, agent, row := newIdleParkingFixture(t, true, 1, "claude-acp")
	ctx := context.Background()
	require.NoError(t, svc.repo.SetSessionMetadataKey(ctx, row.SessionID, "passive", "updated"))
	current, err := svc.repo.GetExecutorRunningBySessionID(ctx, row.SessionID)
	require.NoError(t, err)
	current.Metadata = map[string]interface{}{"passive": "updated"}
	require.NoError(t, svc.repo.UpsertExecutorRunning(ctx, current))
	svc.suspendWorkspaceIdleSessionsOnce(ctx)
	require.Len(t, agent.suspensionCalls(), 1, "fresh row/session metadata must not refresh real activity")
}

// @covers AC-EXECUTORS-IDLE-PARKING-001.11
func TestIdleParkingUnknownRecoveryReceivesOneStableFullInterval(t *testing.T) {
	svc, agent, row := newIdleParkingFixture(t, true, 1, "claude-acp")
	agent.currentPromptLastActivityAt = time.Time{}
	agent.currentPromptActivityEpoch.Store(0)
	now := time.Now().UTC()
	scan := func(at time.Time) {
		summary := idleParkingScanSummary{observed: make(map[idleParkingCandidateKey]struct{}), skipped: make(map[idleParkingSkipReason]int)}
		svc.processWorkspaceIdleRow(context.Background(), row, at, agent, &summary)
		svc.pruneIdleParkingCandidates(summary.observed)
	}
	scan(now)
	require.Empty(t, agent.suspensionCalls())
	scan(now.Add(59 * time.Second))
	require.Empty(t, agent.suspensionCalls())
	scan(now.Add(time.Minute))
	require.Len(t, agent.suspensionCalls(), 1, "rescanning must not restart the unknown-history interval")
}

func TestIdleParkingFutureAndForeignActivityStayProtected(t *testing.T) {
	for _, kind := range []string{"future", "other execution"} {
		t.Run(kind, func(t *testing.T) {
			svc, agent, _ := newIdleParkingFixture(t, true, 1, "claude-acp")
			old := agent.currentPromptLastActivityAt
			if kind == "future" {
				agent.currentPromptLastActivityAt = time.Now().Add(time.Hour)
			} else {
				agent.currentPromptExecutionID = "other"
			}
			svc.suspendWorkspaceIdleSessionsOnce(context.Background())
			require.Empty(t, agent.suspensionCalls())
			agent.currentPromptLastActivityAt = old
			agent.currentPromptExecutionID = "execution-idle-parking"
			svc.suspendWorkspaceIdleSessionsOnce(context.Background())
			require.Len(t, agent.suspensionCalls(), 1, "positive control: same candidate is suspendable")
		})
	}
}

func TestIdleParkingRoutineSkipSummaryUsesDebug(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	log, err := logger.NewFromZap(zap.New(core))
	require.NoError(t, err)
	svc := &Service{logger: log}
	summary := idleParkingScanSummary{skipped: map[idleParkingSkipReason]int{idleParkingSkipKnownWork: 1}}
	svc.logWorkspaceIdleScan(summary)
	svc.logWorkspaceIdleScan(summary)
	require.Equal(t, 2, logs.FilterLevelExact(zap.DebugLevel).Len())
	require.Zero(t, logs.FilterLevelExact(zap.InfoLevel).Len(), "unchanged skips should not produce Info every 30 seconds")
	svc.logWorkspaceIdleScan(idleParkingScanSummary{suspended: 1})
	require.Equal(t, 1, logs.FilterLevelExact(zap.InfoLevel).Len(), "real suspension outcomes stay visible")
}

// @covers AC-EXECUTORS-IDLE-PARKING-001.6
// @covers AC-EXECUTORS-IDLE-PARKING-001.11
func TestIdleParkingExplicitFocusFloorSurvivesRecovery(t *testing.T) {
	ctx := context.Background()
	svc, agent, row := newIdleParkingFixture(t, true, 1, "claude-acp")
	agent.isAgentReadyFn = func(context.Context, string) bool { return true }
	svc.refreshFocusedLiveSession(ctx, row.TaskID, row.SessionID)
	session, err := svc.repo.GetTaskSession(ctx, row.SessionID)
	require.NoError(t, err)
	require.Contains(t, session.Metadata, "idle_parking_focus", "explicit focus must have a durable positive control")
	// Startup recreates these maps; the tracked activity still describes the
	// old settled turn, as it does after lifecycle restores its saved clock.
	svc.idleParkingFocusAt, svc.idleParkingCandidates = nil, nil
	svc.suspendWorkspaceIdleSessionsOnce(ctx)
	require.Empty(t, agent.suspensionCalls(), "restart must not discard the fresh explicit-focus interval")
	now := time.Now().UTC()
	candidate, _, ok := svc.workspaceIdleCandidate(ctx, row, now)
	require.True(t, ok)
	require.False(t, svc.idleParkingCandidateDue(candidate.key, now.Add(59*time.Second), time.Minute))
	require.True(t, svc.idleParkingCandidateDue(candidate.key, now.Add(time.Minute), time.Minute))
}

func TestIdleParkingFocusFloorCannotTransferToAnotherExecution(t *testing.T) {
	ctx := context.Background()
	svc, agent, row := newIdleParkingFixture(t, true, 1, "claude-acp")
	agent.isAgentReadyFn = func(context.Context, string) bool { return true }
	svc.refreshFocusedLiveSession(ctx, row.TaskID, row.SessionID)
	svc.idleParkingFocusAt, svc.idleParkingCandidates = nil, nil
	row.AgentExecutionID = "replacement"
	agent.currentPromptExecutionID = row.AgentExecutionID
	require.NoError(t, svc.repo.UpsertExecutorRunning(ctx, row))
	svc.suspendWorkspaceIdleSessionsOnce(ctx)
	require.Len(t, agent.suspensionCalls(), 1, "a predecessor focus must not change a successor's idle interval")
}
