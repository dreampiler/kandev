package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// seedGithubMergedPR inserts one merged github_task_prs row with the minimal
// columns ListOverviewMergedPRs reads.
func seedGithubMergedPR(t *testing.T, db *sqlx.DB, workspaceID, taskID string, number int, mergedAt time.Time) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS github_task_prs (
		id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL DEFAULT '', task_id TEXT NOT NULL,
		owner TEXT NOT NULL, repo TEXT NOT NULL, pr_number INTEGER NOT NULL,
		pr_title TEXT NOT NULL, merged_at DATETIME
	)`); err != nil {
		t.Fatalf("create github_task_prs: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO github_task_prs (id, workspace_id, task_id, owner, repo, pr_number, pr_title, merged_at)
		VALUES (?, ?, ?, 'octo', 'demo', ?, 'GitHub fix', ?)`,
		"github-pr-1", workspaceID, taskID, number, mergedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("seed github_task_prs: %v", err)
	}
}

func seedPluginMergedChangeRequest(t *testing.T, db *sqlx.DB, workspaceID, taskID, provider string, number int, mergedAt time.Time) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS plugin_task_change_requests (
		id TEXT PRIMARY KEY, installation_id TEXT NOT NULL, workspace_id TEXT NOT NULL,
		task_id TEXT NOT NULL, provider_id TEXT NOT NULL, provider_host TEXT NOT NULL DEFAULT '',
		repository_id TEXT NOT NULL, number INTEGER NOT NULL, url TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '', state TEXT NOT NULL DEFAULT 'open',
		head_branch TEXT NOT NULL DEFAULT '', base_branch TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL, merged_at TEXT NOT NULL DEFAULT '', closed_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create plugin_task_change_requests: %v", err)
	}
	stamp := mergedAt.UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO plugin_task_change_requests (
		id, installation_id, workspace_id, task_id, provider_id, repository_id, number, url, title,
		state, created_at, merged_at, updated_at)
		VALUES (?, 'inst-1', ?, ?, ?, 'repo-1', ?, 'https://forge.example.test/pulls/7', 'Forgejo fix',
		'merged', ?, ?, ?)`,
		"plugin-cr-1", workspaceID, taskID, provider, number, stamp, stamp, stamp); err != nil {
		t.Fatalf("seed plugin_task_change_requests: %v", err)
	}
}

func TestListOverviewMergedPRsIncludesPluginReports(t *testing.T) {
	repo := newTestRepo(t)
	db := repo.ReaderDB()
	now := time.Now().UTC()
	seedGithubMergedPR(t, db, "ws-1", "task-gh", 11, now.Add(-2*time.Hour))
	seedPluginMergedChangeRequest(t, db, "ws-1", "task-forgejo", "forgejo", 7, now.Add(-time.Hour))
	rows, err := repo.ListOverviewMergedPRs(context.Background(), []string{"ws-1"}, now.Add(-24*time.Hour), 10)
	if err != nil {
		t.Fatalf("ListOverviewMergedPRs: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("merged rows = %d, want 2 (github + plugin)", len(rows))
	}
	if rows[0].Provider != "forgejo" || rows[0].Number != 7 {
		t.Fatalf("newest row = provider %q number %d, want forgejo 7", rows[0].Provider, rows[0].Number)
	}
	if rows[1].Provider != "github" || rows[1].Number != 11 {
		t.Fatalf("older row = provider %q number %d, want github 11", rows[1].Provider, rows[1].Number)
	}
}

func TestListOverviewMergedPRsWithoutPluginTable(t *testing.T) {
	repo := newTestRepo(t)
	db := repo.ReaderDB()
	now := time.Now().UTC()
	seedGithubMergedPR(t, db, "ws-1", "task-gh", 11, now.Add(-2*time.Hour))
	if _, err := db.Exec(`DROP TABLE IF EXISTS plugin_task_change_requests`); err != nil {
		t.Fatalf("drop plugin table: %v", err)
	}
	rows, err := repo.ListOverviewMergedPRs(context.Background(), []string{"ws-1"}, now.Add(-24*time.Hour), 10)
	if err != nil {
		t.Fatalf("ListOverviewMergedPRs without plugin table: %v", err)
	}
	if len(rows) != 1 || rows[0].Provider != "github" {
		t.Fatalf("merged rows = %v, want the single github row", rows)
	}
}
