package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	storageworkspaces "github.com/kandev/kandev/internal/system/storage/workspaces"
)

const retainedTaskDirName = "task-retained_aaa"

// newRetainedCheckoutFixture builds a Manager over a fresh clone plus a mock
// store, and returns the assigned flat worktree path for the task.
func newRetainedCheckoutFixture(t *testing.T) (mgr *Manager, store *mockStore, repoPath, worktreePath string) {
	t.Helper()
	repoPath = initGitRepoForNewBranchTest(t)
	cfg := newTestConfig(t)
	store = newMockStore()
	var err error
	mgr, err = NewManager(cfg, store, newTestLogger())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	worktreePath, err = cfg.TaskWorktreePath(retainedTaskDirName, "repo-1", "")
	if err != nil {
		t.Fatalf("TaskWorktreePath: %v", err)
	}
	return mgr, store, repoPath, worktreePath
}

func retainedCreateRequest(repoPath string) CreateRequest {
	return CreateRequest{
		TaskID:         "task-retained",
		SessionID:      "session-retained",
		TaskTitle:      "Retained checkout reconciliation",
		RepositoryID:   "repo-1",
		RepositoryPath: repoPath,
		BaseBranch:     "main",
		TaskDirName:    retainedTaskDirName,
		RepoName:       "repo-1",
	}
}

func addRetainedOccupantWorktree(t *testing.T, repoPath, worktreePath, branch string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatalf("create task dir: %v", err)
	}
	runGit(t, repoPath, "branch", branch)
	runGit(t, repoPath, "worktree", "add", worktreePath, branch)
}

func currentBranch(t *testing.T, worktreePath string) string {
	t.Helper()
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = worktreePath
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git branch --show-current: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestRetainedCheckoutEligibleOldBranchIsRetired covers the reported defect: a
// clean, reflected retained same-task worktree on a different branch must be
// retired so fresh preparation succeeds instead of failing git worktree add.
func TestRetainedCheckoutEligibleOldBranchIsRetired(t *testing.T) {
	mgr, _, repoPath, worktreePath := newRetainedCheckoutFixture(t)
	addRetainedOccupantWorktree(t, repoPath, worktreePath, "old-branch")

	wt, err := mgr.Create(context.Background(), retainedCreateRequest(repoPath))
	if err != nil {
		t.Fatalf("Create() = %v, want success after retiring the eligible occupant", err)
	}
	if wt.Path != worktreePath {
		t.Fatalf("worktree path = %q, want %q", wt.Path, worktreePath)
	}
	if got := currentBranch(t, worktreePath); got == "old-branch" {
		t.Fatalf("stale occupant branch %q still checked out at %q", got, worktreePath)
	}
}

// TestRetainedCheckoutDirtyIsPreserved proves uncommitted content blocks
// retirement and is preserved in place.
func TestRetainedCheckoutDirtyIsPreserved(t *testing.T) {
	mgr, _, repoPath, worktreePath := newRetainedCheckoutFixture(t)
	addRetainedOccupantWorktree(t, repoPath, worktreePath, "old-branch")
	scratch := filepath.Join(worktreePath, "scratch.txt")
	if err := os.WriteFile(scratch, []byte("uncommitted"), 0o644); err != nil {
		t.Fatalf("write scratch: %v", err)
	}

	if _, err := mgr.Create(context.Background(), retainedCreateRequest(repoPath)); !errors.Is(err, ErrWorktreeCorrupted) {
		t.Fatalf("Create() err = %v, want ErrWorktreeCorrupted (preserve)", err)
	}
	if _, err := os.Stat(scratch); err != nil {
		t.Fatalf("dirty content not preserved: %v", err)
	}
}

// TestRetainedCheckoutIgnoredContentIsPreserved proves that ignored unique
// content also blocks retirement, so it is never silently discarded.
func TestRetainedCheckoutIgnoredContentIsPreserved(t *testing.T) {
	mgr, _, repoPath, worktreePath := newRetainedCheckoutFixture(t)
	if err := os.WriteFile(filepath.Join(repoPath, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	runGit(t, repoPath, "add", ".gitignore")
	runGit(t, repoPath, "commit", "-m", "ignore logs")
	addRetainedOccupantWorktree(t, repoPath, worktreePath, "old-branch")
	ignored := filepath.Join(worktreePath, "kept.log")
	if err := os.WriteFile(ignored, []byte("ignored-but-unique"), 0o644); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}

	if _, err := mgr.Create(context.Background(), retainedCreateRequest(repoPath)); !errors.Is(err, ErrWorktreeCorrupted) {
		t.Fatalf("Create() err = %v, want ErrWorktreeCorrupted (preserve ignored)", err)
	}
	if _, err := os.Stat(ignored); err != nil {
		t.Fatalf("ignored content not preserved: %v", err)
	}
}

// TestRetainedCheckoutUnreflectedIsPreserved proves unique committed work blocks
// retirement and is preserved.
func TestRetainedCheckoutUnreflectedIsPreserved(t *testing.T) {
	mgr, _, repoPath, worktreePath := newRetainedCheckoutFixture(t)
	addRetainedOccupantWorktree(t, repoPath, worktreePath, "old-branch")
	unique := filepath.Join(worktreePath, "unique.txt")
	if err := os.WriteFile(unique, []byte("unique work not in main"), 0o644); err != nil {
		t.Fatalf("write unique: %v", err)
	}
	runGit(t, worktreePath, "add", "unique.txt")
	runGit(t, worktreePath, "commit", "-m", "unique work")

	if _, err := mgr.Create(context.Background(), retainedCreateRequest(repoPath)); !errors.Is(err, ErrWorktreeCorrupted) {
		t.Fatalf("Create() err = %v, want ErrWorktreeCorrupted (preserve unreflected)", err)
	}
	if _, err := os.Stat(unique); err != nil {
		t.Fatalf("unreflected content not preserved: %v", err)
	}
}

// TestRetainedCheckoutVerifiedSquashIsRetired proves that verified non-ancestor
// (patch-equivalent) integration is eligible for retirement.
func TestRetainedCheckoutVerifiedSquashIsRetired(t *testing.T) {
	mgr, _, repoPath, worktreePath := newRetainedCheckoutFixture(t)
	if err := os.WriteFile(filepath.Join(repoPath, "shared.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write shared: %v", err)
	}
	runGit(t, repoPath, "add", "shared.txt")
	runGit(t, repoPath, "commit", "-m", "base")

	runGit(t, repoPath, "checkout", "-b", "old-branch")
	if err := os.WriteFile(filepath.Join(repoPath, "x.txt"), []byte("patch-equivalent\n"), 0o644); err != nil {
		t.Fatalf("write x on old-branch: %v", err)
	}
	runGit(t, repoPath, "add", "x.txt")
	runGit(t, repoPath, "commit", "-m", "x on old-branch")
	runGit(t, repoPath, "checkout", "main")
	if err := os.WriteFile(filepath.Join(repoPath, "x.txt"), []byte("patch-equivalent\n"), 0o644); err != nil {
		t.Fatalf("write squash x on main: %v", err)
	}
	runGit(t, repoPath, "add", "x.txt")
	runGit(t, repoPath, "commit", "-m", "squash x")

	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatalf("create task dir: %v", err)
	}
	runGit(t, repoPath, "worktree", "add", worktreePath, "old-branch")

	wt, err := mgr.Create(context.Background(), retainedCreateRequest(repoPath))
	if err != nil {
		t.Fatalf("Create() = %v, want success for verified non-ancestor reflection", err)
	}
	if got := currentBranch(t, filepath.Clean(wt.Path)); got == "old-branch" {
		t.Fatalf("stale occupant branch still present at %q", wt.Path)
	}
}

// TestRetainedCheckoutAbsentCreatesFresh is the baseline: no occupant means
// ordinary creation is unchanged.
func TestRetainedCheckoutAbsentCreatesFresh(t *testing.T) {
	mgr, _, repoPath, _ := newRetainedCheckoutFixture(t)
	wt, err := mgr.Create(context.Background(), retainedCreateRequest(repoPath))
	if err != nil {
		t.Fatalf("Create() = %v, want success", err)
	}
	if wt.Branch == "" {
		t.Fatal("expected a created branch")
	}
}

// TestRetainedCheckoutActiveRowIsPreserved proves a durable active row claiming
// the path blocks retirement even when the checkout itself is clean/reflected.
func TestRetainedCheckoutActiveRowIsPreserved(t *testing.T) {
	mgr, store, repoPath, worktreePath := newRetainedCheckoutFixture(t)
	addRetainedOccupantWorktree(t, repoPath, worktreePath, "old-branch")
	store.worktrees["wt-active"] = &Worktree{
		ID: "wt-active", TaskID: "task-retained", RepositoryID: "repo-1",
		Path: worktreePath, Status: StatusActive,
	}

	if _, err := mgr.Create(context.Background(), retainedCreateRequest(repoPath)); !errors.Is(err, ErrWorktreeCorrupted) {
		t.Fatalf("Create() err = %v, want ErrWorktreeCorrupted (preserve active owner)", err)
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("active-owned checkout not preserved: %v", err)
	}
}

// TestRetainedCheckoutForeignTaskMarkerIsPreserved proves a task-root ownership
// marker for another task blocks preparation without rebranding the root.
func TestRetainedCheckoutForeignTaskMarkerIsPreserved(t *testing.T) {
	mgr, _, repoPath, worktreePath := newRetainedCheckoutFixture(t)
	taskDir := filepath.Dir(worktreePath)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("create task dir: %v", err)
	}
	if err := storageworkspaces.WriteOwnershipMarker(taskDir, storageworkspaces.OwnershipMarker{
		TaskID: "other-task", TaskDirName: "other_zzz", LayoutVersion: storageworkspaces.LayoutVersionSemantic,
	}); err != nil {
		t.Fatalf("write foreign marker: %v", err)
	}
	runGit(t, repoPath, "branch", "old-branch")
	runGit(t, repoPath, "worktree", "add", worktreePath, "old-branch")

	if _, err := mgr.Create(context.Background(), retainedCreateRequest(repoPath)); !errors.Is(err, ErrWorktreePathOwnedByAnotherTask) {
		t.Fatalf("Create() err = %v, want ErrWorktreePathOwnedByAnotherTask", err)
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("foreign-root checkout not preserved: %v", err)
	}
}

// TestRetainedCheckoutForeignRepositoryIsPreserved proves a healthy linked
// checkout of a different repository is preserved, not retired.
func TestRetainedCheckoutForeignRepositoryIsPreserved(t *testing.T) {
	mgr, _, repoPath, worktreePath := newRetainedCheckoutFixture(t)
	otherRepo := initGitRepoForNewBranchTest(t)
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatalf("create task dir: %v", err)
	}
	runGit(t, otherRepo, "branch", "old-branch")
	runGit(t, otherRepo, "worktree", "add", worktreePath, "old-branch")

	if _, err := mgr.Create(context.Background(), retainedCreateRequest(repoPath)); !errors.Is(err, ErrWorktreeCorrupted) {
		t.Fatalf("Create() err = %v, want ErrWorktreeCorrupted (preserve foreign repo)", err)
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("foreign-repository checkout not preserved: %v", err)
	}
}
