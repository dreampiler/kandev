package review

import (
	"context"
	"errors"
	"testing"
)

// TestRunner_OriginExclusionFailsClosedWhenOnlyOriginModel proves the runner
// wires the origin identity into resolution: when every configured candidate is
// the writer's own model, the run fails closed instead of silently reviewing
// with the same model.
func TestRunner_OriginExclusionFailsClosedWhenOnlyOriginModel(t *testing.T) {
	store := newFakeStore()
	profiles := fakeProfilesByID{
		"p-origin": {AgentID: "claude-acp", Model: "opus"},
	}
	runner := NewRunner(RunnerDeps{
		Store: store,
		Resolver: NewResolver(profiles,
			&fakeUtility{found: true, enabled: true, agentID: "claude-acp", model: "opus"},
			&fakeDefaults{agentID: "claude-acp", model: "opus"}),
		Changes:   &fakeChangeSource{uncommitted: map[string]any{"a.go": fileEntry("a.go", "@@ -1 +1,2 @@\n x\n+y\n", "", "")}},
		Inference: &fakeInference{responses: []string{""}},
		Prompts:   &fakePrompts{},
		Sessions:  &fakeSessions{sessionID: "sess-1"},
		Logger:    testLogger(t),
	})
	runner.Start(context.Background())
	t.Cleanup(runner.Stop)

	_, err := runner.Launch(context.Background(), RunRequest{
		TaskID:    "task-origin-only",
		SessionID: "sess-1",
		Origin:    &OriginIdentity{ProfileID: "p-origin", ExecutionProfileID: "p-origin"},
	})
	if !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("expected ErrAgentUnavailable, got %v", err)
	}
	if len(store.runs) != 0 {
		t.Fatalf("no run row should exist when only the origin model can review, got %d", len(store.runs))
	}
}

// TestRunner_OriginRequiredWithoutIdentityFailsClosed proves a workflow-step
// review with an unreadable writer identity fails with review_agent_unavailable
// and creates no run row, rather than reviewing with an unprovable model.
func TestRunner_OriginRequiredWithoutIdentityFailsClosed(t *testing.T) {
	store := newFakeStore()
	profiles := fakeProfilesByID{
		"p-review": {AgentID: "claude-acp", Model: "sonnet"},
	}
	runner := NewRunner(RunnerDeps{
		Store:     store,
		Resolver:  NewResolver(profiles, &fakeUtility{found: true, enabled: true, agentID: "codex", model: "gpt"}, nil),
		Changes:   &fakeChangeSource{uncommitted: map[string]any{"a.go": fileEntry("a.go", "@@ -1 +1,2 @@\n x\n+y\n", "", "")}},
		Inference: &fakeInference{responses: []string{"OK"}},
		Prompts:   &fakePrompts{},
		Sessions:  &fakeSessions{sessionID: "sess-1"},
		Logger:    testLogger(t),
	})
	runner.Start(context.Background())
	t.Cleanup(runner.Stop)

	_, err := runner.Launch(context.Background(), RunRequest{
		TaskID:         "task-unknown-origin",
		SessionID:      "sess-1",
		AgentProfileID: "p-review",
		OriginRequired: true,
	})
	if !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("expected ErrAgentUnavailable, got %v", err)
	}
	if len(store.runs) != 0 {
		t.Fatalf("no run row should exist when the origin is unknown, got %d", len(store.runs))
	}
}

// TestRunner_OriginExclusionUsesDistinctReviewer proves a distinct candidate
// still runs and is recorded on the run row.
func TestRunner_OriginExclusionUsesDistinctReviewer(t *testing.T) {
	store := newFakeStore()
	profiles := fakeProfilesByID{
		"p-origin": {AgentID: "claude-acp", Model: "opus"},
		"p-review": {AgentID: "claude-acp", Model: "sonnet"},
	}
	runner := NewRunner(RunnerDeps{
		Store:     store,
		Resolver:  NewResolver(profiles, nil, nil),
		Changes:   &fakeChangeSource{uncommitted: map[string]any{"a.go": fileEntry("a.go", "@@ -1 +1,2 @@\n x\n+y\n", "", "")}},
		Inference: &fakeInference{responses: []string{"OK"}},
		Prompts:   &fakePrompts{},
		Sessions:  &fakeSessions{sessionID: "sess-1"},
		Logger:    testLogger(t),
	})
	runner.Start(context.Background())
	t.Cleanup(runner.Stop)

	run, err := runner.Launch(context.Background(), RunRequest{
		TaskID:         "task-distinct",
		SessionID:      "sess-1",
		AgentProfileID: "p-review",
		Origin:         &OriginIdentity{ProfileID: "p-origin", ExecutionProfileID: "p-origin"},
	})
	if err != nil {
		t.Fatalf("Launch returned error: %v", err)
	}
	if run.Model != "sonnet" {
		t.Fatalf("run model = %q, want the distinct reviewer sonnet", run.Model)
	}
}
