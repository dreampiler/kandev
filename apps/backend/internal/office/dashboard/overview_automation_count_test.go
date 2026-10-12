package dashboard_test

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/office/dashboard"
	taskmodels "github.com/kandev/kandev/internal/task/models"
)

// TestOverviewActivitySeparatesAutomationCount pins the owner-facing split: an
// automation-execution task (both the hidden automation_run and the visible
// automation_task) is kept out of the default per-workspace counts and reported
// on the project-statistics block's separate automation figure instead.
func TestOverviewActivitySeparatesAutomationCount(t *testing.T) {
	deps := overviewFixture(t)
	deps.svc.SetWorkspaceLister(overviewLister())
	deps.svc.SetOverviewReader(deps.repo)

	now := time.Now().UTC()
	mustExec(t, deps, `INSERT INTO tasks (id, workspace_id, title, state, origin, created_at, updated_at)
		VALUES
			('t-auto-run', 'ws-office', 'nightly check', 'IN_PROGRESS', ?, ?, ?),
			('t-auto-task', 'ws-office', 'visible firing', 'IN_PROGRESS', ?, ?, ?)`,
		taskmodels.TaskOriginAutomationRun, now.Add(-2*time.Hour), now.Add(-2*time.Hour),
		taskmodels.TaskOriginAutomationTask, now.Add(-2*time.Hour), now.Add(-2*time.Hour))

	resp, err := deps.svc.GetWorkspacesAggregate(context.Background())
	if err != nil {
		t.Fatalf("GetWorkspacesAggregate: %v", err)
	}
	entry := workspaceEntry(resp, "ws-office")
	if entry == nil || entry.Metrics == nil || entry.Metrics.Activity == nil {
		t.Fatalf("ws-office activity missing: %+v", entry)
	}
	if got := entry.Metrics.Activity.Automation; got != 2 {
		t.Errorf("activity automation = %d, want 2 (both automation-execution origins)", got)
	}

	// The automation tasks must not surface as ordinary open work.
	list, err := deps.svc.GetOverviewWorkspaceTasks(
		context.Background(), "ws-office", dashboard.OverviewFilterAll, 0)
	if err != nil {
		t.Fatalf("GetOverviewWorkspaceTasks: %v", err)
	}
	for _, item := range list.Tasks {
		if item.TaskID == "t-auto-run" || item.TaskID == "t-auto-task" {
			t.Errorf("open task list includes automation task %q", item.TaskID)
		}
	}
}

func workspaceEntry(resp *dashboard.WorkspaceAggregateResponse, workspaceID string) *dashboard.WorkspaceAggregateEntry {
	for i := range resp.Workspaces {
		if resp.Workspaces[i].WorkspaceID == workspaceID {
			return &resp.Workspaces[i]
		}
	}
	return nil
}
