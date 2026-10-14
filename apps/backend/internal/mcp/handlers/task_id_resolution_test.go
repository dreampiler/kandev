package handlers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpprofile "github.com/kandev/kandev/internal/mcp/profile"
	mcpscope "github.com/kandev/kandev/internal/mcp/scope"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	ws "github.com/kandev/kandev/pkg/websocket"
)

const (
	sharedPrefixTaskA = "15c6b3f2-0000-4000-8000-000000000001"
	sharedPrefixTaskB = "15c6b3f2-0000-4000-8000-000000000002"
	uniquePrefixTask  = "082e5aad-0000-4000-8000-000000000003"
)

func kanbanPrincipalCtx(workspaceID, taskID string) context.Context {
	return mcpscope.WithPrincipal(context.Background(), mcpscope.Principal{
		WorkspaceID:     workspaceID,
		CallerTaskID:    taskID,
		CallerSessionID: taskID + "-session",
		Surface:         mcpprofile.SurfaceKanbanTask,
	})
}

func newPrefixHandlers(t *testing.T) (*Handlers, *sqliterepo.Repository, string) {
	t.Helper()
	svc, repo := newTestTaskService(t)
	ctx := context.Background()
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws-prefix", Name: "Prefix"}))
	require.NoError(t, repo.CreateWorkflow(ctx, &models.Workflow{ID: "wf-prefix", WorkspaceID: "ws-prefix", Name: "Board"}))
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws-elsewhere", Name: "Elsewhere"}))
	for _, task := range []*models.Task{
		{ID: sharedPrefixTaskA, WorkspaceID: "ws-prefix", WorkflowID: "wf-prefix", Title: "First", State: v1.TaskStateInProgress},
		{ID: sharedPrefixTaskB, WorkspaceID: "ws-prefix", WorkflowID: "wf-prefix", Title: "Second", State: v1.TaskStateInProgress},
		{ID: uniquePrefixTask, WorkspaceID: "ws-prefix", WorkflowID: "wf-prefix", Title: "Unique", State: v1.TaskStateInProgress},
		{ID: "cafebabe-0000-4000-8000-000000000004", WorkspaceID: "ws-elsewhere", Title: "Other", State: v1.TaskStateInProgress},
	} {
		require.NoError(t, repo.CreateTask(ctx, task))
	}
	h := &Handlers{taskSvc: svc, logger: testLogger(t).WithFields()}
	return h, repo, "ws-prefix"
}

func msgTaskID(t *testing.T, msg *ws.Message) string {
	t.Helper()
	var fields map[string]string
	require.NoError(t, json.Unmarshal(msg.Payload, &fields))
	return fields["task_id"]
}

func TestExpandShortTaskRefs_ResolvesUniquePrefix(t *testing.T) {
	h, _, wsID := newPrefixHandlers(t)
	ctx := kanbanPrincipalCtx(wsID, sharedPrefixTaskA)
	msg := makeWSMessage(t, ws.ActionMCPArchiveTask, map[string]string{"task_id": "082e5aad"})
	expanded, errMsg := h.expandShortTaskRefs(ctx, msg)
	require.Nil(t, errMsg)
	require.NotNil(t, expanded)
	assert.NotSame(t, msg, expanded)
	assert.Equal(t, uniquePrefixTask, msgTaskID(t, expanded))
}

func TestExpandShortTaskRefs_RejectsAmbiguousPrefixWithCandidates(t *testing.T) {
	h, _, wsID := newPrefixHandlers(t)
	ctx := kanbanPrincipalCtx(wsID, sharedPrefixTaskA)
	msg := makeWSMessage(t, ws.ActionMCPArchiveTask, map[string]string{"task_id": "15c6b3f2"})
	expanded, errMsg := h.expandShortTaskRefs(ctx, msg)
	require.Nil(t, expanded)
	require.NotNil(t, errMsg)
	assertWSError(t, errMsg, ws.ErrorCodeValidation)
	var ep ws.ErrorPayload
	require.NoError(t, json.Unmarshal(errMsg.Payload, &ep))
	assert.Equal(t, "ambiguous_task_id_prefix", ep.Details["reason"])
	candidates, ok := ep.Details["candidates"].([]interface{})
	require.True(t, ok, "details must carry the candidate list")
	assert.Len(t, candidates, 2)
}

func TestExpandShortTaskRefs_LeavesFullUnknownAndUnownedValuesAlone(t *testing.T) {
	h, _, wsID := newPrefixHandlers(t)
	ctx := kanbanPrincipalCtx(wsID, sharedPrefixTaskA)

	full := makeWSMessage(t, ws.ActionMCPArchiveTask, map[string]string{"task_id": uniquePrefixTask})
	expanded, errMsg := h.expandShortTaskRefs(ctx, full)
	require.Nil(t, errMsg)
	assert.Same(t, full, expanded, "a full UUID is never rewritten")

	unknown := makeWSMessage(t, ws.ActionMCPArchiveTask, map[string]string{"task_id": "deadbeef"})
	expanded, errMsg = h.expandShortTaskRefs(ctx, unknown)
	require.Nil(t, errMsg)
	assert.Same(t, unknown, expanded, "an unmatched prefix keeps its value")

	noPrincipal := makeWSMessage(t, ws.ActionMCPArchiveTask, map[string]string{"task_id": "082e5aad"})
	expanded, errMsg = h.expandShortTaskRefs(context.Background(), noPrincipal)
	require.Nil(t, errMsg)
	assert.Same(t, noPrincipal, expanded, "resolution requires a workspace principal")

	foreign := makeWSMessage(t, ws.ActionMCPArchiveTask, map[string]string{"task_id": "cafebabe"})
	expanded, errMsg = h.expandShortTaskRefs(ctx, foreign)
	require.Nil(t, errMsg)
	assert.Same(t, foreign, expanded, "a prefix that lives in another workspace is not resolved")
}

// TestExpandShortTaskRefs_ResolvesDashedLongerPrefix pins the disambiguation
// remedy the ambiguity message offers: stored ids are dashed, so a prefix past
// the first group must resolve too.
func TestExpandShortTaskRefs_ResolvesDashedLongerPrefix(t *testing.T) {
	h, _, wsID := newPrefixHandlers(t)
	ctx := kanbanPrincipalCtx(wsID, sharedPrefixTaskA)

	ambiguous := makeWSMessage(t, ws.ActionMCPArchiveTask, map[string]string{"task_id": "15c6b3f2"})
	expanded, errMsg := h.expandShortTaskRefs(ctx, ambiguous)
	require.Nil(t, expanded)
	require.NotNil(t, errMsg)

	longer := makeWSMessage(t, ws.ActionMCPArchiveTask, map[string]string{"task_id": "15c6b3f2-0000-4000-8000-000000000002"})
	expanded, errMsg = h.expandShortTaskRefs(ctx, longer)
	require.Nil(t, errMsg)
	require.NotNil(t, expanded)
	assert.Equal(t, sharedPrefixTaskB, msgTaskID(t, expanded))
}

// TestHandleArchiveTask_ResolvesShortID is the completion-path check: a
// controller that knows only the first eight characters archives the task.
func TestHandleArchiveTask_ResolvesShortID(t *testing.T) {
	h, repo, wsID := newPrefixHandlers(t)
	ctx := kanbanPrincipalCtx(wsID, sharedPrefixTaskA)
	msg := makeWSMessage(t, ws.ActionMCPArchiveTask, map[string]string{"task_id": "082e5aad"})
	expanded, errMsg := h.expandShortTaskRefs(ctx, msg)
	require.Nil(t, errMsg)
	require.NotNil(t, expanded)

	resp, err := h.handleArchiveTask(ctx, expanded)
	require.NoError(t, err)
	require.Equal(t, ws.MessageTypeResponse, resp.Type)

	archived, err := repo.GetTask(ctx, uniquePrefixTask)
	require.NoError(t, err)
	require.NotNil(t, archived)
	assert.NotNil(t, archived.ArchivedAt, "the resolved task must actually be archived")
}

// TestGuardedDispatcherResolvesShortIDBeforeArchive pins the dispatcher wiring
// itself: a short id sent through the production guarded dispatcher must be
// expanded before the authorization guards, so reordering or dropping the
// expansion step would reintroduce the original short-id archive failure.
func TestGuardedDispatcherResolvesShortIDBeforeArchive(t *testing.T) {
	h, repo, wsID := newPrefixHandlers(t)
	dispatcher := ws.NewDispatcher()
	(&guardedMCPDispatcher{Dispatcher: dispatcher, handlers: h}).
		RegisterFunc(ws.ActionMCPArchiveTask, h.handleArchiveTask)

	ctx := kanbanPrincipalCtx(wsID, sharedPrefixTaskA)
	resp, err := dispatcher.Dispatch(ctx,
		makeWSMessage(t, ws.ActionMCPArchiveTask, map[string]string{"task_id": "082e5aad"}))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, ws.MessageTypeResponse, resp.Type)

	archived, err := repo.GetTask(ctx, uniquePrefixTask)
	require.NoError(t, err)
	require.NotNil(t, archived)
	assert.NotNil(t, archived.ArchivedAt, "the dispatcher must resolve the short id before the archive handler")
}
