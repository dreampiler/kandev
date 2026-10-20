package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
)

type staleRouteFixture struct {
	ctx         context.Context
	repo        *sqliterepo.Repository
	svc         *Service
	taskID      string
	sessionID   string
	steps       []string
	entries     []int64
	routeStep   string
	routeEntry  int64
	currentStep string
	currentEnt  int64
	previousEnt int64
}

// newStaleRouteFixture seeds a task that moved from the previous step to the
// current one while the committed workflow_session_route still belongs to the
// previous entry. This is the state a queued sessionless start sees when
// capacity deferral kept the destination route from being materialized.
func newStaleRouteFixture(t *testing.T) *staleRouteFixture {
	t.Helper()
	return newStaleRouteFixtureWithRouteLag(t, 1,
		"stale-route-previous", "stale-route-current")
}

// newTwoEntryLagFixture keeps the stale route two entries behind the task, so
// the route does not describe the immediately previous entry.
func newTwoEntryLagFixture(t *testing.T) *staleRouteFixture {
	t.Helper()
	return newStaleRouteFixtureWithRouteLag(t, 2,
		"stale-route-predecessor", "stale-route-previous", "stale-route-current")
}

// newStaleRouteFixtureWithRouteLag seeds the given step history through the
// real transition ledger and leaves the task on the last step. The committed
// route is pinned routeLag entries back, naming that entry's destination step.
func newStaleRouteFixtureWithRouteLag(t *testing.T, routeLag int, steps ...string) *staleRouteFixture {
	t.Helper()
	ctx := context.Background()
	repo := setupTestRepo(t)
	const (
		taskID    = "stale-route-task"
		sessionID = "stale-route-session"
	)
	require.GreaterOrEqual(t, len(steps), 2)
	seedTaskAndSessionWithStep(t, repo, taskID, sessionID, steps[0])

	entries := make([]int64, 0, len(steps))
	genesisEntry, err := repo.GetLatestTaskStepTransitionID(ctx, taskID)
	require.NoError(t, err)
	require.Positive(t, genesisEntry)
	entries = append(entries, genesisEntry)
	for _, step := range steps[1:] {
		task, taskErr := repo.GetTask(ctx, taskID)
		require.NoError(t, taskErr)
		task.WorkflowStepID = step
		require.NoError(t, repo.UpdateTaskPreservingDeferredLaunch(ctx, task))
		entryID, entryErr := repo.GetLatestTaskStepTransitionID(ctx, taskID)
		require.NoError(t, entryErr)
		require.Greater(t, entryID, entries[len(entries)-1])
		entries = append(entries, entryID)
	}

	routeIndex := len(steps) - 1 - routeLag
	require.GreaterOrEqual(t, routeIndex, 0)
	routeStep := steps[routeIndex]
	routeEntry := entries[routeIndex]
	require.NoError(t, repo.SetTaskMetadataKey(ctx, taskID, models.MetaKeyWorkflowSessionRoute,
		models.WorkflowSessionRoute{
			OperationID:       "route-stale",
			DestinationStepID: routeStep,
			EntryIdentity:     workflowEntryIdentityForID(routeEntry),
			TargetKind:        "new_session",
			DestinationID:     sessionID,
			Phase:             "committed",
		}))

	stepGetter := newMockStepGetter()
	for _, step := range steps {
		stepGetter.steps[step] = &wfmodels.WorkflowStep{ID: step, WorkflowID: "wf1"}
	}
	stepGetter.steps["stale-route-successor"] = &wfmodels.WorkflowStep{ID: "stale-route-successor", WorkflowID: "wf1"}

	return &staleRouteFixture{
		ctx:         ctx,
		repo:        repo,
		svc:         createTestService(repo, stepGetter, newMockTaskRepo()),
		taskID:      taskID,
		sessionID:   sessionID,
		steps:       steps,
		entries:     entries,
		routeStep:   routeStep,
		routeEntry:  routeEntry,
		currentStep: steps[len(steps)-1],
		currentEnt:  entries[len(entries)-1],
		previousEnt: entries[len(entries)-2],
	}
}

func (f *staleRouteFixture) task(t *testing.T) *models.Task {
	t.Helper()
	task, err := f.repo.GetTask(f.ctx, f.taskID)
	require.NoError(t, err)
	return task
}

// unboundStart is the sessionless start record seam 1 queued for currentStep
// after the capacity gate deferred it, before the destination route committed.
func (f *staleRouteFixture) unboundStart() models.CeilingDeferral {
	return models.CeilingDeferral{
		Kind: models.CeilingLaunchStart,
		Payload: map[string]interface{}{
			metaKeyWorkflowStepID: f.currentStep,
			"workflow_entry_id":   f.currentEnt,
			metaKeyAgentProfileID: "profile-agent",
			metaKeyPrompt:         "entry prompt",
			"auto_start":          true,
		},
		Origin:     string(launchOriginAutomatic),
		ReasonCode: ceilingReasonRefused,
		QueuedAt:   time.Now().UTC(),
	}
}

// A committed route belonging to the immediately previous entry is not proof
// that the current entry was superseded: the task already carries the record's
// step and ledger entry, so the sessionless queued start must stay replayable.
func TestValidateCeilingEntry_UnboundStartWithStaleRouteStaysValid(t *testing.T) {
	f := newStaleRouteFixture(t)

	disposition, detail, err := f.svc.validateCeilingEntry(f.ctx, f.task(t), f.unboundStart())
	require.NoError(t, err)
	require.Equal(t, ceilingEntryValid, disposition, detail)
}

// A session-bearing record keeps the payload-session comparison: only the
// sessionless seam-1 start may treat the previous entry's route as lag.
func TestValidateCeilingEntry_SessionBoundStartWithStaleRouteStaysSuperseded(t *testing.T) {
	f := newStaleRouteFixture(t)
	deferral := f.unboundStart()
	deferral.Kind = models.CeilingLaunchStartCreated
	deferral.Payload[metaKeySessionID] = f.sessionID

	disposition, detail, err := f.svc.validateCeilingEntry(f.ctx, f.task(t), deferral)
	require.NoError(t, err)
	require.Equal(t, ceilingEntrySuperseded, disposition, detail)
}

// A route two entries behind does not describe the immediately previous entry,
// so it is not lag and must not be forgiven.
func TestValidateCeilingEntry_UnboundStartWithOlderStaleRouteStaysSuperseded(t *testing.T) {
	f := newTwoEntryLagFixture(t)
	require.NotEqual(t, f.previousEnt, f.routeEntry)

	disposition, detail, err := f.svc.validateCeilingEntry(f.ctx, f.task(t), f.unboundStart())
	require.NoError(t, err)
	require.Equal(t, ceilingEntrySuperseded, disposition, detail)
}

// A route naming a foreign entry identity is not the previous entry even when
// its destination step matches.
func TestValidateCeilingEntry_UnboundStartWithForeignRouteEntryStaysSuperseded(t *testing.T) {
	f := newStaleRouteFixture(t)
	require.NoError(t, f.repo.SetTaskMetadataKey(f.ctx, f.taskID, models.MetaKeyWorkflowSessionRoute,
		models.WorkflowSessionRoute{
			OperationID:       "route-stale",
			DestinationStepID: f.routeStep,
			EntryIdentity:     workflowEntryIdentityForID(f.routeEntry + 500),
			TargetKind:        "new_session",
			DestinationID:     f.sessionID,
			Phase:             "committed",
		}))

	disposition, detail, err := f.svc.validateCeilingEntry(f.ctx, f.task(t), f.unboundStart())
	require.NoError(t, err)
	require.Equal(t, ceilingEntrySuperseded, disposition, detail)
}

// The same lag guard must still reject a record that names an older entry: the
// task's latest ledger identity is the admission contract.
func TestValidateCeilingEntry_UnboundStartWithOlderEntryStaysSuperseded(t *testing.T) {
	f := newStaleRouteFixture(t)
	deferral := f.unboundStart()
	deferral.Payload["workflow_entry_id"] = f.previousEnt

	disposition, detail, err := f.svc.validateCeilingEntry(f.ctx, f.task(t), deferral)
	require.NoError(t, err)
	require.Equal(t, ceilingEntrySuperseded, disposition, detail)
}

// A record for a step the task has left is superseded even when its entry id
// happens to be the latest one.
func TestValidateCeilingEntry_UnboundStartAfterLeavingStepStaysSuperseded(t *testing.T) {
	f := newStaleRouteFixture(t)
	task := f.task(t)
	task.WorkflowStepID = "stale-route-successor"
	require.NoError(t, f.repo.UpdateTaskPreservingDeferredLaunch(f.ctx, task))

	disposition, detail, err := f.svc.validateCeilingEntry(f.ctx, f.task(t), f.unboundStart())
	require.NoError(t, err)
	require.Equal(t, ceilingEntrySuperseded, disposition, detail)
}

// A record with a stored binding keeps the strict route comparison: the stale
// route names its own entry, so the bound record is still superseded rather
// than retargeted to the current entry.
func TestValidateCeilingEntry_BoundStartWithStaleRouteStaysSuperseded(t *testing.T) {
	f := newStaleRouteFixture(t)
	binding := models.CeilingWorkflowEntryBinding{
		WorkflowID:           "wf1",
		DestinationStepID:    f.routeStep,
		RouteOperationID:     "route-stale",
		EntryIdentity:        workflowEntryIdentityForID(f.routeEntry),
		DestinationSessionID: f.sessionID,
	}
	deferral := models.CeilingDeferral{
		Kind: models.CeilingLaunchStartCreated,
		Payload: map[string]interface{}{
			metaKeySessionID:                    f.sessionID,
			metaKeyWorkflowStepID:               f.routeStep,
			"workflow_entry_id":                 f.routeEntry,
			models.CeilingLaunchEntryBindingKey: ceilingEntryBindingValue(binding),
		},
		Origin:     string(launchOriginAutomatic),
		ReasonCode: ceilingReasonRefused,
		QueuedAt:   time.Now().UTC(),
	}

	disposition, detail, err := f.svc.validateCeilingEntry(f.ctx, f.task(t), deferral)
	require.NoError(t, err)
	require.Equal(t, ceilingEntrySuperseded, disposition, detail)
}
