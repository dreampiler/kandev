package orchestrator

import (
	"context"
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
