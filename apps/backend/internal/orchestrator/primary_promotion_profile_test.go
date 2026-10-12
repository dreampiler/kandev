package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
)

func TestAutoPromotionPreservesLogicalProfileAndNewestSession(t *testing.T) {
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, candidateProfile, stepProfile string
		candidateTime                       time.Time
		wantPromotion                       bool
	}{
		{"wrong concrete profile", "concrete-flash", "", now.Add(time.Hour), false},
		{"workflow mismatch", "dynamic-control", "other-step-profile", now.Add(time.Hour), false},
		{"stale predecessor", "dynamic-control", "", now.Add(-24 * time.Hour), false},
		{"same logical successor", "dynamic-control", "dynamic-control", now.Add(time.Hour), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo := setupTestRepo(t)
			seedTaskAndSession(t, repo, "task", "failed", models.TaskSessionStateFailed)
			failed, err := repo.GetTaskSession(ctx, "failed")
			require.NoError(t, err)
			failed.AgentProfileID = "dynamic-control"
			failed.ExecutionProfileID = "concrete-flash"
			failed.StartedAt = now
			require.NoError(t, repo.UpdateTaskSession(ctx, failed))
			require.NoError(t, repo.SetSessionPrimary(ctx, failed.ID))
			require.NoError(t, repo.CreateTaskSession(ctx, &models.TaskSession{
				ID: "candidate", TaskID: "task", State: models.TaskSessionStateWaitingForInput,
				AgentProfileID: tc.candidateProfile, ExecutionProfileID: "different-concrete",
				StartedAt: tc.candidateTime, UpdatedAt: tc.candidateTime,
			}))
			steps := newMockStepGetter()
			if tc.stepProfile != "" {
				task, err := repo.GetTask(ctx, "task")
				require.NoError(t, err)
				task.WorkflowStepID = "step"
				require.NoError(t, repo.UpdateTask(ctx, task))
				steps.steps["step"] = &wfmodels.WorkflowStep{ID: "step", AgentProfileID: tc.stepProfile}
			}
			svc := createTestService(repo, steps, newMockTaskRepo())
			require.NoError(t, svc.messageQueue.SetAutoRun(ctx, "failed", false))
			require.NoError(t, svc.messageQueue.SetAutoRun(ctx, "candidate", false))
			_, err = svc.messageQueue.QueueMessage(ctx, "failed", "task", "keep ownership", "", messagequeue.QueuedByUser, false, nil)
			require.NoError(t, err)

			svc.maybePromotePrimary(ctx, "task", "failed", models.TaskSessionStateFailed)

			primary, err := repo.GetPrimarySessionByTaskID(ctx, "task")
			require.NoError(t, err)
			want := "failed"
			if tc.wantPromotion {
				want = "candidate"
			}
			require.Equal(t, want, primary.ID)
			entries, _, err := svc.messageQueue.SnapshotSession(ctx, want)
			require.NoError(t, err)
			require.Len(t, entries, 1, "queue follows only the eligible primary")
			require.Equal(t, "keep ownership", entries[0].Content)
		})
	}
}

func TestAutoPromotionDoesNotPreferRunningPredecessorOverNewestProfileSession(t *testing.T) {
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		state models.TaskSessionState
		want  string
	}{
		{"latest waiting wins", models.TaskSessionStateWaitingForInput, "newest"},
		{"latest failed keeps recovery ownership", models.TaskSessionStateFailed, ""},
		{"latest completed cannot revive predecessor", models.TaskSessionStateCompleted, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo := setupTestRepo(t)
			seedTaskAndSession(t, repo, "task", "failed", models.TaskSessionStateFailed)
			svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
			sessions := []*models.TaskSession{
				{ID: "failed", AgentProfileID: "logical", StartedAt: now, State: models.TaskSessionStateFailed},
				{ID: "older-running", AgentProfileID: "logical", StartedAt: now.Add(time.Hour), State: models.TaskSessionStateRunning},
				{ID: "newest", AgentProfileID: "logical", StartedAt: now.Add(2 * time.Hour), State: tc.state},
			}
			require.Equal(t, tc.want, svc.bestPrimarySessionCandidate(ctx, "task", sessions, "failed", true))
		})
	}
}

func TestAutoPromotionCannotIgnoreUnreadableWorkflowProfile(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "task", "failed", models.TaskSessionStateFailed)
	task, err := repo.GetTask(ctx, "task")
	require.NoError(t, err)
	task.WorkflowStepID = "missing-step"
	require.NoError(t, repo.UpdateTask(ctx, task))
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	sessions := []*models.TaskSession{
		{ID: "failed", AgentProfileID: "logical", StartedAt: now, State: models.TaskSessionStateFailed},
		{ID: "newest", AgentProfileID: "logical", StartedAt: now.Add(time.Hour), State: models.TaskSessionStateWaitingForInput},
	}
	require.Empty(t, svc.bestPrimarySessionCandidate(ctx, "task", sessions, "failed", true))
}
