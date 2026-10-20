package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
)

// A non-cascade delete of a parent whose direct inherit_parent child is
// already archived must normalize that child too. The SQLite guard used to
// require the child to be unarchived, so the write matched zero rows and the
// delete failed with a 500.
func TestDeleteTaskTree_NoCascadeNormalizesArchivedInheritParentChild(t *testing.T) {
	for _, tc := range []struct {
		name   string
		marked bool
	}{
		{name: "unmarked child"},
		{name: "child carrying an orphan marker", marked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			taskSvc, repo := setupOfficeTest(t)
			taskSvc.StopTaskResourceCleanupWorker()
			ws, err := repo.GetWorkspace(ctx, "ws-1")
			if err != nil || ws == nil {
				t.Fatalf("GetWorkspace: workspace=%#v err=%v", ws, err)
			}
			workspace := map[string]interface{}{"mode": workspaceModeInheritParent}
			if tc.marked {
				stampOrphanedWorkspaceMetadata(workspace, "delete-archived-root")
			}
			for _, task := range []*models.Task{
				{
					ID: "delete-archived-root", WorkspaceID: ws.ID, WorkflowID: ws.OfficeWorkflowID,
					WorkflowStepID: "step", Title: "Root", Priority: models.TaskPriorityMedium,
				},
				{
					ID: "delete-archived-child", WorkspaceID: ws.ID, WorkflowID: ws.OfficeWorkflowID,
					WorkflowStepID: "step", Title: "Archived child", ParentID: "delete-archived-root",
					Priority: models.TaskPriorityMedium,
					Metadata: map[string]interface{}{"workspace": workspace},
				},
			} {
				if err := repo.CreateTask(ctx, task); err != nil {
					t.Fatalf("CreateTask(%s): %v", task.ID, err)
				}
			}
			if err := repo.ArchiveTask(ctx, "delete-archived-child"); err != nil {
				t.Fatalf("ArchiveTask(child): %v", err)
			}

			handoff := NewHandoffService(repo, repo, nil, nil, nil, nil)
			handoff.SetTaskResourceCleaner(taskSvc)
			if _, err := handoff.DeleteTaskTree(ctx, "delete-archived-root", false); err != nil {
				t.Fatalf("DeleteTaskTree(cascade=false): %v", err)
			}

			root, err := repo.GetTask(ctx, "delete-archived-root")
			if err == nil && root != nil {
				t.Fatal("root task still exists after delete")
			}
			child, err := repo.GetTask(ctx, "delete-archived-child")
			if err != nil || child == nil {
				t.Fatalf("GetTask(child): task=%#v err=%v", child, err)
			}
			if child.ParentID != "" {
				t.Fatalf("child.ParentID = %q, want promoted to root", child.ParentID)
			}
			if child.ArchivedAt == nil {
				t.Fatal("child must stay archived")
			}
			if got := taskWorkspaceMode(child.Metadata); got != workspaceModeSharedGroup {
				t.Fatalf("child workspace mode = %q, want %q", got, workspaceModeSharedGroup)
			}
			stored, _ := child.Metadata["workspace"].(map[string]interface{})
			for _, key := range []string{orphanedWorkspaceKey, orphanedReasonWorkspaceKey, orphanedParentIDKey, orphanedAtKey} {
				if _, ok := stored[key]; ok {
					t.Fatalf("child workspace still carries %q naming the deleted parent: %#v", key, stored)
				}
			}
		})
	}
}

// When the normalization guard really loses, the delete returns a typed
// conflict that names the child and the reason, and the error text carries
// the cause once rather than twice.
func TestDeleteTaskTree_NoCascadeLostGuardReturnsTypedConflict(t *testing.T) {
	tasks := newFakeTaskRepo()
	tasks.addTask("root", "", "ws-1")
	tasks.addTask("child", "root", "ws-1")
	tasks.tasks["child"].Metadata = map[string]interface{}{
		"workspace": map[string]interface{}{"mode": workspaceModeInheritParent},
	}
	repo := &raceInjectingDeleteRepo{
		fakeDeleteRepo: &fakeDeleteRepo{fakeCascadeRepo: newCascadeRepo(tasks)},
		raceTaskID:     "child",
	}
	svc := NewHandoffService(repo, nil, nil, nil, nil, nil)

	_, err := svc.DeleteTaskTree(context.Background(), "root", false)
	var conflict *TaskDeleteChildWorkspaceConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("DeleteTaskTree error = %v, want *TaskDeleteChildWorkspaceConflictError", err)
	}
	if conflict.ChildID != "child" {
		t.Fatalf("conflict.ChildID = %q, want %q", conflict.ChildID, "child")
	}
	if conflict.Reason != TaskDeleteChildWorkspaceChanged {
		t.Fatalf("conflict.Reason = %q, want %q", conflict.Reason, TaskDeleteChildWorkspaceChanged)
	}
	if !errors.Is(err, errWorkspaceMetadataChangedConcurrently) {
		t.Fatalf("DeleteTaskTree error = %v, want it to keep wrapping errWorkspaceMetadataChangedConcurrently", err)
	}
	if n := strings.Count(err.Error(), errWorkspaceMetadataChangedConcurrently.Error()); n != 1 {
		t.Fatalf("error text repeats the cause %d times, want 1: %q", n, err.Error())
	}
}
