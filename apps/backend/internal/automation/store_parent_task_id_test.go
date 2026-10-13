package automation

import (
	"context"
	"testing"
)

// The automation's optional parent task must round-trip through create and
// load, and stay empty when it was never set.
func TestAutomationParentTaskIDRoundTrip(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	withParent := &Automation{
		WorkspaceID:       "ws-1",
		Name:              "daily sync",
		WorkflowID:        "wf-1",
		ParentTaskID:      "parent-task-1",
		AgentProfileID:    "agent-1",
		ExecutorProfileID: "exec-1",
		Enabled:           true,
		MaxConcurrentRuns: 1,
	}
	if err := store.CreateAutomation(ctx, withParent); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetAutomation(ctx, withParent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected automation, got nil")
	}
	if got.ParentTaskID != "parent-task-1" {
		t.Fatalf("ParentTaskID = %q, want %q", got.ParentTaskID, "parent-task-1")
	}

	topLevel := &Automation{
		WorkspaceID:       "ws-1",
		Name:              "no parent",
		WorkflowID:        "wf-1",
		AgentProfileID:    "agent-1",
		ExecutorProfileID: "exec-1",
		Enabled:           true,
		MaxConcurrentRuns: 1,
	}
	if err := store.CreateAutomation(ctx, topLevel); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetAutomation(ctx, topLevel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ParentTaskID != "" {
		t.Fatalf("ParentTaskID = %q, want empty", loaded.ParentTaskID)
	}
}

// Updating the parent must persist and must also be clearable back to empty.
func TestUpdateAutomationParentTaskID(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	a := &Automation{
		WorkspaceID:       "ws-1",
		Name:              "daily sync",
		WorkflowID:        "wf-1",
		AgentProfileID:    "agent-1",
		ExecutorProfileID: "exec-1",
		Enabled:           true,
		MaxConcurrentRuns: 1,
	}
	if err := store.CreateAutomation(ctx, a); err != nil {
		t.Fatal(err)
	}

	parent := "parent-task-2"
	if err := store.UpdateAutomation(ctx, a.ID, &UpdateAutomationRequest{ParentTaskID: &parent}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetAutomation(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentTaskID != parent {
		t.Fatalf("after update ParentTaskID = %q, want %q", got.ParentTaskID, parent)
	}

	cleared := ""
	if err := store.UpdateAutomation(ctx, a.ID, &UpdateAutomationRequest{ParentTaskID: &cleared}); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetAutomation(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentTaskID != "" {
		t.Fatalf("after clear ParentTaskID = %q, want empty", got.ParentTaskID)
	}
}
