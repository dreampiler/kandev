package sqlite

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
)

// seedFailedEnvironment leaves the task with a failed environment whose owner
// released its claim before anything physical was created.
func seedFailedEnvironment(t *testing.T, repo *Repository, taskID, ownerID string) string {
	t.Helper()
	owner := seedCreatingOwner(t, repo, taskID, ownerID, models.TaskSessionStateCreated)
	changed, err := repo.FailTaskEnvironmentMaterialization(context.Background(), owner.ID)
	if err != nil || !changed {
		t.Fatalf("fail seeded claim = %v, %v", changed, err)
	}
	return owner.TaskEnvironmentID
}

func addWorktreeEvidence(t *testing.T, repo *Repository, environmentID string) {
	t.Helper()
	if err := repo.CreateTaskEnvironmentRepo(context.Background(), &models.TaskEnvironmentRepo{
		TaskEnvironmentID: environmentID, RepositoryID: "repository-evidence",
		WorktreeID: "worktree-" + environmentID, WorktreePath: "/tasks/" + environmentID + "/repo", Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
}

// @covers AC-TASKS-TASK-LAUNCH-FAILURE-RECOVERY-004.4
func TestWorkspaceBindingReelectsFailedEnvironmentWithoutEvidence(t *testing.T) {
	repo := newRepoForEntityTests(t)
	seedMaterializationClaimTask(t, repo, "task-failed-reelect")
	environmentID := seedFailedEnvironment(t, repo, "task-failed-reelect", "session-failed-owner")

	retry, err := bindRetry(repo, "task-failed-reelect", "session-after-failure")
	if err != nil {
		t.Fatalf("bind after failed materialization: %v", err)
	}
	assertTakenOverMaterialization(t, repo, environmentID, retry)
}

// @covers AC-TASKS-TASK-LAUNCH-FAILURE-RECOVERY-004.5
func TestWorkspaceBindingKeepsFailedEnvironmentWithEvidence(t *testing.T) {
	repo := newRepoForEntityTests(t)
	ctx := context.Background()
	seedMaterializationClaimTask(t, repo, "task-failed-evidence")
	environmentID := seedFailedEnvironment(t, repo, "task-failed-evidence", "session-failed-evidence-owner")
	addWorktreeEvidence(t, repo, environmentID)

	_, err := bindRetry(repo, "task-failed-evidence", "session-after-evidence")
	if !errors.Is(err, models.ErrWorkspaceReuseUnsafe) {
		t.Fatalf("bind against failed environment with a worktree error = %v, want reuse unsafe", err)
	}
	environment, err := repo.GetTaskEnvironment(ctx, environmentID)
	if err != nil {
		t.Fatal(err)
	}
	if environment.Status != models.TaskEnvironmentStatusFailed || environment.MaterializationSessionID != "" {
		t.Fatalf("environment = status %q owner %q, want unchanged failed row", environment.Status, environment.MaterializationSessionID)
	}
	if _, err := repo.GetTaskSession(ctx, "session-after-evidence"); err == nil {
		t.Fatal("refused binding persisted a session")
	}
}

// @covers AC-TASKS-TASK-LAUNCH-FAILURE-RECOVERY-004.6
func TestWorkspaceBindingConcurrentFailedReelectionHasOneOwner(t *testing.T) {
	repo := newRepoForEntityTests(t)
	seedMaterializationClaimTask(t, repo, "task-failed-race")
	environmentID := seedFailedEnvironment(t, repo, "task-failed-race", "session-failed-race-owner")

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []string{"session-failed-race-a", "session-failed-race-b"} {
		wg.Add(1)
		go func(sessionID string) {
			defer wg.Done()
			<-start
			_, err := bindRetry(repo, "task-failed-race", sessionID)
			errs <- err
		}(id)
	}
	close(start)
	wg.Wait()
	close(errs)

	var succeeded, preparing int
	for err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, models.ErrWorkspacePreparing):
			preparing++
		default:
			t.Fatalf("concurrent re-election error = %v", err)
		}
	}
	if succeeded != 1 || preparing != 1 {
		t.Fatalf("re-election results: success=%d preparing=%d, want one each", succeeded, preparing)
	}
	environment, err := repo.GetTaskEnvironment(context.Background(), environmentID)
	if err != nil {
		t.Fatal(err)
	}
	if environment.Status != models.TaskEnvironmentStatusCreating {
		t.Fatalf("environment status = %q, want creating", environment.Status)
	}
}

// @covers AC-TASKS-TASK-LAUNCH-FAILURE-RECOVERY-004.4
func TestSharedGroupBindingReelectsFailedGroupEnvironment(t *testing.T) {
	repo := newRepoForEntityTests(t)
	ctx := context.Background()
	seedWorkspace(t, repo, "workspace-group-failed")
	for _, taskID := range []string{"task-group-failed-a", "task-group-failed-b"} {
		if err := repo.CreateTask(ctx, &models.Task{ID: taskID, WorkspaceID: "workspace-group-failed", Title: taskID}); err != nil {
			t.Fatal(err)
		}
	}
	seedSharedWorkspaceGroup(t, repo, "group-failed", "task-group-failed-a", "task-group-failed-b")
	owner := &models.TaskSession{ID: "session-group-failed-owner", TaskID: "task-group-failed-a"}
	if err := repo.CreateTaskSessionWithSharedGroupWorkspaceBinding(ctx, owner, &models.TaskEnvironment{
		TaskID: owner.TaskID, ExecutorType: string(models.ExecutorTypeLocal),
	}, "group-failed"); err != nil {
		t.Fatal(err)
	}
	if changed, err := repo.FailTaskEnvironmentMaterialization(ctx, owner.ID); err != nil || !changed {
		t.Fatalf("fail group claim = %v, %v", changed, err)
	}

	member := &models.TaskSession{ID: "session-group-failed-member", TaskID: "task-group-failed-b"}
	if err := repo.CreateTaskSessionWithSharedGroupWorkspaceBinding(ctx, member, &models.TaskEnvironment{
		TaskID: member.TaskID, ExecutorType: string(models.ExecutorTypeLocal),
	}, "group-failed"); err != nil {
		t.Fatalf("bind group member after failed materialization: %v", err)
	}
	if member.TaskEnvironmentID != owner.TaskEnvironmentID {
		t.Fatalf("member environment = %q, want group environment %q", member.TaskEnvironmentID, owner.TaskEnvironmentID)
	}
	assertCreatingOwner(t, repo, owner.TaskEnvironmentID, member.ID)
}
