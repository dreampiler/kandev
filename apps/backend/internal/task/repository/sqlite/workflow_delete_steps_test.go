package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/kandev/kandev/internal/db"
	"github.com/kandev/kandev/internal/task/models"
)

// openWorkflowDeleteStepsRepo opens (or reopens) the task repository on an
// on-disk SQLite file so the boot migrations run against the same rows the
// test seeded. The task repository initializes before the workflow
// repository in production, so the workflow_steps table here is the task
// repository's projection stub, which has no foreign key to workflows.
func openWorkflowDeleteStepsRepo(t *testing.T, dbPath string) (*Repository, *sqlx.DB) {
	t.Helper()
	dbConn, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlxDB := sqlx.NewDb(dbConn, "sqlite3")
	t.Cleanup(func() { _ = sqlxDB.Close() })
	repo, err := NewWithDB(sqlxDB, sqlxDB, nil)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	return repo, sqlxDB
}

func insertWorkflowDeleteStep(t *testing.T, conn *sqlx.DB, id, workflowID string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := conn.Exec(conn.Rebind(`
		INSERT INTO workflow_steps (id, workflow_id, name, position, created_at, updated_at)
		VALUES (?, ?, ?, 0, ?, ?)
	`), id, workflowID, id, now, now); err != nil {
		t.Fatalf("insert workflow step %s: %v", id, err)
	}
}

func countWorkflowDeleteSteps(t *testing.T, conn *sqlx.DB, workflowID string) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(conn.Rebind(`SELECT COUNT(*) FROM workflow_steps WHERE workflow_id = ?`), workflowID).Scan(&n); err != nil {
		t.Fatalf("count workflow steps for %s: %v", workflowID, err)
	}
	return n
}

func createWorkflowDeleteStepsWorkflow(t *testing.T, repo *Repository, workspaceID, name string) *models.Workflow {
	t.Helper()
	wf := &models.Workflow{WorkspaceID: workspaceID, Name: name}
	if err := repo.CreateWorkflow(context.Background(), wf); err != nil {
		t.Fatalf("create workflow %s: %v", name, err)
	}
	return wf
}

func TestDeleteWorkflow_RemovesItsSteps(t *testing.T) {
	repo, conn := openWorkflowDeleteStepsRepo(t, filepath.Join(t.TempDir(), "wf-delete-steps.db"))
	ctx := context.Background()
	doomed := createWorkflowDeleteStepsWorkflow(t, repo, "ws-1", "doomed")
	kept := createWorkflowDeleteStepsWorkflow(t, repo, "ws-1", "kept")
	insertWorkflowDeleteStep(t, conn, "doomed-todo", doomed.ID)
	insertWorkflowDeleteStep(t, conn, "doomed-done", doomed.ID)
	insertWorkflowDeleteStep(t, conn, "kept-todo", kept.ID)

	if err := repo.DeleteWorkflow(ctx, doomed.ID); err != nil {
		t.Fatalf("delete workflow: %v", err)
	}

	if got := countWorkflowDeleteSteps(t, conn, doomed.ID); got != 0 {
		t.Fatalf("steps left for deleted workflow = %d, want 0", got)
	}
	if got := countWorkflowDeleteSteps(t, conn, kept.ID); got != 1 {
		t.Fatalf("steps for other workflow = %d, want 1", got)
	}
}

func TestDeleteWorkflow_NotFoundKeepsSteps(t *testing.T) {
	repo, conn := openWorkflowDeleteStepsRepo(t, filepath.Join(t.TempDir(), "wf-delete-missing.db"))
	insertWorkflowDeleteStep(t, conn, "stray", "missing-workflow")

	if err := repo.DeleteWorkflow(context.Background(), "missing-workflow"); err == nil {
		t.Fatal("delete missing workflow: want not-found error, got nil")
	}
	// The failed delete rolls back, so the step delete inside the same
	// transaction must not land either.
	if got := countWorkflowDeleteSteps(t, conn, "missing-workflow"); got != 1 {
		t.Fatalf("steps after failed delete = %d, want 1 (rolled back)", got)
	}
}

func TestDeleteWorkflowsByWorkspace_RemovesTheirSteps(t *testing.T) {
	repo, conn := openWorkflowDeleteStepsRepo(t, filepath.Join(t.TempDir(), "wf-delete-by-workspace.db"))
	doomed := createWorkflowDeleteStepsWorkflow(t, repo, "ws-1", "doomed")
	excluded := createWorkflowDeleteStepsWorkflow(t, repo, "ws-1", "excluded")
	otherWorkspace := createWorkflowDeleteStepsWorkflow(t, repo, "ws-2", "other")
	insertWorkflowDeleteStep(t, conn, "doomed-todo", doomed.ID)
	insertWorkflowDeleteStep(t, conn, "excluded-todo", excluded.ID)
	insertWorkflowDeleteStep(t, conn, "other-todo", otherWorkspace.ID)

	deleted, err := repo.DeleteWorkflowsByWorkspace(context.Background(), "ws-1", []string{excluded.ID})
	if err != nil {
		t.Fatalf("delete workflows by workspace: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted workflows = %d, want 1", deleted)
	}
	if got := countWorkflowDeleteSteps(t, conn, doomed.ID); got != 0 {
		t.Fatalf("steps left for deleted workflow = %d, want 0", got)
	}
	if got := countWorkflowDeleteSteps(t, conn, excluded.ID); got != 1 {
		t.Fatalf("steps for excluded workflow = %d, want 1", got)
	}
	if got := countWorkflowDeleteSteps(t, conn, otherWorkspace.ID); got != 1 {
		t.Fatalf("steps for other workspace = %d, want 1", got)
	}
}

func TestMigration_PrunesOrphanWorkflowStepsOnce(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wf-orphan-steps.db")
	repo, conn := openWorkflowDeleteStepsRepo(t, dbPath)
	live := createWorkflowDeleteStepsWorkflow(t, repo, "ws-1", "live")
	insertWorkflowDeleteStep(t, conn, "live-todo", live.ID)
	insertWorkflowDeleteStep(t, conn, "orphan-todo", "deleted-workflow")
	insertWorkflowDeleteStep(t, conn, "orphan-done", "deleted-workflow")
	// Simulate a database written before this migration shipped.
	if _, err := conn.Exec(conn.Rebind(`DELETE FROM kandev_meta WHERE key = ?`), workflowStepOrphansPrunedMetaKey); err != nil {
		t.Fatalf("clear migration marker: %v", err)
	}
	_ = conn.Close()

	_, conn = openWorkflowDeleteStepsRepo(t, dbPath)
	if got := countWorkflowDeleteSteps(t, conn, "deleted-workflow"); got != 0 {
		t.Fatalf("orphan steps after migration = %d, want 0", got)
	}
	if got := countWorkflowDeleteSteps(t, conn, live.ID); got != 1 {
		t.Fatalf("live workflow steps after migration = %d, want 1", got)
	}
	var marker int
	if err := conn.QueryRow(conn.Rebind(`SELECT COUNT(*) FROM kandev_meta WHERE key = ?`), workflowStepOrphansPrunedMetaKey).Scan(&marker); err != nil {
		t.Fatalf("read migration marker: %v", err)
	}
	if marker != 1 {
		t.Fatalf("migration marker rows = %d, want 1", marker)
	}
	_ = conn.Close()

	// A later boot is a no-op: the marker is present and the live step stays.
	_, conn = openWorkflowDeleteStepsRepo(t, dbPath)
	if got := countWorkflowDeleteSteps(t, conn, live.ID); got != 1 {
		t.Fatalf("live workflow steps after second boot = %d, want 1", got)
	}
}
