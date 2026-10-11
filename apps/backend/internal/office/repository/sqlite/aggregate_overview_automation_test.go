package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/office/repository/sqlite"
	taskmodels "github.com/kandev/kandev/internal/task/models"
)

// seedOverviewTask inserts one task row with the fields the overview count
// queries read: origin, ephemerality, state, archive mark, and created_at.
func seedOverviewTask(
	t *testing.T, repo *sqlite.Repository,
	id, workspaceID, origin string, ephemeral int, state string, created time.Time,
) {
	t.Helper()
	if _, err := repo.ExecRaw(context.Background(), `
		INSERT INTO tasks (id, workspace_id, title, state, is_ephemeral, origin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, workspaceID, id, state, ephemeral, origin, created.UTC(), created.UTC(),
	); err != nil {
		t.Fatalf("seed task %s: %v", id, err)
	}
}

// TestOverviewCountsExcludeAutomationExecutions pins the split the owner asked
// for: the default created/breakdown counts drop every automation-execution
// origin (the hidden run and the visible normal task), keep an ordinary task
// however cheap, and leave the automation volume on its own read.
func TestOverviewCountsExcludeAutomationExecutions(t *testing.T) {
	repo := newSearchTestRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	recent := now.Add(-time.Hour)
	old := now.Add(-48 * time.Hour)
	since := now.Add(-24 * time.Hour)

	seedOverviewTask(t, repo, "t-manual", "ws-1", taskmodels.TaskOriginManual, 0, "TODO", recent)
	seedOverviewTask(t, repo, "t-zero", "ws-1", taskmodels.TaskOriginAgentCreated, 0, "IN_PROGRESS", recent)
	seedOverviewTask(t, repo, "t-run", "ws-1", taskmodels.TaskOriginAutomationRun, 0, "TODO", recent)
	seedOverviewTask(t, repo, "t-task", "ws-1", taskmodels.TaskOriginAutomationTask, 0, "TODO", recent)
	seedOverviewTask(t, repo, "t-ephem", "ws-1", taskmodels.TaskOriginManual, 1, "TODO", recent)
	seedOverviewTask(t, repo, "t-old", "ws-1", taskmodels.TaskOriginManual, 0, "TODO", old)

	created, err := repo.ListOverviewCreatedTasks(ctx, []string{"ws-1"}, since, 50)
	if err != nil {
		t.Fatalf("ListOverviewCreatedTasks: %v", err)
	}
	gotCreated := map[string]bool{}
	for _, row := range created {
		gotCreated[row.ID] = true
	}
	for _, want := range []string{"t-manual", "t-zero"} {
		if !gotCreated[want] {
			t.Errorf("created list is missing ordinary task %q (got %v)", want, gotCreated)
		}
	}
	for _, banned := range []string{"t-run", "t-task", "t-ephem", "t-old"} {
		if gotCreated[banned] {
			t.Errorf("created list includes %q, which is not an ordinary in-window task", banned)
		}
	}

	automation, err := repo.ListOverviewAutomationCounts(ctx, []string{"ws-1"}, since)
	if err != nil {
		t.Fatalf("ListOverviewAutomationCounts: %v", err)
	}
	if automation["ws-1"] != 2 {
		t.Errorf("automation count = %d, want 2 (both automation-execution origins, in window)", automation["ws-1"])
	}

	breakdowns, err := repo.QueryWorkspaceTaskBreakdowns(ctx, []string{"ws-1"})
	if err != nil {
		t.Fatalf("QueryWorkspaceTaskBreakdowns: %v", err)
	}
	bd := breakdowns["ws-1"]
	if bd.Open != 2 || bd.InProgress != 1 {
		t.Errorf("breakdown = %+v, want open=2 in_progress=1 (t-manual, t-zero, t-old; no automation, no ephemeral)", bd)
	}
}

// TestOverviewAutomationCountsRespectWindowAndWorkspace pins that the separate
// automation figure is scoped by both the window boundary and the workspace
// set it accompanies, so it can never disagree with the counts beside it.
func TestOverviewAutomationCountsRespectWindowAndWorkspace(t *testing.T) {
	repo := newSearchTestRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	recent := now.Add(-time.Hour)
	old := now.Add(-48 * time.Hour)
	since := now.Add(-24 * time.Hour)

	seedOverviewTask(t, repo, "a-run", "ws-1", taskmodels.TaskOriginAutomationRun, 0, "TODO", recent)
	seedOverviewTask(t, repo, "a-task", "ws-1", taskmodels.TaskOriginAutomationTask, 0, "TODO", recent)
	seedOverviewTask(t, repo, "a-old", "ws-1", taskmodels.TaskOriginAutomationRun, 0, "TODO", old)
	seedOverviewTask(t, repo, "b-run", "ws-2", taskmodels.TaskOriginAutomationRun, 0, "TODO", recent)

	automation, err := repo.ListOverviewAutomationCounts(ctx, []string{"ws-1", "ws-2"}, since)
	if err != nil {
		t.Fatalf("ListOverviewAutomationCounts: %v", err)
	}
	if automation["ws-1"] != 2 {
		t.Errorf("ws-1 automation = %d, want 2 (in-window only)", automation["ws-1"])
	}
	if automation["ws-2"] != 1 {
		t.Errorf("ws-2 automation = %d, want 1", automation["ws-2"])
	}
}
