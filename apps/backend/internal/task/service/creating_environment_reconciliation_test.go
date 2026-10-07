package service

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
)

// stubExecutionLiveness is the test double for TaskExecutionLivenessChecker:
// it reports the sessions named in live as having a live in-memory execution.
type stubExecutionLiveness struct {
	live map[string]bool
}

func (s stubExecutionLiveness) HasLiveExecution(sessionID string) bool {
	return s.live[sessionID]
}

func newCreatingEnvFixture(t *testing.T) (*Service, *sqliterepo.Repository, time.Time) {
	t.Helper()
	svc, _, repo := createTestService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws-w047", Name: "Workspace"}); err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if err := repo.CreateWorkflow(ctx, &models.Workflow{ID: "wf-w047", WorkspaceID: "ws-w047", Name: "Workflow"}); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if err := repo.CreateTask(ctx, &models.Task{
		ID: "task-w047", WorkspaceID: "ws-w047", WorkflowID: "wf-w047",
		WorkflowStepID: "step-w047", Title: "W047", Priority: "medium",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return svc, repo, now
}

// seedCreatingEnv inserts a creating environment owned by ownerID. A non-empty
// ownerState inserts the owner session row; an empty ownerState simulates a
// deleted owner. The environment's created_at is backdated so the sweep sees it
// past (or inside) its timeout without sleeping.
func seedCreatingEnv(
	t *testing.T,
	repo *sqliterepo.Repository,
	envID, taskID, ownerID string,
	createdAt time.Time,
	ownerState models.TaskSessionState,
) {
	t.Helper()
	ctx := context.Background()
	if ownerID != "" && ownerState != "" {
		if err := repo.CreateTaskSession(ctx, &models.TaskSession{
			ID: ownerID, TaskID: taskID, State: ownerState,
			AgentProfileID: "agent-1", StartedAt: createdAt, UpdatedAt: createdAt,
		}); err != nil {
			t.Fatalf("CreateTaskSession: %v", err)
		}
	}
	env := &models.TaskEnvironment{
		ID: envID, TaskID: taskID, ExecutorType: string(models.ExecutorTypeWorktree),
		Status: models.TaskEnvironmentStatusCreating, MaterializationSessionID: ownerID,
	}
	if err := repo.CreateTaskEnvironment(ctx, env); err != nil {
		t.Fatalf("CreateTaskEnvironment: %v", err)
	}
	if _, err := repo.DB().Exec(
		`UPDATE task_environments SET created_at = ?, updated_at = ? WHERE id = ?`,
		createdAt, createdAt, envID,
	); err != nil {
		t.Fatalf("backdate environment: %v", err)
	}
}

func mustEnvironment(t *testing.T, repo *sqliterepo.Repository, envID string) *models.TaskEnvironment {
	t.Helper()
	env, err := repo.GetTaskEnvironment(context.Background(), envID)
	if err != nil {
		t.Fatalf("GetTaskEnvironment(%s): %v", envID, err)
	}
	return env
}

func TestCreatingEnvironmentReconciliation_ReapsAbandonedOwner(t *testing.T) {
	svc, repo, now := newCreatingEnvFixture(t)
	seedCreatingEnv(t, repo, "env-abandoned", "task-w047", "sess-waiting", now.Add(-11*time.Minute), models.TaskSessionStateWaitingForInput)

	svc.runCreatingEnvironmentReconciliation(context.Background(), now)

	env := mustEnvironment(t, repo, "env-abandoned")
	if env.Status != models.TaskEnvironmentStatusFailed {
		t.Fatalf("status = %q, want failed after abandoned materialization", env.Status)
	}
	if env.MaterializationSessionID != "" {
		t.Fatalf("materialization_session_id = %q, want cleared", env.MaterializationSessionID)
	}
}

func TestCreatingEnvironmentReconciliation_ReapsDeletedOwner(t *testing.T) {
	svc, repo, now := newCreatingEnvFixture(t)
	seedCreatingEnv(t, repo, "env-deleted", "task-w047", "sess-deleted", now.Add(-11*time.Minute), "")

	svc.runCreatingEnvironmentReconciliation(context.Background(), now)

	env := mustEnvironment(t, repo, "env-deleted")
	if env.Status != models.TaskEnvironmentStatusFailed {
		t.Fatalf("status = %q, want failed when the owner session is gone", env.Status)
	}
}

func TestCreatingEnvironmentReconciliation_KeepsRunningOwner(t *testing.T) {
	svc, repo, now := newCreatingEnvFixture(t)
	seedCreatingEnv(t, repo, "env-running", "task-w047", "sess-running", now.Add(-11*time.Minute), models.TaskSessionStateStarting)
	if err := repo.UpsertExecutorRunning(context.Background(), &models.ExecutorRunning{
		ID: "sess-running", SessionID: "sess-running", TaskID: "task-w047",
		ExecutorID: "exec-local", Status: models.ExecutorRunningStatusRunning,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertExecutorRunning: %v", err)
	}

	svc.runCreatingEnvironmentReconciliation(context.Background(), now)

	env := mustEnvironment(t, repo, "env-running")
	if env.Status != models.TaskEnvironmentStatusCreating {
		t.Fatalf("status = %q, want creating while the owner is actively materializing", env.Status)
	}
	if env.MaterializationSessionID != "sess-running" {
		t.Fatalf("materialization_session_id = %q, want preserved", env.MaterializationSessionID)
	}
}

func TestCreatingEnvironmentReconciliation_KeepsLiveInMemoryLaunch(t *testing.T) {
	svc, repo, now := newCreatingEnvFixture(t)
	svc.SetExecutionLivenessChecker(stubExecutionLiveness{live: map[string]bool{"sess-live": true}})
	seedCreatingEnv(t, repo, "env-live", "task-w047", "sess-live", now.Add(-11*time.Minute), models.TaskSessionStateWaitingForInput)

	svc.runCreatingEnvironmentReconciliation(context.Background(), now)

	env := mustEnvironment(t, repo, "env-live")
	if env.Status != models.TaskEnvironmentStatusCreating {
		t.Fatalf("status = %q, want creating while a live in-memory launch backs the owner", env.Status)
	}
}

func TestCreatingEnvironmentReconciliation_KeepsEnvironmentInsideTimeout(t *testing.T) {
	svc, repo, now := newCreatingEnvFixture(t)
	seedCreatingEnv(t, repo, "env-fresh", "task-w047", "sess-waiting", now.Add(-5*time.Minute), models.TaskSessionStateWaitingForInput)

	svc.runCreatingEnvironmentReconciliation(context.Background(), now)

	env := mustEnvironment(t, repo, "env-fresh")
	if env.Status != models.TaskEnvironmentStatusCreating {
		t.Fatalf("status = %q, want creating inside the materialization timeout", env.Status)
	}
}

func TestCreatingEnvironmentReconciliation_ClearsSharedGroupPointer(t *testing.T) {
	svc, repo, now := newCreatingEnvFixture(t)
	seedCreatingEnv(t, repo, "env-shared", "task-w047", "sess-waiting", now.Add(-11*time.Minute), models.TaskSessionStateWaitingForInput)
	if _, err := repo.DB().Exec(
		`INSERT INTO task_workspace_groups (
			id, workspace_id, owner_task_id, materialized_environment_id,
			materialized_kind, created_at, updated_at
		) VALUES ('group-w047', 'ws-w047', 'task-w047', 'env-shared', 'task_directory', ?, ?)`,
		now, now,
	); err != nil {
		t.Fatalf("seed workspace group: %v", err)
	}

	svc.runCreatingEnvironmentReconciliation(context.Background(), now)

	var materializedEnvironmentID string
	if err := repo.DB().QueryRow(
		`SELECT materialized_environment_id FROM task_workspace_groups WHERE id = 'group-w047'`,
	).Scan(&materializedEnvironmentID); err != nil {
		t.Fatalf("read workspace group: %v", err)
	}
	if materializedEnvironmentID != "" {
		t.Fatalf("materialized_environment_id = %q, want cleared so the group can re-elect", materializedEnvironmentID)
	}
	if env := mustEnvironment(t, repo, "env-shared"); env.Status != models.TaskEnvironmentStatusFailed {
		t.Fatalf("status = %q, want failed", env.Status)
	}
}
