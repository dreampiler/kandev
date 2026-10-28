package sqlite

import (
	"context"
	"fmt"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	"github.com/stretchr/testify/require"
)

func TestRoutineRequiredAssignmentsBlockEveryCompletionWrite(t *testing.T) {
	repo := newRepoForBuiltinWorkflowTests(t)
	ctx := context.Background()
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws-required", Name: "Office"}))
	_, err := repo.EnsureOfficeWorkflow(ctx, "ws-required")
	require.NoError(t, err)
	wf, err := repo.EnsureRoutineWorkflow(ctx, "ws-required")
	require.NoError(t, err)
	var start, done string
	require.NoError(t, repo.db.QueryRowContext(ctx, `SELECT id FROM workflow_steps WHERE workflow_id=? AND is_start_step=1`, wf).Scan(&start))
	require.NoError(t, repo.db.QueryRowContext(ctx, `SELECT id FROM workflow_steps WHERE workflow_id=? AND complete_task_on_enter=1`, wf).Scan(&done))
	parent := &models.Task{ID: "required-parent", WorkspaceID: "ws-required", WorkflowID: wf, WorkflowStepID: start, Title: "Required assignments", State: v1.TaskStateInProgress, Metadata: map[string]interface{}{"required_subtask_count": 3}}
	require.NoError(t, repo.CreateTask(ctx, parent))
	snapshot, err := repo.GetTaskCompletionGate(ctx, parent.ID)
	require.NoError(t, err)
	require.True(t, snapshot.Blocked)
	require.Contains(t, snapshot.Blockers, models.TaskCompletionBlocker{CriterionID: "required-subtask-assignments", Reason: "required_assignments_incomplete"})
	batch, err := repo.GetTaskCompletionGateSummaries(ctx, []string{parent.ID})
	require.NoError(t, err)
	require.True(t, batch.ByTaskID[parent.ID].Blocked)
	require.Equal(t, 0, batch.ByTaskID[parent.ID].CriteriaCount, "read-only assignment blockers do not create editable criteria")
	for _, mode := range []string{"state", "state-cas", "step"} {
		t.Run(mode, func(t *testing.T) {
			switch mode {
			case "state":
				err = repo.UpdateTaskState(ctx, parent.ID, v1.TaskStateCompleted)
			case "state-cas":
				_, _, err = repo.UpdateTaskStateIfCurrentIn(ctx, parent.ID, v1.TaskStateCompleted, []v1.TaskState{v1.TaskStateInProgress})
			case "step":
				copy := *parent
				copy.WorkflowStepID = done
				err = repo.UpdateTask(ctx, &copy)
			}
			require.ErrorContains(t, err, "required subtask assignments")
			current, readErr := repo.GetTask(ctx, parent.ID)
			require.NoError(t, readErr)
			require.Equal(t, v1.TaskStateInProgress, current.State)
			require.Equal(t, start, current.WorkflowStepID)
		})
	}
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("required-child-%d", i)
		require.NoError(t, repo.CreateTask(ctx, &models.Task{ID: id, WorkspaceID: parent.WorkspaceID, WorkflowID: wf, WorkflowStepID: start, ParentID: parent.ID, Title: id, State: v1.TaskStateInProgress}))
	}
	require.ErrorContains(t, repo.UpdateTaskState(ctx, parent.ID, v1.TaskStateCompleted), "required subtask assignments", "unassigned children are not assignments")
	_, err = repo.db.ExecContext(ctx, `UPDATE workflow_steps SET agent_profile_id='default-agent' WHERE id=?`, start)
	require.NoError(t, err)
	require.ErrorContains(t, repo.UpdateTaskState(ctx, parent.ID, v1.TaskStateCompleted), "required subtask assignments", "a workflow default is not a durable task-specific runner assignment")
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("required-child-%d", i)
		seatRow(t, repo, "runner-"+id, start, id, "runner", false)
	}
	require.NoError(t, repo.UpdateTaskState(ctx, parent.ID, v1.TaskStateCompleted))
	current, err := repo.GetTask(ctx, parent.ID)
	require.NoError(t, err)
	require.Equal(t, v1.TaskStateCompleted, current.State)
	snapshot, err = repo.GetTaskCompletionGate(ctx, parent.ID)
	require.NoError(t, err)
	require.False(t, snapshot.Blocked)
	// Already completed tasks remain terminal and are never reopened by validation.
	_, err = repo.db.ExecContext(ctx, `DELETE FROM workflow_step_participants WHERE task_id LIKE 'required-child-%'`)
	require.NoError(t, err)
	require.NoError(t, repo.UpdateTaskState(ctx, parent.ID, v1.TaskStateCompleted))
	batch, err = repo.GetTaskCompletionGateSummaries(ctx, []string{parent.ID})
	require.NoError(t, err)
	require.Nil(t, batch.ByTaskID[parent.ID], "completed tasks retain the legacy empty-gate read shape")
}
