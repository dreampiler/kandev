package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// seedBoundStepTask seeds a workspace, workflow, task, and two sessions so a
// test can observe how the current step's durable session binding moves.
func seedBoundStepTask(t *testing.T, repo *sqliterepo.Repository) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{
		ID: "ws-bind", Name: "Bind", CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, repo.CreateWorkflow(ctx, &models.Workflow{
		ID: "wf-bind", WorkspaceID: "ws-bind", Name: "WF", CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "task-bind", WorkspaceID: "ws-bind", WorkflowID: "wf-bind",
		WorkflowStepID: "step-bind", Title: "T", State: v1.TaskStateInProgress,
		CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, repo.CreateTaskSession(ctx, &models.TaskSession{
		ID: "session-a", TaskID: "task-bind", IsPrimary: true, State: models.TaskSessionStateRunning,
		AgentProfileID: "profile-step", StartedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, repo.CreateTaskSession(ctx, &models.TaskSession{
		ID: "session-b", TaskID: "task-bind", IsPrimary: false, State: models.TaskSessionStateRunning,
		AgentProfileID: "profile-quota", StartedAt: now, UpdatedAt: now,
	}))
}

func newBoundStepService(repo *sqliterepo.Repository) *Service {
	getter := newMockStepGetter()
	getter.steps["step-bind"] = &wfmodels.WorkflowStep{
		ID: "step-bind", WorkflowID: "wf-bind", Name: "bind", AgentProfileID: "profile-step",
	}
	return &Service{logger: testLogger(), repo: repo, workflowStepGetter: getter}
}

// TestSetPrimarySessionMovesCurrentStepBinding covers the restart path: when a
// session that is not the one the step was entered with becomes the task's
// driving session, the promotion must move the durable step binding onto it so
// a later step-complete signal from that session is recognized.
func TestSetPrimarySessionMovesCurrentStepBinding(t *testing.T) {
	repo := setupTestRepo(t)
	seedBoundStepTask(t, repo)
	svc := newBoundStepService(repo)

	before, err := svc.BoundWorkflowStepSession(context.Background(), "task-bind", "step-bind")
	require.NoError(t, err)
	assert.Empty(t, before, "no binding is recorded before a promotion")

	require.NoError(t, svc.SetPrimarySession(context.Background(), "session-b"))

	bound, err := svc.BoundWorkflowStepSession(context.Background(), "task-bind", "step-bind")
	require.NoError(t, err)
	assert.Equal(t, "session-b", bound, "the promoted session must own the current step")
}

// TestBoundWorkflowStepSessionUnknownStep covers the no-binding answer: a step
// with no recorded binding resolves to "" without an error so the caller falls
// back to the primary-and-profile rule.
func TestBoundWorkflowStepSessionUnknownStep(t *testing.T) {
	repo := setupTestRepo(t)
	seedBoundStepTask(t, repo)
	svc := newBoundStepService(repo)

	bound, err := svc.BoundWorkflowStepSession(context.Background(), "task-bind", "step-absent")
	require.NoError(t, err)
	assert.Empty(t, bound)
}

// @covers AC-TASKS-WORKFLOW-PROFILE-SESSIONS-004.2
func TestRecoveryRecipientInitialSelectionPreservesOriginal(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedBoundStepTask(t, repo)
	svc := newBoundStepService(repo)
	require.NoError(t, repo.SetTaskMetadataKey(ctx, "task-bind", "workflow_initial_recipient_binding", map[string]interface{}{
		"initial_session_id": "session-a", "source_session_id": "session-a",
		"session_id": "session-b", "agent_profile_id": "profile-quota",
	}))
	step := &wfmodels.WorkflowStep{ID: "review", SessionTarget: &wfmodels.WorkflowSessionTarget{Kind: wfmodels.WorkflowSessionTargetInitial}}
	selected, err := svc.resolveWorkflowSessionTarget(ctx, "task-bind", step)
	require.NoError(t, err)
	require.Equal(t, "session-b", selected.session.ID)
	require.Equal(t, "profile-quota", selected.profileID)
	original, err := svc.originalTaskSession(ctx, "task-bind")
	require.NoError(t, err)
	require.Equal(t, "session-a", original.ID)
	preview, profile, err := svc.resolvePreviewInitialWorkflowSession(ctx, "task-bind")
	require.NoError(t, err)
	require.Equal(t, "session-b", preview.ID)
	require.Equal(t, "profile-quota", profile)
}

func TestRecoveryRecipientUsesRegisteredProfileWhenHistoricalProfileIsUnknown(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedBoundStepTask(t, repo)
	svc := newBoundStepService(repo)
	original, err := repo.GetTaskSession(ctx, "session-a")
	require.NoError(t, err)
	original.AgentProfileID = ""
	require.NoError(t, repo.UpdateTaskSession(ctx, original))
	snapshot := models.WorkflowInitialSessionSnapshot{SessionID: original.ID}
	require.NoError(t, repo.SetTaskMetadataKey(ctx, "task-bind", models.MetaKeyWorkflowInitialSession, snapshot))
	require.NoError(t, repo.SetTaskMetadataKey(ctx, "task-bind", models.MetaKeyWorkflowInitialRecipientBinding, models.WorkflowInitialRecipientBinding{
		InitialSessionID: original.ID, SourceSessionID: original.ID, SessionID: "session-b", AgentProfileID: "profile-quota",
	}))
	selected, profile, err := svc.resolveInitialWorkflowSession(ctx, "task-bind")
	require.NoError(t, err)
	require.Equal(t, "session-b", selected.ID)
	require.Equal(t, "profile-quota", profile)
	preview, profile, err := svc.resolvePreviewInitialWorkflowSession(ctx, "task-bind")
	require.NoError(t, err)
	require.Equal(t, "session-b", preview.ID)
	require.Equal(t, "profile-quota", profile)
	task, err := repo.GetTask(ctx, "task-bind")
	require.NoError(t, err)
	saved, ok := models.LoadWorkflowInitialSessionSnapshot(task.Metadata)
	require.True(t, ok)
	require.Equal(t, snapshot, saved)
}

func TestRecoveryRecipientRejectsSelectionSupersededBeforePreparation(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedBoundStepTask(t, repo)
	svc := newBoundStepService(repo)
	require.NoError(t, repo.SetTaskMetadataKey(ctx, "task-bind", "workflow_initial_recipient_binding", map[string]interface{}{
		"initial_session_id": "session-a", "source_session_id": "session-a",
		"session_id": "session-b", "agent_profile_id": "profile-quota",
	}))
	err := svc.persistWorkflowSessionRoute(ctx, "task-bind", models.WorkflowSessionRoute{
		OperationID: "new-entry", DestinationStepID: "review", TargetKind: "initial",
		AgentProfileID: "profile-step", DestinationID: "session-a", Phase: workflowSessionRoutePrepared,
	})
	require.Error(t, err, "a preflight selection made before recovery must not overwrite its successor")
}

func TestRecoveryRecipientUnavailableAndCorruptBindings(t *testing.T) {
	for _, reason := range []string{"terminal", "deleted", "profile changed", "corrupt"} {
		t.Run(reason, func(t *testing.T) {
			ctx := context.Background()
			repo := setupTestRepo(t)
			seedBoundStepTask(t, repo)
			svc := newBoundStepService(repo)
			require.NoError(t, repo.SetTaskMetadataKey(ctx, "task-bind", models.MetaKeyWorkflowInitialRecipientBinding, models.WorkflowInitialRecipientBinding{
				InitialSessionID: "session-a", SourceSessionID: "session-a", SessionID: "session-b", AgentProfileID: "profile-quota",
			}))
			session, err := repo.GetTaskSession(ctx, "session-b")
			require.NoError(t, err)
			switch reason {
			case "terminal", "deleted":
				require.NoError(t, repo.UpdateTaskSessionState(ctx, session.ID, models.TaskSessionStateFailed, "failed"))
				if reason == "deleted" {
					require.NoError(t, repo.DeleteTaskSession(ctx, session))
				}
			case "profile changed":
				session.AgentProfileID = "another-profile"
				require.NoError(t, repo.UpdateTaskSession(ctx, session))
			case "corrupt":
				require.NoError(t, repo.SetTaskMetadataKey(ctx, "task-bind", models.MetaKeyWorkflowInitialRecipientBinding, "bad"))
			}
			selected, profile, err := svc.resolveInitialWorkflowSession(ctx, "task-bind")
			if reason == "profile changed" || reason == "corrupt" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Nil(t, selected)
				require.Equal(t, "profile-quota", profile)
			}
		})
	}
}

// @covers AC-TASKS-WORKFLOW-PROFILE-SESSIONS-004.3
func TestRecoveryRecipientCommittedEntryKeepsRecordedProfile(t *testing.T) {
	ctx := context.Background()
	f := newProfileSwitchFixture(t, models.WorkflowProfileSessionStartPolicyReuse, models.WorkflowProfileSessionEndPolicyPark)
	recovery := *f.current
	recovery.ID, recovery.AgentProfileID = "recovered", "profile-b"
	require.NoError(t, f.repo.CreateTaskSession(ctx, &recovery))
	require.NoError(t, f.repo.SetTaskMetadataKey(ctx, "t1", "workflow_initial_recipient_binding", map[string]interface{}{
		"initial_session_id": f.current.ID, "source_session_id": f.current.ID,
		"session_id": recovery.ID, "agent_profile_id": recovery.AgentProfileID,
	}))
	step := &wfmodels.WorkflowStep{ID: "review", SessionTarget: &wfmodels.WorkflowSessionTarget{Kind: wfmodels.WorkflowSessionTargetInitial}}
	entry := f.svc.workflowEntryIdentity(ctx, "t1")
	route := models.WorkflowSessionRoute{
		OperationID:   workflowSessionRouteID("t1", step.ID, entry, step.SessionTarget, models.WorkflowProfileSessionStartPolicyReuse),
		EntryIdentity: entry, DestinationStepID: step.ID, DestinationID: f.current.ID,
		AgentProfileID: f.current.AgentProfileID, TargetKind: "initial", Phase: workflowSessionRouteCommitted,
	}
	require.NoError(t, f.repo.SetTaskMetadataKey(ctx, "t1", models.MetaKeyWorkflowSessionRoute, route))
	selected, _, err := f.svc.prepareExplicitWorkflowSession(ctx, "t1", f.current, step, nil)
	require.NoError(t, err)
	require.Equal(t, f.current.ID, selected.ID)
}

func TestRecoveryRecipientDoesNotChangeSourceStepProfileResolution(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedBoundStepTask(t, repo)
	svc := newBoundStepService(repo)
	source := svc.workflowStepGetter.(*mockStepGetter).steps["step-bind"]
	source.AgentProfileID = "profile-quota"
	step := &wfmodels.WorkflowStep{ID: "review", WorkflowID: "wf-bind", Position: 1,
		SessionTarget: &wfmodels.WorkflowSessionTarget{Kind: wfmodels.WorkflowSessionTargetStep, StepID: source.ID}}
	require.NoError(t, repo.SetTaskMetadataKey(ctx, "task-bind", models.MetaKeyWorkflowSessionRoute, models.WorkflowSessionRoute{
		OperationID: "same-entry", DestinationStepID: step.ID, TargetKind: "step", TargetStepID: source.ID,
		AgentProfileID: "profile-step", DestinationID: "session-a", Phase: workflowSessionRouteCommitted,
	}))
	selection, err := svc.resolveWorkflowEntryTarget(ctx, "task-bind", step, "same-entry")
	require.NoError(t, err)
	require.Equal(t, "profile-quota", selection.profileID)
}

func TestRecoveryRecipientKeepsDynamicLogicalProfile(t *testing.T) {
	ctx := context.Background()
	f := newProfileSwitchFixture(t, models.WorkflowProfileSessionStartPolicyReuse, models.WorkflowProfileSessionEndPolicyPark)
	require.NoError(t, f.repo.UpdateTaskSessionState(ctx, f.current.ID, models.TaskSessionStateFailed, "failed"))
	require.NoError(t, f.repo.DeleteExecutorRunningBySessionID(ctx, f.current.ID))
	f.agentMgr.isAgentRunning = false
	f.agentMgr.resolveProfileErr = errors.New("logical profile is not a concrete launch")
	f.svc.SetProfileExecutionResolver(newWorkflowDynamicProfileResolver(t, "dynamic-recovery", "concrete-recovery"))
	recovery := &models.TaskSession{ID: "dynamic-successor", TaskID: "t1", AgentProfileID: "dynamic-recovery",
		ExecutionProfileID: "concrete-recovery", State: models.TaskSessionStateWaitingForInput}
	require.NoError(t, f.repo.CreateTaskSession(ctx, recovery))
	require.NoError(t, f.svc.RecoverInitialWorkflowRecipient(ctx, recovery.ID, f.current.ID))
	task, err := f.repo.GetTask(ctx, "t1")
	require.NoError(t, err)
	binding, err := models.LoadWorkflowInitialRecipientBinding(task.Metadata)
	require.NoError(t, err)
	require.Equal(t, "dynamic-recovery", binding.AgentProfileID)
	saved, err := f.repo.GetTaskSession(ctx, recovery.ID)
	require.NoError(t, err)
	require.Equal(t, int64(0), saved.RouteGeneration)
	require.Equal(t, "concrete-recovery", saved.ExecutionProfileID)
}

// TestSetPrimarySessionOverwritesExistingStepBinding is the regression guard
// for the promotion path: when the step already has a binding on a replaced
// session, promoting a different session must overwrite it. The ordinary
// order-guarded upsert would compare equal operation identities and equal
// timestamps (a promotion is not a new workflow entry and does not touch the
// task row) and reject the write, leaving the binding stale.
func TestSetPrimarySessionOverwritesExistingStepBinding(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedBoundStepTask(t, repo)
	svc := newBoundStepService(repo)

	step := &wfmodels.WorkflowStep{
		ID: "step-bind", WorkflowID: "wf-bind", Name: "bind", AgentProfileID: "profile-step",
	}
	sessionA, err := repo.GetTaskSession(ctx, "session-a")
	require.NoError(t, err)
	require.NoError(t, svc.recordWorkflowSourceBinding(ctx, "task-bind", step, sessionA))

	bound, err := svc.BoundWorkflowStepSession(ctx, "task-bind", "step-bind")
	require.NoError(t, err)
	require.Equal(t, "session-a", bound)

	require.NoError(t, svc.SetPrimarySession(ctx, "session-b"))

	bound, err = svc.BoundWorkflowStepSession(ctx, "task-bind", "step-bind")
	require.NoError(t, err)
	assert.Equal(t, "session-b", bound, "the promotion must overwrite the existing binding")
}
