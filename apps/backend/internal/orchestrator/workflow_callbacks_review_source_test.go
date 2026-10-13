package orchestrator

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/steptelemetry"
	"github.com/kandev/kandev/internal/task/models"
)

func TestRunCodeReviewCallbackUsesWriterSessionForReviewSource(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "t1", "s1", "step-review")
	writer, err := repo.GetTaskSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	writer.AgentProfileID = "writer-profile"
	writer.ExecutionProfileID = "writer-concrete"
	writer.IsPrimary = true
	writer.Metadata = map[string]any{"review_source": map[string]any{"worktree_path": "D:/gate/slot", "base_ref": "origin/main"}}
	if err := repo.UpdateTaskSession(ctx, writer); err != nil {
		t.Fatal(err)
	}
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{})
	runner := &fakeReviewRunner{}
	svc.reviewRunner = runner
	in := reviewCallbackInput("")
	in.State.SessionID = ""
	ctx = steptelemetry.WithAttribution(ctx, steptelemetry.Attribution{SessionID: "s1"})
	if _, err := (&runCodeReviewCallback{svc: svc}).Execute(ctx, in); err != nil {
		t.Fatal(err)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("launches = %d", len(runner.requests))
	}
	req := runner.requests[0]
	if req.SessionID != "s1" || req.Trigger != models.ReviewTriggerWorkflowStep {
		t.Fatalf("request = %+v", req)
	}
	if !req.OriginRequired || req.Origin == nil || req.Origin.ExecutionProfileID != "writer-concrete" {
		t.Fatalf("writer exclusion = %+v", req)
	}
}
