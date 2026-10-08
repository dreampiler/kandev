package sqlite_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/office/repository/sqlite"
)

// ensureFailureBucketSessionTables creates the task_sessions stub the failure
// bucket read needs. It carries error_message, which the shared dashboard
// session stub omits, so the read can be exercised without a full task schema.
func ensureFailureBucketSessionTables(t *testing.T, repo *sqlite.Repository) {
	t.Helper()
	if _, err := repo.ExecRaw(context.Background(), `
		CREATE TABLE IF NOT EXISTS task_sessions (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			state TEXT NOT NULL DEFAULT 'CREATED',
			error_message TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMP NOT NULL,
			completed_at TIMESTAMP,
			updated_at TIMESTAMP NOT NULL
		)`); err != nil {
		t.Fatalf("create task_sessions stub: %v", err)
	}
}

func seedFailureBucketTask(t *testing.T, repo *sqlite.Repository) {
	t.Helper()
	if _, err := repo.ExecRaw(context.Background(), `
		INSERT INTO tasks (id, workspace_id, title, is_ephemeral, origin, created_at, updated_at) VALUES
			('t-fail', 'ws-1', 'failed task', 0, 'manual', datetime('now'), datetime('now'))
	`); err != nil {
		t.Fatalf("seed task: %v", err)
	}
}

// Product failures must land in the owner's four buckets: a no-output session
// is no_response (even when its completed_at is null, which used to read as a
// launch failure), a launch fault is start_failed, a provider limit is limit.
func TestListOverviewFailureBuckets_ClassifiesProductFailures(t *testing.T) {
	repo := newSearchTestRepo(t)
	ensureFailureBucketSessionTables(t, repo)
	seedFailureBucketTask(t, repo)
	ctx := context.Background()
	now := time.Now().UTC()
	start := now.Add(-time.Hour)
	completed := now.Add(-time.Minute)

	if _, err := repo.ExecRaw(ctx, `
		INSERT INTO task_sessions (id, task_id, state, error_message, started_at, completed_at, updated_at) VALUES
			('s-noout',  't-fail', 'FAILED', 'agent produced no output since start', ?, ?, ?),
			('s-noout2', 't-fail', 'FAILED', 'agent produced no output since start', ?, NULL, ?),
			('s-start',  't-fail', 'FAILED', 'The agent could not start.', ?, ?, ?),
			('s-cmd',    't-fail', 'FAILED', 'validate agent command: agent command cannot be empty', ?, ?, ?),
			('s-wt',     't-fail', 'FAILED', 'validate launch workspace repository x: required worktree is unavailable', ?, ?, ?),
			('s-limit',  't-fail', 'FAILED', 'AI_APICallError: Rate limit exceeded', ?, ?, ?),
			('s-empty',  't-fail', 'FAILED', '', ?, ?, ?),
			('s-other',  't-fail', 'FAILED', 'something else broke', ?, ?, ?)
	`,
		start, completed, completed,
		start, completed,
		start, completed, completed,
		start, completed, completed,
		start, completed, completed,
		start, completed, completed,
		start, completed, completed,
		start, completed, completed,
	); err != nil {
		t.Fatalf("seed sessions: %v", err)
	}

	rows, err := repo.ListOverviewFailureBuckets(ctx, []string{"ws-1"}, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("ListOverviewFailureBuckets: %v", err)
	}
	got := map[string]int{}
	total := 0
	for _, row := range rows {
		got[row.Bucket] += row.Count
		total += row.Count
	}
	if total != 8 {
		t.Fatalf("bucket total = %d, want 8 (buckets=%v)", total, got)
	}
	want := map[string]int{"no_response": 3, "start_failed": 3, "limit": 1, "other": 1}
	for code, count := range want {
		if got[code] != count {
			t.Errorf("bucket %q = %d, want %d (all=%v)", code, got[code], count, got)
		}
	}
}

// The breakdown shows the agent's own first line verbatim, so a first line
// longer than the old 300-character response cap must reach the caller whole.
func TestListOverviewFailureBuckets_KeepsWholeFirstLine(t *testing.T) {
	repo := newSearchTestRepo(t)
	ensureFailureBucketSessionTables(t, repo)
	seedFailureBucketTask(t, repo)
	ctx := context.Background()
	now := time.Now().UTC()
	firstLine := strings.Repeat("x", 320)
	message := firstLine + "\nmore detail below"

	if _, err := repo.ExecRaw(ctx, `
		INSERT INTO task_sessions (id, task_id, state, error_message, started_at, completed_at, updated_at) VALUES
			('s-long', 't-fail', 'FAILED', ?, ?, ?, ?)
	`, message, now.Add(-time.Hour), now.Add(-time.Minute), now.Add(-time.Minute)); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	rows, err := repo.ListOverviewFailureBuckets(ctx, []string{"ws-1"}, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("ListOverviewFailureBuckets: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if !strings.HasPrefix(rows[0].ErrorMessage, firstLine) {
		t.Fatalf("first line truncated at %d chars: %q", len(rows[0].ErrorMessage), rows[0].ErrorMessage)
	}
}
