package executor

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/task/models"
)

func TestFailOwnedCreatingTaskEnvironment_MarksOwnCreatingFailed(t *testing.T) {
	log, err := logger.NewLogger(logger.LoggingConfig{Level: "error", Format: "json"})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	repo := newMockRepository()
	repo.taskEnvironments["env-own"] = &models.TaskEnvironment{
		ID: "env-own", TaskID: "task-own", ExecutorType: string(models.ExecutorTypeWorktree),
		Status: models.TaskEnvironmentStatusCreating, MaterializationSessionID: "sess-own",
	}
	e := &Executor{logger: log, repo: repo}

	e.failOwnedCreatingTaskEnvironment(context.Background(), "task-own", "sess-own")

	env := repo.taskEnvironments["env-own"]
	if env.Status != models.TaskEnvironmentStatusFailed {
		t.Fatalf("status = %q, want failed", env.Status)
	}
	if env.MaterializationSessionID != "" {
		t.Fatalf("materialization_session_id = %q, want cleared", env.MaterializationSessionID)
	}
}

func TestFailOwnedCreatingTaskEnvironment_LeavesForeignOwner(t *testing.T) {
	log, err := logger.NewLogger(logger.LoggingConfig{Level: "error", Format: "json"})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	repo := newMockRepository()
	repo.taskEnvironments["env-foreign"] = &models.TaskEnvironment{
		ID: "env-foreign", TaskID: "task-own", ExecutorType: string(models.ExecutorTypeWorktree),
		Status: models.TaskEnvironmentStatusCreating, MaterializationSessionID: "sess-other",
	}
	e := &Executor{logger: log, repo: repo}

	e.failOwnedCreatingTaskEnvironment(context.Background(), "task-own", "sess-own")

	env := repo.taskEnvironments["env-foreign"]
	if env.Status != models.TaskEnvironmentStatusCreating || env.MaterializationSessionID != "sess-other" {
		t.Fatalf("foreign materialization changed: status=%q owner=%q", env.Status, env.MaterializationSessionID)
	}
}
