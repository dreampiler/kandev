package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/task/models"
)

// TestSettleCeilingReplayFailureLogsTheReason is the fix for the blind retry
// loop: a non-ceiling replay failure is retried forever, so the sweep's warning
// must carry the actual error and reason instead of only that one happened.
func TestSettleCeilingReplayFailureLogsTheReason(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	observedLogger, err := logger.NewFromZap(zap.New(core))
	require.NoError(t, err)
	svc := &Service{
		logger:                observedLogger,
		deferredRetrySchedule: newDeferredRetrySchedule(),
	}
	task := &models.Task{ID: "t-replay-failure-log"}
	deferral := models.CeilingDeferral{Kind: models.CeilingLaunchPromptEnsure, QueuedAt: time.Now().UTC()}
	sentinel := errors.New("session runtime unavailable")

	svc.settleCeilingReplay(context.Background(), task, nil, deferral,
		ceilingReplayFailedResult(sentinel, "prompt ensure dispatch failed"))

	entries := logs.FilterMessage(
		"ceiling retry replay failed for a non-ceiling reason; will retry on a later sweep").All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	require.Equal(t, "prompt ensure dispatch failed", fields["detail"])
	require.Equal(t, sentinel.Error(), fields["error"])
	require.Equal(t, task.ID, fields["task_id"])
}

// TestCeilingReplayFailedResultRetainsTheCause locks the contract the log line
// depends on: the failure factories must carry the error and reason through,
// while every non-failure outcome leaves both zero.
func TestCeilingReplayFailedResultRetainsTheCause(t *testing.T) {
	sentinel := errors.New("boom")
	failed := ceilingReplayFailedResult(sentinel, "dispatch failed")
	require.Equal(t, ceilingReplayFailed, failed.outcome)
	require.ErrorIs(t, failed.err, sentinel)
	require.Equal(t, "dispatch failed", failed.detail)

	for _, result := range []ceilingReplayResult{
		ceilingReplaySucceededResult(),
		ceilingReplayDeferredResult(),
		ceilingReplaySupersededResult(),
		ceilingReplayRunClosedResult(),
	} {
		require.Nil(t, result.err)
		require.Empty(t, result.detail)
	}
}
