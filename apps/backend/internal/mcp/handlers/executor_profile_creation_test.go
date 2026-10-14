package handlers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
	ws "github.com/kandev/kandev/pkg/websocket"
	"github.com/stretchr/testify/require"
)

func TestMCPExecutorProfileReference(t *testing.T) {
	for _, start := range []bool{false, true} {
		t.Run(map[bool]string{false: "deferred", true: "start"}[start], func(t *testing.T) {
			f := newAtomicityFixture(t)
			before := f.taskCount(t)
			resp := f.create(t, map[string]interface{}{"executor_profile_id": "missing-profile", "start_agent": start})
			require.Equal(t, ws.MessageTypeError, resp.Type)
			var payload ws.ErrorPayload
			require.NoError(t, json.Unmarshal(resp.Payload, &payload))
			require.Equal(t, ws.ErrorCodeNotFound, payload.Code)
			require.Contains(t, payload.Message, "missing-profile")
			require.Equal(t, "executor_profile_not_found", payload.Details["error_code"])
			require.Equal(t, before, f.taskCount(t))
			ctx := context.Background()
			require.NoError(t, f.repo.CreateExecutor(ctx, &models.Executor{ID: "profile-exec", Name: "Local", Type: models.ExecutorTypeLocal}))
			require.NoError(t, f.repo.CreateExecutorProfile(ctx, &models.ExecutorProfile{ID: "valid-profile", ExecutorID: "profile-exec", Name: "Valid"}))
			resp = f.create(t, map[string]interface{}{"executor_profile_id": "valid-profile", "start_agent": false})
			require.NotEqual(t, ws.MessageTypeError, resp.Type, string(resp.Payload))
			require.Equal(t, before+1, f.taskCount(t))
		})
	}
}
