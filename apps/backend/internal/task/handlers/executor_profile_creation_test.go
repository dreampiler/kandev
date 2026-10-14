package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
	ws "github.com/kandev/kandev/pkg/websocket"
	"github.com/stretchr/testify/require"
)

func TestTaskCreationExecutorProfileReference(t *testing.T) {
	for _, transport := range []string{"rest", "ws"} {
		t.Run(transport, func(t *testing.T) {
			f := newAssigneeContractFixture(t, nil)
			ctx := context.Background()
			body := map[string]interface{}{"workspace_id": f.workspaceID, "workflow_id": f.workflowID, "workflow_step_id": f.stepID, "title": "Task", "executor_profile_id": "missing-profile"}
			data, err := json.Marshal(body)
			require.NoError(t, err)
			if transport == "rest" {
				rec := doCreateTask(f.handlers, string(data))
				require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
				var payload map[string]interface{}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
				require.Equal(t, "executor_profile_not_found", payload["error_code"])
				require.Contains(t, payload["error"], "missing-profile")
			} else {
				resp, err := f.handlers.wsCreateTask(ctx, &ws.Message{ID: "request", Action: ws.ActionTaskCreate, Payload: data})
				require.NoError(t, err)
				var payload ws.ErrorPayload
				require.NoError(t, json.Unmarshal(resp.Payload, &payload))
				require.Equal(t, ws.ErrorCodeNotFound, payload.Code)
				require.Contains(t, payload.Message, "missing-profile")
				require.Equal(t, "executor_profile_not_found", payload.Details["error_code"])
			}
			tasks, err := f.handlers.service.ListTasks(ctx, f.workflowID)
			require.NoError(t, err)
			require.Empty(t, tasks)
			require.NoError(t, f.repo.CreateExecutor(ctx, &models.Executor{ID: "profile-exec", Name: "Local", Type: models.ExecutorTypeLocal}))
			require.NoError(t, f.repo.CreateExecutorProfile(ctx, &models.ExecutorProfile{ID: "valid-profile", ExecutorID: "profile-exec", Name: "Valid"}))
			body["executor_profile_id"] = "valid-profile"
			data, err = json.Marshal(body)
			require.NoError(t, err)
			if transport == "rest" {
				rec := doCreateTask(f.handlers, string(data))
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			} else {
				resp, err := f.handlers.wsCreateTask(ctx, &ws.Message{ID: "control", Action: ws.ActionTaskCreate, Payload: data})
				require.NoError(t, err)
				require.NotEqual(t, ws.MessageTypeError, resp.Type, string(resp.Payload))
			}
		})
	}
}
