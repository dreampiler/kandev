package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/events"
	"github.com/kandev/kandev/internal/events/bus"
	mcpprofile "github.com/kandev/kandev/internal/mcp/profile"
	mcpscope "github.com/kandev/kandev/internal/mcp/scope"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
	"github.com/kandev/kandev/internal/task/service"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	ws "github.com/kandev/kandev/pkg/websocket"
	"github.com/stretchr/testify/require"
)

type childReorderFixture struct {
	tasks     map[string]*models.Task
	band      []*models.Task
	orders    [][]string
	conflicts int
	onList    func()
}

func newChildReorderFixture() *childReorderFixture {
	f := &childReorderFixture{tasks: map[string]*models.Task{
		"parent": {ID: "parent", WorkspaceID: "workspace"},
	}}
	for i, id := range []string{"other", "a", "middle", "b"} {
		task := &models.Task{ID: id, ParentID: "parent", WorkspaceID: "workspace", WorkflowID: "workflow", WorkflowStepID: "step", WIPAdmitted: true, Position: i}
		if id == "other" || id == "middle" {
			task.ParentID = "elsewhere"
		}
		f.tasks[id] = task
		f.band = append(f.band, task)
	}
	return f
}

func (f *childReorderFixture) GetTask(_ context.Context, id string) (*models.Task, error) {
	if task := f.tasks[id]; task != nil {
		return task, nil
	}
	return nil, repoerrors.ErrTaskNotFound
}

func (f *childReorderFixture) ListTasks(context.Context, string) ([]*models.Task, error) {
	if f.onList != nil {
		f.onList()
	}
	return f.band, nil
}

func (f *childReorderFixture) ReorderStepTasks(_ context.Context, step, band string, ids []string) (*service.ReorderStepTasksResult, error) {
	f.orders = append(f.orders, ids)
	result := &service.ReorderStepTasksResult{WorkflowStepID: step, Tasks: f.band, Revision: 1}
	if f.conflicts > 0 {
		f.conflicts--
		return result, repoerrors.ErrStepChanged
	}
	for i, id := range ids {
		f.tasks[id].Position = i
	}
	return result, nil
}

func TestHandleReorderChildTasksPlacement(t *testing.T) {
	for _, tc := range []struct {
		placement string
		want      []string
	}{
		{"", []string{"other", "b", "middle", "a"}},
		{"front", []string{"b", "a", "other", "middle"}},
	} {
		t.Run(tc.placement, func(t *testing.T) {
			f := newChildReorderFixture()
			h := &Handlers{childTaskReorderer: f}
			response, err := h.handleReorderChildTasks(context.Background(), makeWSMessage(t, ws.ActionMCPReorderChildTasks, map[string]interface{}{
				"sender_task_id": "parent", "ordered_task_ids": []string{"b", "a"}, "placement": tc.placement,
			}))
			require.NoError(t, err)
			require.Equal(t, ws.MessageTypeResponse, response.Type)
			require.Equal(t, [][]string{tc.want}, f.orders)
		})
	}
}

func TestHandleReorderChildTasksRejectsInvalidTargets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*childReorderFixture)
		ids    []string
	}{
		{"self", nil, []string{"parent"}},
		{"unrelated", nil, []string{"other"}},
		{"grandchild", func(f *childReorderFixture) { f.tasks["a"].ParentID = "b" }, []string{"a"}},
		{"workspace", func(f *childReorderFixture) { f.tasks["a"].WorkspaceID = "different" }, []string{"a"}},
		{"step", func(f *childReorderFixture) { f.tasks["a"].WorkflowStepID = "different" }, []string{"b", "a"}},
		{"band", func(f *childReorderFixture) { f.tasks["a"].WIPAdmitted = false; f.tasks["a"].QueuedForStepID = "step" }, []string{"b", "a"}},
		{"duplicate", nil, []string{"a", "a"}},
		{"empty", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newChildReorderFixture()
			if tc.mutate != nil {
				tc.mutate(f)
			}
			h := &Handlers{childTaskReorderer: f}
			response, err := h.handleReorderChildTasks(context.Background(), makeWSMessage(t, ws.ActionMCPReorderChildTasks, map[string]interface{}{
				"sender_task_id": "parent", "ordered_task_ids": tc.ids,
			}))
			require.NoError(t, err)
			require.Equal(t, ws.MessageTypeError, response.Type)
			require.Empty(t, f.orders)
		})
	}
}

func TestHandleReorderChildTasksConflictRetry(t *testing.T) {
	for _, count := range []int{1, 2} {
		f := newChildReorderFixture()
		f.conflicts = count
		h := &Handlers{childTaskReorderer: f}
		response, err := h.handleReorderChildTasks(context.Background(), makeWSMessage(t, ws.ActionMCPReorderChildTasks, map[string]interface{}{
			"sender_task_id": "parent", "ordered_task_ids": []string{"b", "a"},
		}))
		require.NoError(t, err)
		require.Len(t, f.orders, 2)
		if count == 1 {
			require.Equal(t, ws.MessageTypeResponse, response.Type)
		} else {
			require.Equal(t, ws.MessageTypeError, response.Type)
		}
	}
}

func TestHandleReorderChildTasksRevalidatesParentInBandSnapshot(t *testing.T) {
	f := newChildReorderFixture()
	f.onList = func() { f.tasks["a"].ParentID = "different" }
	h := &Handlers{childTaskReorderer: f}
	response, err := h.handleReorderChildTasks(context.Background(), makeWSMessage(t, ws.ActionMCPReorderChildTasks, map[string]interface{}{
		"sender_task_id": "parent", "ordered_task_ids": []string{"a"},
	}))
	require.NoError(t, err)
	require.Equal(t, ws.MessageTypeError, response.Type)
	require.Empty(t, f.orders)
}

func TestReorderChildTasksRegisteredEntryPersistsPublishesAndChangesPullOrder(t *testing.T) {
	ctx := context.Background()
	svc, repo, eventBus := newTestTaskServiceWithEventBus(t)
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: "order-workspace", Name: "Order"}))
	require.NoError(t, repo.CreateWorkflow(ctx, &models.Workflow{ID: "order-workflow", WorkspaceID: "order-workspace", Name: "Order"}))
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	_, err := repo.DB().Exec(`INSERT INTO workflow_steps (id, workflow_id, name, position, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"order-step", "order-workflow", "Order", 0, now, now)
	require.NoError(t, err)
	svc.SetWorkflowStepGetter(&staticWorkflowStepGetter{steps: map[string]*wfmodels.WorkflowStep{
		"order-step": {ID: "order-step", WorkflowID: "order-workflow"},
	}})
	require.NoError(t, repo.CreateTask(ctx, &models.Task{ID: "order-parent", WorkspaceID: "order-workspace", WorkflowID: "order-workflow", Title: "Parent"}))
	for i, id := range []string{"order-other", "order-a", "order-b"} {
		parentID := "order-parent"
		if i == 0 {
			parentID = ""
		}
		require.NoError(t, repo.CreateTask(ctx, &models.Task{
			ID: id, ParentID: parentID, WorkspaceID: "order-workspace", WorkflowID: "order-workflow", WorkflowStepID: "order-step",
			Title: id, Position: i, QueuedForStepID: "order-step", QueuedAt: &now,
		}))
	}
	published := make(chan *bus.Event, 1)
	sub, err := eventBus.Subscribe(events.TaskReordered, func(_ context.Context, event *bus.Event) error { published <- event; return nil })
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	h := NewHandlers(svc, nil, nil, nil, nil, repo, repo, nil, nil, nil, nil, nil, testLogger(t))
	dispatcher := ws.NewDispatcher()
	h.RegisterHandlers(dispatcher)
	msg := makeWSMessage(t, ws.ActionMCPReorderChildTasks, map[string]interface{}{
		"sender_task_id": "order-parent", "ordered_task_ids": []string{"order-b", "order-a"}, "placement": "front",
	})
	response, err := dispatcher.Dispatch(ctx, msg)
	require.NoError(t, err)
	require.Equal(t, ws.MessageTypeResponse, response.Type)
	for i, id := range []string{"order-b", "order-a", "order-other"} {
		task, readErr := repo.GetTask(ctx, id)
		require.NoError(t, readErr)
		require.Equal(t, i, task.Position)
	}
	select {
	case event := <-published:
		require.Equal(t, events.TaskReordered, event.Type)
	case <-time.After(5 * time.Second):
		t.Fatal("task.reordered was not published")
	}
	candidate, err := repo.NextQueuedTaskForStepExcluding(ctx, "order-step", "order-step", nil)
	require.NoError(t, err)
	require.Equal(t, "order-b", candidate.ID)
}

func TestReorderChildTasksRejectsRawAutomationAction(t *testing.T) {
	h := &Handlers{logger: testLogger(t).WithFields()}
	dispatcher := ws.NewDispatcher()
	h.RegisterHandlers(dispatcher)
	ctx := mcpscope.WithPrincipal(context.Background(), mcpscope.Principal{AutomationID: "automation", Surface: mcpprofile.SurfaceAutomation})
	response, err := dispatcher.Dispatch(ctx, makeWSMessage(t, ws.ActionMCPReorderChildTasks, map[string]interface{}{
		"sender_task_id": "parent", "ordered_task_ids": []string{"a"},
	}))
	require.NoError(t, err)
	require.Equal(t, ws.MessageTypeError, response.Type)
}
