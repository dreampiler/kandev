package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReorderChildTasksTrustedSenderAndSchema(t *testing.T) {
	backend := &testBackend{response: map[string]interface{}{"band": "queued"}}
	s := newTaskModeServer(t, backend, "parent")
	tool := s.mcpServer.ListTools()["reorder_child_tasks_kandev"]
	schema, err := json.Marshal(tool.Tool.InputSchema)
	require.NoError(t, err)
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(schema, &parsed))
	properties := parsed["properties"].(map[string]interface{})
	require.Len(t, properties, 2)
	require.NotContains(t, properties, "sender_task_id")
	require.Equal(t, []interface{}{"ordered_task_ids"}, parsed["required"])
	result := callTool(t, s, "reorder_child_tasks_kandev", map[string]interface{}{
		"ordered_task_ids": []interface{}{"b", "a"}, "placement": "front",
	})
	require.False(t, result.IsError)
	require.Equal(t, "mcp.reorder_child_tasks", backend.lastAction)
	payload := backend.lastPayload.(map[string]interface{})
	require.Equal(t, "parent", payload["sender_task_id"])
	require.Equal(t, []string{"b", "a"}, payload["ordered_task_ids"])
	require.Equal(t, "front", payload["placement"])
	require.Len(t, payload, 3)
	backend.lastAction = ""
	result = callTool(t, s, "reorder_child_tasks_kandev", map[string]interface{}{
		"ordered_task_ids": []interface{}{"a"}, "sender_task_id": "spoofed",
	})
	require.True(t, result.IsError)
	require.Empty(t, backend.lastAction)
}

func TestReorderChildTasksEmptyIDsDoNotForward(t *testing.T) {
	backend := &testBackend{}
	s := newTaskModeServer(t, backend, "parent")
	result := callTool(t, s, "reorder_child_tasks_kandev", map[string]interface{}{"ordered_task_ids": []interface{}{}})
	require.True(t, result.IsError)
	require.Empty(t, backend.lastAction)
}
