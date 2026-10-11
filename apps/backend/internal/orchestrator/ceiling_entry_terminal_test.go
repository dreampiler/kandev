package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
	tasksqlite "github.com/kandev/kandev/internal/task/repository/sqlite"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// @covers AC-AGENTS-SESSION-CEILING-001.12
func TestEvaluateCeilingDropReasons_LegacySessionlessStartIsTerminal(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskWithoutSession(t, repo, "drop-legacy-start", "step-auto")
	require.NoError(t, repo.SetTaskMetadataKey(ctx, "drop-legacy-start", models.MetaKeyWorkflowSessionRoute, models.WorkflowSessionRoute{
		OperationID:       "route-legacy",
		DestinationStepID: "step-auto",
		EntryIdentity:     "entry:00000000000000000001",
		TargetKind:        "new_session",
		Phase:             "committed",
	}))
	task, err := repo.GetTask(ctx, "drop-legacy-start")
	require.NoError(t, err)

	stepGetter := newMockStepGetter()
	stepGetter.steps["step-auto"] = &wfmodels.WorkflowStep{ID: "step-auto", WorkflowID: "wf1",
		Events: wfmodels.StepEvents{OnEnter: []wfmodels.OnEnterAction{{Type: wfmodels.OnEnterAutoStartAgent}}}}
	svc := createTestService(repo, stepGetter, newMockTaskRepo())

	deferral := models.CeilingDeferral{Kind: models.CeilingLaunchStart, Payload: map[string]interface{}{}}
	reasonCode, detail, drop := svc.evaluateCeilingDropReasons(ctx, task, deferral)
	require.True(t, drop, "a legacy sessionless start can never become valid, so it must terminate")
	require.Equal(t, ceilingReasonSuperseded, reasonCode)
	require.Contains(t, detail, "legacy sessionless")
}

// @covers AC-AGENTS-SESSION-CEILING-001.12
func TestEvaluateCeilingDropReasons_AmbiguousBindingIsTerminal(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "drop-ambiguous", "amb-session", models.TaskSessionStateCreated)
	require.NoError(t, repo.SetTaskMetadataKey(ctx, "drop-ambiguous", models.MetaKeyWorkflowSessionRoute, models.WorkflowSessionRoute{
		OperationID:       "route-ambiguous",
		DestinationStepID: "step-x",
		EntryIdentity:     "entry:00000000000000000001",
		TargetKind:        "new_session",
		Phase:             "committed",
	}))
	task, err := repo.GetTask(ctx, "drop-ambiguous")
	require.NoError(t, err)
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())

	deferral := models.CeilingDeferral{Kind: models.CeilingLaunchStartCreated, Payload: map[string]interface{}{
		metaKeySessionID:      "amb-session",
		metaKeyWorkflowStepID: "step-x",
	}}
	reasonCode, detail, drop := svc.evaluateCeilingDropReasons(ctx, task, deferral)
	require.True(t, drop, "an unresolvable workflow binding can never become valid, so it must terminate")
	require.Equal(t, ceilingReasonSuperseded, reasonCode)
	require.Contains(t, detail, "ambiguous")
}

func TestEvaluateCeilingDropReasons_StepReadErrorIsRetried(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSessionWithStep(t, repo, "keep-unreadable", "keep-unreadable-session", "step-unreadable")
	task, err := repo.GetTask(ctx, "keep-unreadable")
	require.NoError(t, err)

	stepGetter := newMockStepGetter()
	stepGetter.steps["step-unreadable"] = &wfmodels.WorkflowStep{ID: "step-unreadable", WorkflowID: "wf1"}
	stepGetter.getStepFunc = func(ctx context.Context, stepID string) (*wfmodels.WorkflowStep, error) {
		if stepID == "step-unreadable" {
			return nil, errors.New("workflow store unavailable")
		}
		return stepGetter.steps[stepID], nil
	}
	svc := createTestService(repo, stepGetter, newMockTaskRepo())
	entryIdentity := svc.workflowEntryIdentity(ctx, task.ID)
	binding := models.CeilingWorkflowEntryBinding{
		WorkflowID:        "wf1",
		DestinationStepID: "step-unreadable",
		RouteOperationID:  workflowSessionRouteID(task.ID, "step-unreadable", entryIdentity, nil, svc.resolveStepProfileSessionStartPolicy(stepGetter.steps["step-unreadable"])),
		EntryIdentity:     entryIdentity,
	}
	deferral := models.CeilingDeferral{
		Kind: models.CeilingLaunchStartCreated,
		Payload: map[string]interface{}{
			metaKeySessionID:                    "keep-unreadable-session",
			metaKeyWorkflowStepID:               "step-unreadable",
			models.CeilingLaunchEntryBindingKey: ceilingEntryBindingValue(binding),
		},
	}

	_, detail, drop := svc.evaluateCeilingDropReasons(ctx, task, deferral)
	require.False(t, drop, "a step read failure is uncertainty, so the record must be kept for a later sweep")
	require.Empty(t, detail)
}

func TestEvaluateCeilingDropReasons_BindingDerivationStepReadErrorIsRetried(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "keep-derive-unreadable", "derive-session", models.TaskSessionStateCreated)
	require.NoError(t, repo.SetTaskMetadataKey(ctx, "keep-derive-unreadable", models.MetaKeyWorkflowSessionRoute, models.WorkflowSessionRoute{
		OperationID:       "route-derive",
		DestinationStepID: "step-derive",
		EntryIdentity:     "entry:00000000000000000001",
		TargetKind:        "new_session",
		DestinationID:     "derive-session",
		Phase:             "committed",
	}))
	task, err := repo.GetTask(ctx, "keep-derive-unreadable")
	require.NoError(t, err)

	stepGetter := newMockStepGetter()
	stepGetter.getStepFunc = func(ctx context.Context, stepID string) (*wfmodels.WorkflowStep, error) {
		return nil, errors.New("workflow store unavailable")
	}
	svc := createTestService(repo, stepGetter, newMockTaskRepo())

	deferral := models.CeilingDeferral{Kind: models.CeilingLaunchStartCreated, Payload: map[string]interface{}{
		metaKeySessionID:      "derive-session",
		metaKeyWorkflowStepID: "step-derive",
	}}
	_, detail, drop := svc.evaluateCeilingDropReasons(ctx, task, deferral)
	require.False(t, drop, "a step read failure while deriving the binding is uncertainty, so the record must be kept")
	require.Empty(t, detail)
}

func TestEvaluateCeilingDropReasons_IdentityReadErrorIsRetried(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSessionWithStep(t, repo, "keep-identity-unreadable", "identity-session", "step-identity")
	task, err := repo.GetTask(ctx, "keep-identity-unreadable")
	require.NoError(t, err)

	stepGetter := newMockStepGetter()
	stepGetter.steps["step-identity"] = &wfmodels.WorkflowStep{ID: "step-identity", WorkflowID: "wf1"}
	svc := createTestService(repo, stepGetter, newMockTaskRepo())
	entryIdentity := svc.workflowEntryIdentity(ctx, task.ID)
	binding := models.CeilingWorkflowEntryBinding{
		WorkflowID:        "wf1",
		DestinationStepID: "step-identity",
		RouteOperationID:  workflowSessionRouteID(task.ID, "step-identity", entryIdentity, nil, svc.resolveStepProfileSessionStartPolicy(stepGetter.steps["step-identity"])),
		EntryIdentity:     entryIdentity,
	}
	deferral := models.CeilingDeferral{
		Kind: models.CeilingLaunchStartCreated,
		Payload: map[string]interface{}{
			metaKeySessionID:                    "identity-session",
			metaKeyWorkflowStepID:               "step-identity",
			models.CeilingLaunchEntryBindingKey: ceilingEntryBindingValue(binding),
		},
	}
	failing := &workflowStartPromptTransitionReadFailureRepository{Repository: repo, err: errors.New("transition ledger unavailable")}
	svc.repo = failing

	_, detail, drop := svc.evaluateCeilingDropReasons(ctx, task, deferral)
	require.False(t, drop, "an entry identity read failure is uncertainty, so the record must be kept")
	require.Empty(t, detail)
}

// @covers AC-AGENTS-SESSION-CEILING-001.12
func TestCeilingSweepDropsALegacySessionlessStart(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskWithoutSession(t, repo, "sweep-legacy-start", "step-auto")
	// The committed route names another step, so the sweep cannot derive a
	// binding for this legacy record and validation sees it sessionless.
	require.NoError(t, repo.SetTaskMetadataKey(ctx, "sweep-legacy-start", models.MetaKeyWorkflowSessionRoute, models.WorkflowSessionRoute{
		OperationID:       "route-legacy",
		DestinationStepID: "step-routed",
		EntryIdentity:     "entry:00000000000000000001",
		TargetKind:        "new_session",
		Phase:             "committed",
	}))
	stepGetter := newMockStepGetter()
	stepGetter.steps["step-auto"] = &wfmodels.WorkflowStep{ID: "step-auto", WorkflowID: "wf1",
		Events: wfmodels.StepEvents{OnEnter: []wfmodels.OnEnterAction{{Type: wfmodels.OnEnterAutoStartAgent}}}}
	agent := &mockAgentManager{repoForExecutionLookup: repo}
	tasks := newMockTaskRepo()
	seedMockTaskState(tasks, "sweep-legacy-start", v1.TaskStateScheduling)
	svc := createTestServiceWithScheduler(repo, stepGetter, tasks, agent)
	svc.sessionCeiling = newSessionCeilingController(unlimitedSessionCeiling, nil, nil)
	record := models.CeilingRecordKeys(models.CeilingDeferral{
		Kind:       models.CeilingLaunchStart,
		Payload:    map[string]interface{}{metaKeyPrompt: "start"},
		Origin:     string(launchOriginAutomatic),
		ReasonCode: ceilingReasonRefused,
		QueuedAt:   time.Now().UTC(),
	})
	_, _, err := repo.SetTaskDeferredLaunchIfUnchanged(ctx, "sweep-legacy-start", tasksqlite.AbsentDeferredLaunch(), record)
	require.NoError(t, err)

	svc.drainDeferredCeilingLaunches(ctx)

	require.False(t, models.HasCeilingDeferredIntent(&models.Task{Metadata: map[string]interface{}{
		models.MetaKeyDeferredLaunch: deferredLaunchOf(t, svc, "sweep-legacy-start"),
	}}), "a start that can never become valid must be dropped instead of kept for every later sweep")
	agent.mu.Lock()
	defer agent.mu.Unlock()
	require.Empty(t, agent.setExecutionDescriptionCalls, "a dropped start must not launch")
}
