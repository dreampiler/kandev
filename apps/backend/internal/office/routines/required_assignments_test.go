package routines_test

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/office/models"
	"github.com/stretchr/testify/require"
)

type requiredAssignmentCreator struct {
	fakeTaskCreator
	required int
}

func (f *requiredAssignmentCreator) CreateOfficeTaskInWorkflowWithRequirements(ctx context.Context, ws, project, agent, workflow, title, description, routine string, required int) (string, error) {
	f.required = required
	return f.CreateOfficeTaskInWorkflow(ctx, ws, project, agent, workflow, title, description, routine)
}

func TestHeavyRoutinePreservesRequiredAssignments(t *testing.T) {
	svc := newTestRoutineService(t)
	ctx := context.Background()
	routine := &models.Routine{WorkspaceID: "ws-1", Name: "LD", Status: "active", AssigneeAgentProfileID: "agent-coord", ConcurrencyPolicy: "always_create", TaskTemplate: `{"title":"Discovery","description":"Existing evidence and result refs","required_subtask_count":3}`}
	require.NoError(t, svc.CreateRoutine(ctx, routine))
	creator := &requiredAssignmentCreator{}
	svc.SetWorkflowEnsurer(&fakeWorkflowEnsurer{})
	svc.SetTaskCreator(creator)
	run, err := svc.FireManual(ctx, routine.ID, nil)
	require.NoError(t, err)
	require.Equal(t, 3, creator.required)
	require.Equal(t, "task-routine-1", run.LinkedTaskID)
	require.Equal(t, "Existing evidence and result refs", creator.captured.description)
}

func TestHeavyRoutineDoesNotSilentlyDropUnsupportedRequiredAssignments(t *testing.T) {
	svc := newTestRoutineService(t)
	ctx := context.Background()
	routine := &models.Routine{WorkspaceID: "ws-1", Name: "LD", Status: "active", AssigneeAgentProfileID: "agent-coord", ConcurrencyPolicy: "always_create", TaskTemplate: `{"title":"Discovery","required_subtask_count":3}`}
	require.NoError(t, svc.CreateRoutine(ctx, routine))
	creator := &fakeTaskCreator{}
	svc.SetWorkflowEnsurer(&fakeWorkflowEnsurer{})
	svc.SetTaskCreator(creator)
	_, err := svc.FireManual(ctx, routine.ID, nil)
	require.ErrorContains(t, err, "required subtask assignments")
	require.Empty(t, creator.captured.title)
}
