package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	runsmodels "github.com/kandev/kandev/internal/runs/models"
)

func TestSettleCeilingDeferredRun(t *testing.T) {
	repo, db := newTestRepoWithDB(t)
	seedAgentProfile(t, db, "agent-cap", "ws-cap")
	ctx := context.Background()

	run := &runsmodels.Run{
		AgentProfileID: "agent-cap",
		Reason:         "task_assigned",
		Payload:        `{"task_id":"t-cap"}`,
		Status:         "queued",
		CoalescedCount: 1,
	}
	require.NoError(t, repo.CreateRun(ctx, run))
	require.NoError(t, repo.ParkRunForProviderCapacity(
		ctx, run.ID, string(runsmodels.RoutingBlockedActionRequired), time.Time{},
	))

	wrote, err := repo.SettleCeilingDeferredRun(ctx, run.ID, "session-1")
	require.NoError(t, err)
	require.True(t, wrote)

	got, err := repo.GetRunByID(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "claimed", string(got.Status))
	require.Equal(t, "session-1", got.SessionID)
	require.Nil(t, got.RoutingBlockedStatus)
	require.NotNil(t, got.ClaimedAt)
	require.Nil(t, got.FinishedAt)

	// A duplicate replay is a no-op and cannot overwrite the bound session.
	wrote, err = repo.SettleCeilingDeferredRun(ctx, run.ID, "session-2")
	require.NoError(t, err)
	require.False(t, wrote)
	got, err = repo.GetRunByID(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "session-1", got.SessionID)
}

func TestSettleCeilingDeferredRunIgnoresOtherParks(t *testing.T) {
	repo, db := newTestRepoWithDB(t)
	seedAgentProfile(t, db, "agent-cap", "ws-cap")
	ctx := context.Background()

	run := &runsmodels.Run{
		AgentProfileID: "agent-cap",
		Reason:         "task_assigned",
		Payload:        `{"task_id":"t-cap"}`,
		Status:         "queued",
		CoalescedCount: 1,
	}
	require.NoError(t, repo.CreateRun(ctx, run))
	require.NoError(t, repo.ParkRunForProviderCapacity(
		ctx, run.ID, string(runsmodels.RoutingBlockedWaitingForCapacity), time.Now().UTC().Add(time.Minute),
	))

	wrote, err := repo.SettleCeilingDeferredRun(ctx, run.ID, "session-1")
	require.NoError(t, err)
	require.False(t, wrote)

	got, err := repo.GetRunByID(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "queued", string(got.Status))
	require.NotNil(t, got.RoutingBlockedStatus)
}

func TestFailCeilingDeferredRun(t *testing.T) {
	repo, db := newTestRepoWithDB(t)
	seedAgentProfile(t, db, "agent-cap", "ws-cap")
	ctx := context.Background()

	run := &runsmodels.Run{
		AgentProfileID: "agent-cap",
		Reason:         "task_assigned",
		Payload:        `{"task_id":"t-cap"}`,
		Status:         "queued",
		CoalescedCount: 1,
	}
	require.NoError(t, repo.CreateRun(ctx, run))
	require.NoError(t, repo.ParkRunForProviderCapacity(
		ctx, run.ID, string(runsmodels.RoutingBlockedActionRequired), time.Time{},
	))

	failed, err := repo.FailCeilingDeferredRun(ctx, run.ID, "the queued launch was dropped: superseded")
	require.NoError(t, err)
	require.NotNil(t, failed)
	require.Equal(t, "failed", string(failed.Status))
	require.Equal(t, "the queued launch was dropped: superseded", failed.ErrorMessage)
	require.Nil(t, failed.RoutingBlockedStatus)
	require.NotNil(t, failed.FinishedAt)

	// A stale drop cannot fail a run another path already settled.
	again, err := repo.FailCeilingDeferredRun(ctx, run.ID, "second drop")
	require.NoError(t, err)
	require.Nil(t, again)
	got, err := repo.GetRunByID(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "the queued launch was dropped: superseded", got.ErrorMessage)
}
