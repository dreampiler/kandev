package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/office/repository/sqlite"
)

func seedAgentErrorActivity(
	t *testing.T, repo *sqlite.Repository, id, wsID, agentID, runID, createdAt string,
) {
	t.Helper()
	if _, err := repo.ExecRaw(context.Background(), `
		INSERT INTO office_activity_log
			(id, workspace_id, actor_type, actor_id, action, target_type, target_id, details, run_id, session_id, created_at)
		VALUES (?, ?, 'system', 'scheduler', 'agent.error', 'agent', ?, '{}', ?, '', ?)`,
		id, wsID, agentID, runID, createdAt); err != nil {
		t.Fatalf("seed agent.error %s: %v", id, err)
	}
}

func TestListAgentErrorInboxRows_DismissalAndScope(t *testing.T) {
	repo, db := newTestRepoWithDB(t)
	ctx := context.Background()

	seedFailureAgent(t, db, "agent-err", "ws-1", "Err Agent", "", 0, 0, false)
	seedSummaryRun(t, repo, "run-err", "agent-err", "failed",
		"2026-01-01 09:00:00", "", "2026-01-01 10:00:00", `{"task_id":"t-1"}`)
	seedAgentErrorActivity(t, repo, "ae-1", "ws-1", "agent-err", "run-err", "2026-01-01 10:00:01")

	rows, err := repo.ListAgentErrorInboxRows(ctx, "ws-1", "user-1")
	if err != nil {
		t.Fatalf("ListAgentErrorInboxRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.EntryID != "ae-1" || got.AgentProfileID != "agent-err" ||
		got.AgentName != "Err Agent" || got.TaskID != "t-1" || got.RunID != "run-err" {
		t.Fatalf("row = %+v, want ae-1 / agent-err / Err Agent / t-1 / run-err", got)
	}

	// A per-user dismissal hides the entry only for that user.
	if err := repo.DismissInboxItem(ctx, "user-1", "agent_error", "ae-1"); err != nil {
		t.Fatalf("dismiss ae-1: %v", err)
	}
	rows, err = repo.ListAgentErrorInboxRows(ctx, "ws-1", "user-1")
	if err != nil {
		t.Fatalf("ListAgentErrorInboxRows after dismiss: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("dismissing user rows = %d, want 0", len(rows))
	}
	rows, err = repo.ListAgentErrorInboxRows(ctx, "ws-1", "user-2")
	if err != nil {
		t.Fatalf("ListAgentErrorInboxRows other user: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("other user rows = %d, want 1", len(rows))
	}
}

func TestHasSuccessfulRunAfter_SameScopeOnly(t *testing.T) {
	repo, db := newTestRepoWithDB(t)
	ctx := context.Background()
	seedFailureAgent(t, db, "agent-err", "ws-1", "Err", "", 0, 0, false)

	seedSummaryRun(t, repo, "ok-t1", "agent-err", "finished",
		"2026-01-02 09:00:00", "", "2026-01-02 10:00:00", `{"task_id":"t-1"}`)
	failureAt := time.Date(2026, 1, 1, 10, 0, 1, 0, time.UTC)

	ok, err := repo.HasSuccessfulRunAfter(ctx, "agent-err", "t-1", failureAt)
	if err != nil || !ok {
		t.Fatalf("same-scope success: ok=%v err=%v, want true", ok, err)
	}
	// A success on another task does not clear this scope.
	ok, err = repo.HasSuccessfulRunAfter(ctx, "agent-err", "t-2", failureAt)
	if err != nil || ok {
		t.Fatalf("other-scope success: ok=%v err=%v, want false", ok, err)
	}
	// A success older than the failure does not clear it.
	ok, err = repo.HasSuccessfulRunAfter(ctx, "agent-err", "t-1",
		time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC))
	if err != nil || ok {
		t.Fatalf("older success: ok=%v err=%v, want false", ok, err)
	}
	// An empty agent scope is never resolved.
	ok, err = repo.HasSuccessfulRunAfter(ctx, "", "t-1", failureAt)
	if err != nil || ok {
		t.Fatalf("empty agent scope: ok=%v err=%v, want false", ok, err)
	}
}

func TestHasSuccessfulRunAfter_TasklessScope(t *testing.T) {
	repo, db := newTestRepoWithDB(t)
	ctx := context.Background()
	seedFailureAgent(t, db, "agent-err", "ws-1", "Err", "", 0, 0, false)

	// A tasked success must not clear the taskless scope.
	seedSummaryRun(t, repo, "ok-tasked", "agent-err", "finished",
		"2026-01-02 09:00:00", "", "2026-01-02 10:00:00", `{"task_id":"t-1"}`)
	failureAt := time.Date(2026, 1, 1, 10, 0, 1, 0, time.UTC)
	ok, err := repo.HasSuccessfulRunAfter(ctx, "agent-err", "", failureAt)
	if err != nil || ok {
		t.Fatalf("tasked success vs taskless scope: ok=%v err=%v, want false", ok, err)
	}

	// A later taskless success clears it.
	seedSummaryRun(t, repo, "ok-taskless", "agent-err", "finished",
		"2026-01-02 11:00:00", "", "2026-01-02 12:00:00", `{}`)
	ok, err = repo.HasSuccessfulRunAfter(ctx, "agent-err", "", failureAt)
	if err != nil || !ok {
		t.Fatalf("taskless success: ok=%v err=%v, want true", ok, err)
	}
}
