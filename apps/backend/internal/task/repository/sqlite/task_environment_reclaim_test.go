package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
)

func seedReclaimEnvironment(t *testing.T, repo *Repository, suffix, executorType string, mutate func(*models.TaskEnvironment)) *models.TaskEnvironment {
	t.Helper()
	ctx := context.Background()
	workspaceID := "workspace-reclaim-" + suffix
	taskID := "task-reclaim-" + suffix
	seedWorkspace(t, repo, workspaceID)
	if err := repo.CreateTask(ctx, &models.Task{ID: taskID, WorkspaceID: workspaceID, Title: "reclaim"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// A never-materialized environment is born creating without a workspace
	// path and reaches failed through the ownership closure writes, so seed it
	// through the same sequence instead of inserting a failed row directly.
	env := &models.TaskEnvironment{
		ID:           "environment-reclaim-" + suffix,
		TaskID:       taskID,
		ExecutorType: executorType,
		Status:       models.TaskEnvironmentStatusCreating,
	}
	if mutate != nil {
		mutate(env)
	}
	if err := repo.CreateTaskEnvironment(ctx, env); err != nil {
		t.Fatalf("CreateTaskEnvironment: %v", err)
	}
	env.Status = models.TaskEnvironmentStatusFailed
	if err := repo.UpdateTaskEnvironment(ctx, env); err != nil {
		t.Fatalf("mark environment failed: %v", err)
	}
	return env
}

const reclaimWorktreeExecutor = string(models.ExecutorTypeWorktree)

func TestReclaimFailedTaskEnvironmentMaterializationReelectsOwnerlessFailedEnvironment(t *testing.T) {
	repo := newRepoForEntityTests(t)
	ctx := context.Background()
	env := seedReclaimEnvironment(t, repo, "basic", reclaimWorktreeExecutor, nil)

	reclaimed, err := repo.ReclaimFailedTaskEnvironmentMaterialization(ctx, env.ID, "session-reclaim-a")
	if err != nil || !reclaimed {
		t.Fatalf("reclaim = (%v, %v), want reclaimed", reclaimed, err)
	}
	persisted, err := repo.GetTaskEnvironment(ctx, env.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != models.TaskEnvironmentStatusCreating || persisted.MaterializationSessionID != "session-reclaim-a" {
		t.Fatalf("reclaimed environment = status %q owner %q, want creating owned by the re-elected session", persisted.Status, persisted.MaterializationSessionID)
	}

	// A second reclaim must lose: the row is owned again and creating.
	reclaimed, err = repo.ReclaimFailedTaskEnvironmentMaterialization(ctx, env.ID, "session-reclaim-b")
	if err != nil || reclaimed {
		t.Fatalf("second reclaim = (%v, %v), want refused", reclaimed, err)
	}
}

func TestReclaimFailedTaskEnvironmentMaterializationClearsStaleContainerHandles(t *testing.T) {
	repo := newRepoForEntityTests(t)
	ctx := context.Background()
	env := seedReclaimEnvironment(t, repo, "handles", reclaimWorktreeExecutor, func(env *models.TaskEnvironment) {
		env.ExecutorType = string(models.ExecutorTypeLocalDocker)
		env.ContainerBootstrapNonceSecretID = "stale-bootstrap"
		env.ContainerControlAuthTokenSecretID = "stale-control"
	})

	reclaimed, err := repo.ReclaimFailedTaskEnvironmentMaterialization(ctx, env.ID, "session-reclaim-handles")
	if err != nil || !reclaimed {
		t.Fatalf("reclaim = (%v, %v), want reclaimed", reclaimed, err)
	}
	persisted, err := repo.GetTaskEnvironment(ctx, env.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ContainerBootstrapNonceSecretID != "" || persisted.ContainerControlAuthTokenSecretID != "" {
		t.Fatalf("stale container secret references survived reclaim: %+v", persisted)
	}
}

func TestReclaimFailedTaskEnvironmentMaterializationRefusesMaterializedEnvironments(t *testing.T) {
	cases := []struct {
		name   string
		suffix string
		mutate func(*models.TaskEnvironment)
	}{
		{name: "workspace path", suffix: "path", mutate: func(env *models.TaskEnvironment) { env.WorkspacePath = "/tasks/materialized" }},
		{name: "container handle", suffix: "container", mutate: func(env *models.TaskEnvironment) {
			env.ExecutorType = string(models.ExecutorTypeLocalDocker)
			env.ContainerID = "container-1"
		}},
		{name: "sandbox handle", suffix: "sandbox", mutate: func(env *models.TaskEnvironment) { env.SandboxID = "sandbox-1" }},
		{name: "claimed owner", suffix: "claimed", mutate: func(env *models.TaskEnvironment) { env.MaterializationSessionID = "session-still-owned" }},
		{name: "live repository row", suffix: "repo-row", mutate: func(env *models.TaskEnvironment) {
			env.WorkspacePath = "/tasks/materialized"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepoForEntityTests(t)
			ctx := context.Background()
			env := seedReclaimEnvironment(t, repo, tc.suffix, reclaimWorktreeExecutor, tc.mutate)
			if tc.suffix == "repo-row" {
				if err := repo.CreateTaskEnvironmentRepo(ctx, &models.TaskEnvironmentRepo{
					ID: env.ID + "-repo-1", TaskEnvironmentID: env.ID, RepositoryID: "reclaim-repo-1",
					WorktreeID: "wt-1", WorktreePath: "/tasks/materialized", BranchSlug: "main",
				}); err != nil {
					t.Fatalf("CreateTaskEnvironmentRepo: %v", err)
				}
				env.WorkspacePath = ""
				if err := repo.UpdateTaskEnvironment(ctx, env); err != nil {
					t.Fatalf("clear workspace path: %v", err)
				}
			}
			reclaimed, err := repo.ReclaimFailedTaskEnvironmentMaterialization(ctx, env.ID, "session-reclaim-x")
			if err != nil || reclaimed {
				t.Fatalf("reclaim = (%v, %v), want refused without error", reclaimed, err)
			}
			persisted, loadErr := repo.GetTaskEnvironment(ctx, env.ID)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if persisted.Status != models.TaskEnvironmentStatusFailed {
				t.Fatalf("refused environment status = %q, want failed", persisted.Status)
			}
		})
	}
}

func TestReclaimFailedTaskEnvironmentMaterializationRefusesNonFailedStatuses(t *testing.T) {
	for _, status := range []models.TaskEnvironmentStatus{
		models.TaskEnvironmentStatusCreating,
		models.TaskEnvironmentStatusReady,
		models.TaskEnvironmentStatusStopped,
	} {
		t.Run(string(status), func(t *testing.T) {
			repo := newRepoForEntityTests(t)
			ctx := context.Background()
			env := seedReclaimEnvironment(t, repo, "status-"+string(status), string(models.ExecutorTypeLocal), nil)
			env.Status = status
			env.MaterializationSessionID = ""
			if err := repo.UpdateTaskEnvironment(ctx, env); err != nil {
				t.Fatalf("seed status %q: %v", status, err)
			}
			reclaimed, err := repo.ReclaimFailedTaskEnvironmentMaterialization(ctx, env.ID, "session-reclaim-s")
			if err != nil || reclaimed {
				t.Fatalf("reclaim = (%v, %v), want refused", reclaimed, err)
			}
		})
	}
}

func TestReclaimFailedTaskEnvironmentMaterializationMissingEnvironment(t *testing.T) {
	repo := newRepoForEntityTests(t)
	reclaimed, err := repo.ReclaimFailedTaskEnvironmentMaterialization(context.Background(), "missing-environment", "session-reclaim-m")
	if err != nil || reclaimed {
		t.Fatalf("reclaim missing = (%v, %v), want false without error", reclaimed, err)
	}
}

func TestCreateTaskSessionWithWorkspaceBindingReclaimsFailedNeverMaterializedEnvironment(t *testing.T) {
	repo := newRepoForEntityTests(t)
	ctx := context.Background()
	env := seedReclaimEnvironment(t, repo, "binding", reclaimWorktreeExecutor, nil)

	session := &models.TaskSession{ID: "session-binding-new", TaskID: env.TaskID}
	if err := repo.CreateTaskSessionWithWorkspaceBinding(ctx, session, &models.TaskEnvironment{TaskID: env.TaskID}); err != nil {
		t.Fatalf("bind over failed environment: %v", err)
	}
	if session.TaskEnvironmentID != env.ID {
		t.Fatalf("bound environment = %q, want reclaimed row %q", session.TaskEnvironmentID, env.ID)
	}
	persisted, err := repo.GetTaskEnvironment(ctx, env.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != models.TaskEnvironmentStatusCreating || persisted.MaterializationSessionID != session.ID {
		t.Fatalf("reclaimed binding = status %q owner %q, want creating owned by the new session", persisted.Status, persisted.MaterializationSessionID)
	}
}

func TestCreateTaskSessionWithWorkspaceBindingStillRejectsMaterializedFailedEnvironment(t *testing.T) {
	repo := newRepoForEntityTests(t)
	ctx := context.Background()
	env := seedReclaimEnvironment(t, repo, "binding-materialized", reclaimWorktreeExecutor, func(env *models.TaskEnvironment) {
		env.WorkspacePath = "/tasks/materialized-failed"
	})

	session := &models.TaskSession{ID: "session-binding-rejected", TaskID: env.TaskID}
	err := repo.CreateTaskSessionWithWorkspaceBinding(ctx, session, &models.TaskEnvironment{TaskID: env.TaskID})
	if !errors.Is(err, models.ErrWorkspaceReuseUnsafe) {
		t.Fatalf("bind over materialized failed environment error = %v, want ErrWorkspaceReuseUnsafe", err)
	}
	sessions, err := repo.ListTaskSessions(ctx, env.TaskID)
	if err != nil || len(sessions) != 0 {
		t.Fatalf("sessions after rejected bind = %+v, %v", sessions, err)
	}
}

func TestCreateTaskSessionWithSharedGroupWorkspaceBindingReclaimsFailedEnvironment(t *testing.T) {
	repo := newRepoForEntityTests(t)
	ctx := context.Background()
	seedWorkspace(t, repo, "workspace-group-reclaim")
	taskID := "task-group-reclaim"
	if err := repo.CreateTask(ctx, &models.Task{ID: taskID, WorkspaceID: "workspace-group-reclaim", Title: taskID}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`
		CREATE TABLE task_workspace_groups (
			id TEXT PRIMARY KEY,
			materialized_environment_id TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMP NOT NULL
		);
		CREATE TABLE task_workspace_group_members (
			workspace_group_id TEXT NOT NULL,
			task_id TEXT NOT NULL,
			released_at TIMESTAMP NULL
		);
		INSERT INTO task_workspace_groups (id, updated_at) VALUES ('group-reclaim', CURRENT_TIMESTAMP);
		INSERT INTO task_workspace_group_members (workspace_group_id, task_id) VALUES ('group-reclaim', ?);
	`, taskID); err != nil {
		t.Fatalf("seed workspace group: %v", err)
	}

	// A local executor environment can be seeded failed directly: the
	// workspace-path restriction only applies to worktree environments.
	env := &models.TaskEnvironment{
		ID: "environment-group-reclaim", TaskID: taskID,
		ExecutorType: string(models.ExecutorTypeLocal), Status: models.TaskEnvironmentStatusFailed,
	}
	if err := repo.CreateTaskEnvironment(ctx, env); err != nil {
		t.Fatalf("CreateTaskEnvironment: %v", err)
	}
	if _, err := repo.db.Exec(`UPDATE task_workspace_groups SET materialized_environment_id = ? WHERE id = 'group-reclaim'`, env.ID); err != nil {
		t.Fatalf("seed group pointer: %v", err)
	}

	session := &models.TaskSession{ID: "session-group-reclaim", TaskID: taskID}
	if err := repo.CreateTaskSessionWithSharedGroupWorkspaceBinding(ctx, session, &models.TaskEnvironment{TaskID: taskID}, "group-reclaim"); err != nil {
		t.Fatalf("bind over failed shared-group environment: %v", err)
	}
	if session.TaskEnvironmentID != env.ID {
		t.Fatalf("bound shared environment = %q, want reclaimed row %q", session.TaskEnvironmentID, env.ID)
	}
	persisted, err := repo.GetTaskEnvironment(ctx, env.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != models.TaskEnvironmentStatusCreating || persisted.MaterializationSessionID != session.ID {
		t.Fatalf("reclaimed shared environment = status %q owner %q, want creating owned by the new session", persisted.Status, persisted.MaterializationSessionID)
	}

	// The next member observes workspace-preparing against the canonical row.
	blocked := &models.TaskSession{ID: "session-group-blocked", TaskID: taskID}
	err = repo.CreateTaskSessionWithSharedGroupWorkspaceBinding(ctx, blocked, &models.TaskEnvironment{TaskID: taskID}, "group-reclaim")
	if !errors.Is(err, models.ErrWorkspacePreparing) {
		t.Fatalf("second shared bind error = %v, want preparing", err)
	}
}
