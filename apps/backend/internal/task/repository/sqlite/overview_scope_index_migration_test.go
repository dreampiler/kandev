package sqlite

import (
	"strings"
	"testing"
)

// TestOverviewScopeIndexesExistAfterMigration guards the multi-workspace
// overview read path (GET /api/v1/office/workspaces/aggregate): the snapshot
// build filters tasks by workspace plus is_ephemeral/origin/state and joins
// per-task session and turn rows by a time window, so these three indexes are
// what keep each read seekable instead of a per-task scan of every row.
func TestOverviewScopeIndexesExistAfterMigration(t *testing.T) {
	repo := newRepoForEntityTests(t)

	for _, want := range []struct {
		name string
		cols []string
	}{
		{"idx_tasks_workspace_scope", []string{"workspace_id", "is_ephemeral", "origin", "state", "id"}},
		{"idx_turns_task_started", []string{"task_id", "started_at"}},
		{"idx_sessions_state_updated", []string{"state", "updated_at"}},
	} {
		var name string
		if err := repo.db.Get(&name, `
			SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?
		`, want.name); err != nil {
			t.Fatalf("index %s missing after task repository migration: %v", want.name, err)
		}

		var cols []string
		if err := repo.db.Select(&cols, "SELECT name FROM pragma_index_info('"+want.name+"') ORDER BY seqno"); err != nil {
			t.Fatalf("read columns of index %s: %v", want.name, err)
		}
		if strings.Join(cols, ",") != strings.Join(want.cols, ",") {
			t.Fatalf("index %s columns = %v, want %v", want.name, cols, want.cols)
		}
	}
}
