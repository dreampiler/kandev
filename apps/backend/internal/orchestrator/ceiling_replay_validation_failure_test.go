package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	tasksqlite "github.com/kandev/kandev/internal/task/repository/sqlite"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// A deterministic launch-validation failure must not be retried by the
// ceiling replay: the record is dropped instead of retained.
func TestCeilingReplayResultFromExecution_ValidationFailureIsTerminal(t *testing.T) {
	launchErr := errors.New("validate agent command: agent command cannot be empty")
	result := ceilingReplayResultFromExecution(nil, launchErr)
	require.Equal(t, ceilingReplayPermanentlyFailed, result.outcome)
	require.Equal(t, launchErr, result.err)
}

// A transient dispatch failure keeps today's behavior: retained for a later
// sweep.
func TestCeilingReplayResultFromExecution_TransientFailureStillRetries(t *testing.T) {
	result := ceilingReplayResultFromExecution(nil, errors.New("connection reset by peer"))
	require.Equal(t, ceilingReplayFailed, result.outcome)
}

// Success and still-deferred keep their outcomes.
func TestCeilingReplayResultFromExecution_SuccessAndDeferredUnchanged(t *testing.T) {
	require.Equal(t, ceilingReplaySucceeded, ceilingReplayResultFromExecution(&executor.TaskExecution{}, nil).outcome)
	require.Equal(t, ceilingReplayStillDeferred, ceilingReplayResultFromExecution(nil, nil).outcome)
}

// A permanently failed replay drops the record instead of retaining it for
// the next sweep: no new session is created per retry.
func TestSettleCeilingReplayPermanentlyFailedDropsTheRecord(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "drop-invalid-launch", Title: "t", State: v1.TaskStateScheduling,
		CreatedAt: now, UpdatedAt: now,
	}))
	deferral := models.CeilingDeferral{
		Kind: models.CeilingLaunchStart,
		Payload: map[string]interface{}{
			metaKeyPrompt:                  "p",
			ceilingPayloadAutomationRunKey: map[string]interface{}{"run_id": "run-gone"},
		},
		Origin:     string(launchOriginAutomatic),
		ReasonCode: ceilingReasonRefused,
		QueuedAt:   now,
	}
	record := models.CeilingRecordKeys(deferral)
	_, _, err := repo.SetTaskDeferredLaunchIfUnchanged(ctx, "drop-invalid-launch", tasksqlite.AbsentDeferredLaunch(), record)
	require.NoError(t, err)

	// Read back the stored record so the identity comparison in the clear
	// path sees the exact persisted form.
	storedRaw, _, err := repo.GetTaskDeferredLaunch(ctx, "drop-invalid-launch")
	require.NoError(t, err)
	stored, err := models.ReadCeilingDeferral(storedRaw)
	require.NoError(t, err)

	launchErr := errors.New("validate agent command: agent command cannot be empty")
	svc.deferredRetrySchedule = newDeferredRetrySchedule()
	svc.settleCeilingReplay(ctx,
		&models.Task{ID: "drop-invalid-launch"},
		nil, stored,
		ceilingReplayPermanentlyFailedResult(launchErr, "launch validation failed"))

	require.False(t, models.HasCeilingDeferredIntent(&models.Task{Metadata: map[string]interface{}{
		models.MetaKeyDeferredLaunch: deferredLaunchOf(t, svc, "drop-invalid-launch"),
	}}), "a deterministically failing launch must be dropped, not retried")
}
