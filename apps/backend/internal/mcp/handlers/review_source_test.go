package handlers

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	ws "github.com/kandev/kandev/pkg/websocket"
	"github.com/stretchr/testify/require"
)

func TestManualReviewForwardsExplicitSource(t *testing.T) {
	h, _, runner := newReviewHandlers(t)
	runner.run = &models.TaskReviewRun{ID: "run-1"}
	msg := makeWSMessage(t, ws.ActionTaskReviewRun, map[string]interface{}{
		"task_id": "task-x", "worktree_path": "D:/gate/slot", "base_ref": "origin/main", "head_ref": "HEAD",
	})
	resp, err := h.handleRunTaskReview(context.Background(), msg)
	require.NoError(t, err)
	requireWSSuccess(t, resp)
	require.Len(t, runner.launched, 1)
	b, err := json.Marshal(runner.launched[0])
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	require.Equal(t, "D:/gate/slot", got["worktree_path"])
	require.Equal(t, "origin/main", got["base_ref"])
	require.Equal(t, "HEAD", got["head_ref"])
}

func TestManualReviewCarriesWriterIdentity(t *testing.T) {
	h, _, runner := newReviewHandlers(t)
	seedReviewHandlerTask(t, h, "task-writer")
	ctx := context.Background()
	repo := h.sessionRepo.(interface {
		CreateTaskSession(context.Context, *models.TaskSession) error
	})
	require.NoError(t, repo.CreateTaskSession(ctx, &models.TaskSession{
		ID: "writer-session", TaskID: "task-writer", AgentProfileID: "writer-logical", ExecutionProfileID: "writer-concrete",
		IsPrimary: true, State: models.TaskSessionStateIdle, StartedAt: time.Now(), UpdatedAt: time.Now(),
	}))
	runner.run = &models.TaskReviewRun{ID: "run-writer"}
	for _, sessionID := range []string{"writer-session", ""} {
		resp, err := h.handleRunTaskReview(ctx, makeWSMessage(t, ws.ActionTaskReviewRun, map[string]any{"task_id": "task-writer", "session_id": sessionID}))
		require.NoError(t, err)
		requireWSSuccess(t, resp)
		req := runner.launched[len(runner.launched)-1]
		require.True(t, req.OriginRequired)
		require.NotNil(t, req.Origin)
		require.Equal(t, "writer-logical", req.Origin.ProfileID)
		require.Equal(t, "writer-concrete", req.Origin.ExecutionProfileID)
	}
}

func TestSetReviewSourcePersistsAndClearsTaskMetadata(t *testing.T) {
	h, _, _ := newReviewHandlers(t)
	seedReviewHandlerTask(t, h, "task-source")
	dir := t.TempDir()
	for _, args := range [][]string{{"init"}, {"-c", "user.name=Review Test", "-c", "user.email=review@example.test", "commit", "--allow-empty", "-m", "base"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	ctx := context.Background()
	_, err := h.taskSvc.UpdateTaskMetadata(ctx, "task-source", map[string]any{"keep": "unchanged"})
	require.NoError(t, err)
	resp, err := h.handleSetReviewSource(ctx, makeWSMessage(t, ws.ActionTaskReviewSourceSet,
		map[string]any{"task_id": "task-source", "worktree_path": dir, "base_ref": "HEAD"}))
	require.NoError(t, err)
	requireWSSuccess(t, resp)
	task, err := h.taskSvc.GetTask(ctx, "task-source")
	require.NoError(t, err)
	source := task.Metadata["review_source"].(map[string]interface{})
	require.Equal(t, dir, source["worktree_path"])
	require.Equal(t, "HEAD", source["base_ref"])
	require.Equal(t, "unchanged", task.Metadata["keep"])

	resp, err = h.handleSetReviewSource(ctx, makeWSMessage(t, ws.ActionTaskReviewSourceSet,
		map[string]any{"task_id": "task-source", "worktree_path": t.TempDir(), "base_ref": "HEAD"}))
	require.NoError(t, err)
	assertWSError(t, resp, ws.ErrorCodeValidation)
	task, err = h.taskSvc.GetTask(ctx, "task-source")
	require.NoError(t, err)
	require.Equal(t, dir, task.Metadata["review_source"].(map[string]interface{})["worktree_path"])

	resp, err = h.handleSetReviewSource(ctx, makeWSMessage(t, ws.ActionTaskReviewSourceSet,
		map[string]any{"task_id": "task-source", "clear": true}))
	require.NoError(t, err)
	requireWSSuccess(t, resp)
	task, err = h.taskSvc.GetTask(ctx, "task-source")
	require.NoError(t, err)
	require.Nil(t, task.Metadata["review_source"])
	require.Equal(t, "unchanged", task.Metadata["keep"])
}
