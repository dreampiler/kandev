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
	ctx          context.Context
	repo         *sqliterepo.Repository
	svc          *Service
	taskID       string
	sessionID    string
	previousStep string
	currentStep  string
	previousEnt  int64
	currentEnt   int64
}

// newStaleRouteFixture seeds a task that moved from previousStep to currentStep
// while the committed workflow_session_route still belongs to previousStep's
// entry. This is the state a queued start sees when capacity deferral kept the
// destination route from being materialized.
func newStaleRouteFixture(t *testing.T) *staleRouteFixture {
	t.Helper()
	ctx := context.Background()
	repo := setupTestRepo(t)
	const (
		taskID       = "stale-route-task"
		sessionID    = "stale-route-session"
		previousStep = "stale-route-previous"
		currentStep  = "stale-route-current"
	)
	seedTaskAndSessionWithStep(t, repo, taskID, sessionID, previousStep)
	previousEntry, err := repo.GetLatestTaskStepTransitionID(ctx, taskID)
	require.NoError(t, err)
	require.Positive(t, previousEntry)

	task, err := repo.GetTask(ctx, taskID)
	require.NoError(t, err)
	task.WorkflowStepID = currentStep
	require.NoError(t, repo.UpdateTaskPreservingDeferredLaunch(ctx, task))
	currentEntry, err := repo.GetLatestTaskStepTransitionID(ctx, taskID)
	require.NoError(t, err)
	require.Greater(t, currentEntry, previousEntry)

	require.NoError(t, repo.SetTaskMetadataKey(ctx, taskID, models.MetaKeyWorkflowSessionRoute,
		models.WorkflowSessionRoute{
			OperationID:       "route-previous",
			DestinationStepID: previousStep,
			EntryIdentity:     workflowEntryIdentityForID(previousEntry),
			TargetKind:        "new_session",
			DestinationID:     sessionID,
			Phase:             "committed",
		}))

	stepGetter := newMockStepGetter()
	stepGetter.steps[currentStep] = &wfmodels.WorkflowStep{ID: currentStep, WorkflowID: "wf1"}
	stepGetter.steps[previousStep] = &wfmodels.WorkflowStep{ID: previousStep, WorkflowID: "wf1"}
	stepGetter.steps["stale-route-successor"] = &wfmodels.WorkflowStep{ID: "stale-route-successor", WorkflowID: "wf1"}

	return &staleRouteFixture{
		ctx:          ctx,
		repo:         repo,
		svc:          createTestService(repo, stepGetter, newMockTaskRepo()),
		taskID:       taskID,
		sessionID:    sessionID,
		previousStep: previousStep,
		currentStep:  currentStep,
		previousEnt:  previousEntry,
		currentEnt:   currentEntry,
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

// A committed route belonging to the previous entry is not proof that the
// current entry was superseded: the task already carries the record's step and
// ledger entry, so the queued start must stay replayable.
func TestValidateCeilingEntry_UnboundStartWithStaleRouteStaysValid(t *testing.T) {
	f := newStaleRouteFixture(t)

	disposition, detail, err := f.svc.validateCeilingEntry(f.ctx, f.task(t), f.unboundStart())
	require.NoError(t, err)
	require.Equal(t, ceilingEntryValid, disposition, detail)
}

// The same stale route must still reject a record that names an older entry:
// the task's latest ledger identity is the admission contract.
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
		DestinationStepID:    f.previousStep,
		RouteOperationID:     "route-previous",
		EntryIdentity:        workflowEntryIdentityForID(f.previousEnt),
		DestinationSessionID: f.sessionID,
	}
	deferral := models.CeilingDeferral{
		Kind: models.CeilingLaunchStartCreated,
		Payload: map[string]interface{}{
			metaKeySessionID:                    f.sessionID,
			metaKeyWorkflowStepID:               f.previousStep,
			"workflow_entry_id":                 f.previousEnt,
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
