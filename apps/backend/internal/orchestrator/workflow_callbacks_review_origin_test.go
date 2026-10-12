package orchestrator

import (
	"context"
	"testing"
)

// TestRunCodeReviewCallback_ForwardsOriginIdentity proves the callback reads the
// entering session's concrete execution identity so the resolver can exclude the
// writer's own model from review.
func TestRunCodeReviewCallback_ForwardsOriginIdentity(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "t1", "s1", "step-review")

	session, err := repo.GetTaskSession(ctx, "s1")
	if err != nil || session == nil {
		t.Fatalf("load seeded session: session=%v err=%v", session, err)
	}
	session.AgentProfileID = "origin-logical"
	session.ExecutionProfileID = "origin-concrete"
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatalf("update seeded session: %v", err)
	}

	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{})
	runner := &fakeReviewRunner{}
	svc.reviewRunner = runner

	if _, err := (&runCodeReviewCallback{svc: svc}).Execute(ctx, reviewCallbackInput("")); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("expected one launch, got %d", len(runner.requests))
	}
	got := runner.requests[0].Origin
	if got == nil {
		t.Fatal("expected an origin identity on the launch request")
	}
	if got.ProfileID != "origin-logical" || got.ExecutionProfileID != "origin-concrete" {
		t.Fatalf("origin = %+v, want the entering session's logical/concrete profiles", got)
	}
	if !runner.requests[0].OriginRequired {
		t.Fatal("the workflow-step path must require origin exclusion")
	}
}

// TestRunCodeReviewCallback_MissingSessionRequiresOrigin proves step entry stays
// non-blocking while origin exclusion is still required: an unreadable writer
// session launches with a nil origin plus OriginRequired, so resolution fails
// closed instead of silently reviewing with the writer's own model.
func TestRunCodeReviewCallback_MissingSessionRequiresOrigin(t *testing.T) {
	repo := setupTestRepo(t)
	seedSession(t, repo, "t1", "s1", "step-review")
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{})
	runner := &fakeReviewRunner{}
	svc.reviewRunner = runner

	in := reviewCallbackInput("")
	in.State.SessionID = "missing"
	if _, err := (&runCodeReviewCallback{svc: svc}).Execute(context.Background(), in); err != nil {
		t.Fatalf("a missing origin session must not fail step entry, got %v", err)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("expected one launch, got %d", len(runner.requests))
	}
	if runner.requests[0].Origin != nil {
		t.Fatalf("expected no origin identity for an unreadable session, got %+v", runner.requests[0].Origin)
	}
	if !runner.requests[0].OriginRequired {
		t.Fatal("an unreadable origin must still require exclusion so resolution fails closed")
	}
}
