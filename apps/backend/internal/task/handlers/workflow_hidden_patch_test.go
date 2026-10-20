package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/dto"
	"github.com/kandev/kandev/internal/task/models"
	ws "github.com/kandev/kandev/pkg/websocket"
)

type workflowHiddenPatchResult struct {
	ok       bool
	status   int
	wsCode   string
	workflow dto.WorkflowDTO
}

func (f *workflowTransportFixture) patchWorkflow(t *testing.T, transport, id, payload string) workflowHiddenPatchResult {
	t.Helper()
	var result workflowHiddenPatchResult
	if transport == "HTTP" {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/workflows/"+id, strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		f.http.ServeHTTP(rec, req)
		result.status = rec.Code
		if rec.Code == http.StatusOK {
			result.ok = true
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result.workflow))
		}
		return result
	}
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(payload), &body))
	body["id"] = id
	response, err := f.ws.Dispatch(context.Background(), wsWorkflowRequest(t, ws.ActionWorkflowUpdate, body))
	require.NoError(t, err)
	if response.Type != ws.MessageTypeResponse {
		var errPayload ws.ErrorPayload
		require.NoError(t, json.Unmarshal(response.Payload, &errPayload))
		result.wsCode = errPayload.Code
		return result
	}
	result.ok = true
	wsWorkflowResponse(t, response, &result.workflow)
	return result
}

func (f *workflowTransportFixture) hiddenEvents() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	values := make([]bool, 0, len(f.seen))
	for _, event := range f.seen {
		data := event.Data.(map[string]interface{})
		values = append(values, data["hidden"].(bool))
	}
	return values
}

func registeredWorkflowHiddenPatch(t *testing.T, transport string) {
	t.Helper()
	ctx := context.Background()

	t.Run("hide_then_unhide_round_trip", func(t *testing.T) {
		f := registeredWorkflowFixtures(t)[0]
		require.NoError(t, f.repo.CreateTask(ctx, &models.Task{
			ID: "hidden-patch-task", WorkspaceID: "transport-ws", WorkflowID: "transport-fields", Title: "keep me",
		}))
		before, err := f.repo.GetWorkflow(ctx, "transport-fields")
		require.NoError(t, err)
		require.False(t, before.Hidden)

		hidden := f.patchWorkflow(t, transport, "transport-fields", `{"hidden":true}`)
		require.True(t, hidden.ok, "hide request failed: %+v", hidden)
		require.True(t, hidden.workflow.Hidden)
		stored, err := f.repo.GetWorkflow(ctx, "transport-fields")
		require.NoError(t, err)
		require.True(t, stored.Hidden)
		require.Equal(t, before.Name, stored.Name)
		require.Equal(t, before.Prompt, stored.Prompt)
		require.Equal(t, before.Description, stored.Description)

		shown := f.patchWorkflow(t, transport, "transport-fields", `{"hidden":false}`)
		require.True(t, shown.ok, "unhide request failed: %+v", shown)
		require.False(t, shown.workflow.Hidden)
		stored, err = f.repo.GetWorkflow(ctx, "transport-fields")
		require.NoError(t, err)
		require.False(t, stored.Hidden)

		require.Equal(t, []bool{true, false}, f.hiddenEvents())
		task, err := f.repo.GetTask(ctx, "hidden-patch-task")
		require.NoError(t, err)
		require.Nil(t, task.ArchivedAt, "hiding a workflow must not archive its tasks")
		require.Equal(t, "transport-fields", task.WorkflowID)
	})

	t.Run("omitted_hidden_keeps_value_and_other_fields_apply", func(t *testing.T) {
		f := registeredWorkflowFixtures(t)[0]
		require.True(t, f.patchWorkflow(t, transport, "transport-fields", `{"hidden":true}`).ok)

		renamed := f.patchWorkflow(t, transport, "transport-fields", `{"name":"renamed","prompt":"new prompt"}`)
		require.True(t, renamed.ok, "rename failed: %+v", renamed)
		stored, err := f.repo.GetWorkflow(ctx, "transport-fields")
		require.NoError(t, err)
		require.True(t, stored.Hidden, "omitted hidden must keep the stored value")
		require.Equal(t, "renamed", stored.Name)
		require.Equal(t, "new prompt", stored.Prompt)
		require.Equal(t, "keep description", stored.Description)

		mixed := f.patchWorkflow(t, transport, "transport-fields", `{"hidden":false,"description":"mixed"}`)
		require.True(t, mixed.ok, "mixed update failed: %+v", mixed)
		stored, err = f.repo.GetWorkflow(ctx, "transport-fields")
		require.NoError(t, err)
		require.False(t, stored.Hidden)
		require.Equal(t, "mixed", stored.Description)
		require.Equal(t, "renamed", stored.Name)
		require.Equal(t, []bool{true, true, false}, f.hiddenEvents())
	})

	t.Run("system_workflow_visibility_is_rejected", func(t *testing.T) {
		f := registeredWorkflowFixtures(t)[0]
		systemID, err := f.repo.EnsureRoutineWorkflow(ctx, "transport-ws")
		require.NoError(t, err)
		before, err := f.repo.GetWorkflow(ctx, systemID)
		require.NoError(t, err)
		require.True(t, before.Hidden, "routine is a hidden built-in workflow")

		rejected := f.patchWorkflow(t, transport, systemID, `{"hidden":false,"name":"exposed"}`)
		require.False(t, rejected.ok)
		if transport == "HTTP" {
			require.Equal(t, http.StatusBadRequest, rejected.status)
		} else {
			require.Equal(t, ws.ErrorCodeValidation, rejected.wsCode)
		}
		stored, err := f.repo.GetWorkflow(ctx, systemID)
		require.NoError(t, err)
		require.True(t, stored.Hidden)
		require.Equal(t, before.Name, stored.Name, "a rejected request must not apply other fields")
		require.Empty(t, f.hiddenEvents())

		same := f.patchWorkflow(t, transport, systemID, `{"hidden":true}`)
		require.True(t, same.ok, "supplying the unchanged value is a no-op: %+v", same)
		require.True(t, same.workflow.Hidden)
	})
}

// @covers AC-TASKS-WORKFLOW-VISIBILITY-PATCH-001.1, AC-TASKS-WORKFLOW-VISIBILITY-PATCH-001.2, AC-TASKS-WORKFLOW-VISIBILITY-PATCH-001.3, AC-TASKS-WORKFLOW-VISIBILITY-PATCH-001.4
func TestRegisteredWorkflowHiddenPatchHTTP(t *testing.T) { registeredWorkflowHiddenPatch(t, "HTTP") }

// @covers AC-TASKS-WORKFLOW-VISIBILITY-PATCH-001.1, AC-TASKS-WORKFLOW-VISIBILITY-PATCH-001.2, AC-TASKS-WORKFLOW-VISIBILITY-PATCH-001.3, AC-TASKS-WORKFLOW-VISIBILITY-PATCH-001.4
func TestRegisteredWorkflowHiddenPatchWS(t *testing.T) { registeredWorkflowHiddenPatch(t, "WS") }
