package sqlite_test

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/office/models"
)

func TestCrossWorkspaceAccess_DefaultIsDisabled(t *testing.T) {
	repo := newTestRepo(t)
	got, err := repo.GetCrossWorkspaceAccess(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("GetCrossWorkspaceAccess: %v", err)
	}
	if got.Enabled || len(got.AllowedWorkspaceIDs) != 0 || len(got.CoordinatorTaskIDs) != 0 {
		t.Fatalf("default = %+v, want disabled and empty", got)
	}
}

func TestCrossWorkspaceAccess_RoundTripAndListEnabled(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.SetCrossWorkspaceAccess(ctx, models.CrossWorkspaceAccess{
		WorkspaceID:         "office-1",
		Enabled:             true,
		AllowedWorkspaceIDs: []string{"ws-a", "ws-b"},
		CoordinatorTaskIDs:  map[string]string{"ws-a": "task-a"},
	}); err != nil {
		t.Fatalf("set office-1: %v", err)
	}
	if err := repo.SetCrossWorkspaceAccess(ctx, models.CrossWorkspaceAccess{WorkspaceID: "office-2"}); err != nil {
		t.Fatalf("set office-2: %v", err)
	}
	got, err := repo.GetCrossWorkspaceAccess(ctx, "office-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Enabled || len(got.AllowedWorkspaceIDs) != 2 || got.CoordinatorTaskIDs["ws-a"] != "task-a" {
		t.Fatalf("round trip = %+v", got)
	}
	enabled, err := repo.ListEnabledCrossWorkspaceAccess(ctx)
	if err != nil {
		t.Fatalf("list enabled: %v", err)
	}
	if len(enabled) != 1 || enabled[0].WorkspaceID != "office-1" {
		t.Fatalf("enabled = %+v, want office-1 only", enabled)
	}

	// Turning it off is a replace, not a merge.
	if err := repo.SetCrossWorkspaceAccess(ctx, models.CrossWorkspaceAccess{WorkspaceID: "office-1"}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, _ = repo.GetCrossWorkspaceAccess(ctx, "office-1")
	if got.Enabled || len(got.AllowedWorkspaceIDs) != 0 || len(got.CoordinatorTaskIDs) != 0 {
		t.Fatalf("after disable = %+v", got)
	}
}
