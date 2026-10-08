package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

func seedOwnedCreatingEnvironment(repo *mockRepository, taskID, sessionID string) *models.TaskEnvironment {
	env := &models.TaskEnvironment{
		ID:                       "env-" + taskID,
		TaskID:                   taskID,
		ExecutorType:             string(models.ExecutorTypeWorktree),
		Status:                   models.TaskEnvironmentStatusCreating,
		MaterializationSessionID: sessionID,
	}
	repo.taskEnvironments[env.ID] = env
	repo.sessions[sessionID] = &models.TaskSession{ID: sessionID, TaskID: taskID}
	return env
}

func TestLaunchPreparedSessionClosesOwnedCreatingEnvironmentOnEarlyFailure(t *testing.T) {
	repo := newMockRepository()
	env := seedOwnedCreatingEnvironment(repo, "task-1", "session-1")
	repo.getTaskFunc = func(ctx context.Context, id string) (*models.Task, error) {
		return nil, errors.New("db unavailable")
	}
	e := newTestExecutor(t, &mockAgentManager{}, repo)

	_, err := e.LaunchPreparedSession(context.Background(), &v1.Task{ID: "task-1"}, "session-1", LaunchOptions{
		AgentProfileID: "profile-1", Prompt: "hello", StartAgent: true,
	})
	if err == nil {
		t.Fatal("LaunchPreparedSession() error = nil, want an early launch failure")
	}
	if env.Status != models.TaskEnvironmentStatusFailed || env.MaterializationSessionID != "" {
		t.Fatalf("owned creating environment after early failure = status %q owner %q, want failed with cleared owner", env.Status, env.MaterializationSessionID)
	}
}

func TestLaunchPreparedSessionLeavesOwnedCreatingEnvironmentForAlreadyRunning(t *testing.T) {
	repo := newMockRepository()
	env := seedOwnedCreatingEnvironment(repo, "task-1", "session-1")
	repo.tasks["task-1"] = &models.Task{ID: "task-1"}
	session := repo.sessions["session-1"]
	session.State = models.TaskSessionStateRunning
	agentManager := &mockAgentManager{isAgentRunningForSessionFunc: func(ctx context.Context, sessionID string) bool {
		return sessionID == "session-1"
	}}
	e := newTestExecutor(t, agentManager, repo)

	_, err := e.LaunchPreparedSession(context.Background(), &v1.Task{ID: "task-1"}, "session-1", LaunchOptions{
		AgentProfileID: "profile-1", Prompt: "hello", StartAgent: true,
	})
	if !errors.Is(err, ErrExecutionAlreadyRunning) {
		t.Fatalf("LaunchPreparedSession() error = %v, want ErrExecutionAlreadyRunning", err)
	}
	if env.Status != models.TaskEnvironmentStatusCreating || env.MaterializationSessionID != "session-1" {
		t.Fatalf("owned creating environment after already-running = status %q owner %q, want the live claim preserved", env.Status, env.MaterializationSessionID)
	}
}

func TestReelectFailedTaskEnvironmentForLaunchReclaimsNeverMaterializedEnvironment(t *testing.T) {
	repo := newMockRepository()
	env := &models.TaskEnvironment{
		ID:           "env-failed-1",
		TaskID:       "task-1",
		ExecutorType: string(models.ExecutorTypeWorktree),
		Status:       models.TaskEnvironmentStatusFailed,
	}
	repo.taskEnvironments[env.ID] = env
	e := newTestExecutor(t, &mockAgentManager{}, repo)
	session := &models.TaskSession{ID: "session-retry", TaskID: "task-1"}

	if err := e.reelectFailedTaskEnvironmentForLaunch(context.Background(), &v1.Task{ID: "task-1"}, session, env); err != nil {
		t.Fatalf("reelectFailedTaskEnvironmentForLaunch() error = %v", err)
	}
	if env.Status != models.TaskEnvironmentStatusCreating || env.MaterializationSessionID != session.ID {
		t.Fatalf("reclaimed environment = status %q owner %q, want creating owned by the retrying session", env.Status, env.MaterializationSessionID)
	}
}

func TestReelectFailedTaskEnvironmentForLaunchFailsClosed(t *testing.T) {
	cases := []struct {
		name string
		env  *models.TaskEnvironment
	}{
		{
			name: "materialized workspace path",
			env: &models.TaskEnvironment{
				ID: "env-mat-path", TaskID: "task-1", Status: models.TaskEnvironmentStatusFailed,
				WorkspacePath: "/tasks/materialized",
			},
		},
		{
			name: "live repository inventory",
			env: &models.TaskEnvironment{
				ID: "env-mat-repos", TaskID: "task-1", Status: models.TaskEnvironmentStatusFailed,
				Repos: []*models.TaskEnvironmentRepo{{ID: "row-1", RepositoryID: "repo-1", WorktreeID: "wt-1"}},
			},
		},
		{
			name: "claimed by another session",
			env: &models.TaskEnvironment{
				ID: "env-mat-claimed", TaskID: "task-1", Status: models.TaskEnvironmentStatusFailed,
				MaterializationSessionID: "session-other",
			},
		},
		{
			name: "inherited environment of another task",
			env: &models.TaskEnvironment{
				ID: "env-mat-inherited", TaskID: "task-parent", Status: models.TaskEnvironmentStatusFailed,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMockRepository()
			repo.taskEnvironments[tc.env.ID] = tc.env
			e := newTestExecutor(t, &mockAgentManager{}, repo)
			session := &models.TaskSession{ID: "session-retry", TaskID: "task-1"}

			err := e.reelectFailedTaskEnvironmentForLaunch(context.Background(), &v1.Task{ID: "task-1"}, session, tc.env)
			if !errors.Is(err, models.ErrWorkspaceReuseUnsafe) {
				t.Fatalf("reelectFailedTaskEnvironmentForLaunch() error = %v, want ErrWorkspaceReuseUnsafe", err)
			}
			if tc.env.Status != models.TaskEnvironmentStatusFailed {
				t.Fatalf("environment status = %q, want failed", tc.env.Status)
			}
		})
	}
}

func TestReelectFailedTaskEnvironmentForLaunchWaitsForConcurrentReclaim(t *testing.T) {
	repo := newMockRepository()
	env := &models.TaskEnvironment{
		ID:           "env-raced-1",
		TaskID:       "task-1",
		ExecutorType: string(models.ExecutorTypeWorktree),
		Status:       models.TaskEnvironmentStatusFailed,
	}
	repo.taskEnvironments[env.ID] = env
	// A concurrent launch wins the reclaim CAS and publishes ready before this
	// launch re-reads the row.
	repo.reclaimFailedTaskEnvironmentFunc = func(ctx context.Context, environmentID, sessionID string) (bool, error) {
		env.Status = models.TaskEnvironmentStatusReady
		env.MaterializationSessionID = "session-winner"
		return false, nil
	}
	e := newTestExecutor(t, &mockAgentManager{}, repo)
	session := &models.TaskSession{ID: "session-loser", TaskID: "task-1"}

	if err := e.reelectFailedTaskEnvironmentForLaunch(context.Background(), &v1.Task{ID: "task-1"}, session, env); err != nil {
		t.Fatalf("reelectFailedTaskEnvironmentForLaunch() error = %v", err)
	}
	if env.Status != models.TaskEnvironmentStatusReady || env.MaterializationSessionID != "session-winner" {
		t.Fatalf("environment after lost race = status %q owner %q, want the winner's ready row", env.Status, env.MaterializationSessionID)
	}
}
