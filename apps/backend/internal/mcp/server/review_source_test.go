package mcp

import (
	"testing"

	ws "github.com/kandev/kandev/pkg/websocket"
	"github.com/stretchr/testify/require"
)

func TestReviewSourceToolsForwardToTaskReview(t *testing.T) {
	for _, tc := range []struct {
		tool, action string
		args         map[string]interface{}
	}{
		{"run_code_review_kandev", ws.ActionTaskReviewRun, map[string]interface{}{"worktree_path": "D:/gate/slot", "base_ref": "origin/main", "head_ref": "HEAD"}},
		{"set_review_source_kandev", "task.review.source.set", map[string]interface{}{"worktree_path": "D:/gate/slot", "base_ref": "origin/main"}},
		{"get_code_review_kandev", ws.ActionTaskReviewGet, nil},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			backend := &testBackend{response: map[string]interface{}{"accepted": true}}
			s := newTaskModeServer(t, backend, "task-current")
			result := callTool(t, s, tc.tool, tc.args)
			require.False(t, result.IsError)
			require.Equal(t, tc.action, backend.lastAction)
			payload, ok := backend.lastPayload.(map[string]interface{})
			require.True(t, ok)
			require.Equal(t, "task-current", payload["task_id"])
			for key, value := range tc.args {
				require.Equal(t, value, payload[key])
			}
		})
	}
}

func TestReviewSourceToolModes(t *testing.T) {
	for _, mode := range []string{ModeTask, ModeOffice, ModeExternal, ModeConfig, ModeAutomation} {
		t.Run(mode, func(t *testing.T) {
			s := New(&testBackend{}, "session-1", "task-1", 10005, newTestLogger(t), "", false, mode)
			for _, name := range []string{"run_code_review_kandev", "set_review_source_kandev", "get_code_review_kandev"} {
				_, present := s.mcpServer.ListTools()[name]
				require.Equal(t, mode == ModeTask || mode == ModeOffice || mode == ModeExternal, present, name)
			}
		})
	}
}
