package service

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
)

// TestService_ArchiveTaskIfIdle_RefusesActiveSession proves the settlement
// archive refuses rather than stops a session that is live, so a reply racing
// a failed-run settlement can never be terminated.
func TestService_ArchiveTaskIfIdle_RefusesActiveSession(t *testing.T) {
	svc, _, repo := createTestService(t)
	ctx := context.Background()
	_ = repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws-1", Name: "WS"})
	_ = repo.CreateWorkflow(ctx, &models.Workflow{ID: "wf-1", WorkspaceID: "ws-1", Name: "WF"})
	if err := repo.CreateTask(ctx, &models.Task{
		ID: "task-1", WorkspaceID: "ws-1", WorkflowID: "wf-1", WorkflowStepID: "step-1", Title: "T",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := repo.CreateTaskSession(ctx, &models.TaskSession{
		ID: "session-1", TaskID: "task-1", State: models.TaskSessionStateRunning,
		AgentProfileID: "agent-1", IsPrimary: true,
	}); err != nil {
		t.Fatalf("CreateTaskSession: %v", err)
	}

	err := svc.ArchiveTaskIfIdle(ctx, "task-1")
	if !errors.Is(err, ErrTaskActiveSessions) {
		t.Fatalf("ArchiveTaskIfIdle with a running session: want ErrTaskActiveSessions, got %v", err)
	}
	task, getErr := repo.GetTask(ctx, "task-1")
	if getErr != nil {
		t.Fatalf("GetTask: %v", getErr)
	}
	if task.ArchivedAt != nil {
		t.Fatal("task must not be archived while a session is live")
	}
}

// TestService_ArchiveTaskIfIdle_ArchivesWhenIdle proves an idle task archives
// through the same path, so the refusal only applies to live sessions.
func TestService_ArchiveTaskIfIdle_ArchivesWhenIdle(t *testing.T) {
	svc, _, repo := createTestService(t)
	ctx := context.Background()
	_ = repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws-1", Name: "WS"})
	_ = repo.CreateWorkflow(ctx, &models.Workflow{ID: "wf-1", WorkspaceID: "ws-1", Name: "WF"})
	if err := repo.CreateTask(ctx, &models.Task{
		ID: "task-1", WorkspaceID: "ws-1", WorkflowID: "wf-1", WorkflowStepID: "step-1", Title: "T",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := svc.ArchiveTaskIfIdle(ctx, "task-1"); err != nil {
		t.Fatalf("ArchiveTaskIfIdle on an idle task: %v", err)
	}
	task, getErr := repo.GetTask(ctx, "task-1")
	if getErr != nil {
		t.Fatalf("GetTask: %v", getErr)
	}
	if task.ArchivedAt == nil {
		t.Fatal("idle task must be archived")
	}
}
