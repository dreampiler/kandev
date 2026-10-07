package worktree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestCreate_ReuseRequiredRejectsCheckoutOfAnotherRepository verifies that a
// structurally healthy checkout that belongs to a different repository is not
// reused as this repository's workspace. Reuse must fail closed, and the
// existing checkout (which may hold another task's work) must be preserved
// rather than deleted.
func TestCreate_ReuseRequiredRejectsCheckoutOfAnotherRepository(t *testing.T) {
	repoPath := initGitRepoWithRemote(t)
	otherRepoPath := initGitRepoWithRemote(t)
	worktreePath := filepath.Join(t.TempDir(), "canonical")
	runGit(t, otherRepoPath, "worktree", "add", "-b", "unrelated", worktreePath, "main")
	marker := filepath.Join(worktreePath, "unrelated-uncommitted-marker")
	if err := os.WriteFile(marker, []byte("other repository state\n"), 0o600); err != nil {
		t.Fatalf("write uncommitted marker: %v", err)
	}

	store := newMockStore()
	store.worktrees["canonical-worktree"] = &Worktree{
		ID:                "canonical-worktree",
		TaskID:            "task-1",
		TaskEnvironmentID: "environment-1",
		RepositoryID:      "repository-1",
		Path:              worktreePath,
		Branch:            "main",
		Status:            StatusActive,
	}
	mgr, err := NewManager(newTestConfig(t), store, newTestLogger())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	_, err = mgr.Create(context.Background(), CreateRequest{
		TaskID:            "task-1",
		SessionID:         "session-2",
		TaskEnvironmentID: "environment-1",
		RepositoryID:      "repository-1",
		RepositoryPath:    repoPath,
		BaseBranch:        "main",
		WorktreeID:        "canonical-worktree",
		ReuseRequired:     true,
	})
	if !errors.Is(err, ErrReuseWorktreeUnavailable) {
		t.Fatalf("Create() error = %v, want ErrReuseWorktreeUnavailable", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("refused reuse deleted the existing checkout: %v", statErr)
	}
	if !mgr.IsValid(worktreePath) {
		t.Fatalf("refused reuse damaged the existing checkout at %q", worktreePath)
	}
}

// TestCreate_NormalReusePreservesCheckoutOfAnotherRepository verifies the
// non-attach reuse path (tryReuseExisting) also fails closed on an unrelated
// checkout and preserves it: the failure surfaces the typed recovery error
// without deleting the existing checkout.
func TestCreate_NormalReusePreservesCheckoutOfAnotherRepository(t *testing.T) {
	repoPath := initGitRepoWithRemote(t)
	otherRepoPath := initGitRepoWithRemote(t)
	worktreePath := filepath.Join(t.TempDir(), "canonical")
	runGit(t, otherRepoPath, "worktree", "add", "-b", "unrelated", worktreePath, "main")
	marker := filepath.Join(worktreePath, "unrelated-uncommitted-marker")
	if err := os.WriteFile(marker, []byte("other repository state\n"), 0o600); err != nil {
		t.Fatalf("write uncommitted marker: %v", err)
	}

	store := newMockStore()
	store.worktrees["canonical-worktree"] = &Worktree{
		ID:           "canonical-worktree",
		TaskID:       "task-1",
		SessionID:    "session-2",
		RepositoryID: "repository-1",
		Path:         worktreePath,
		Branch:       "unrelated",
		Status:       StatusActive,
	}
	mgr, err := NewManager(newTestConfig(t), store, newTestLogger())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	_, err = mgr.Create(context.Background(), CreateRequest{
		TaskID:         "task-1",
		SessionID:      "session-2",
		RepositoryID:   "repository-1",
		RepositoryPath: repoPath,
		BaseBranch:     "main",
		WorktreeID:     "canonical-worktree",
	})
	var recoveryErr *WorktreeRecoveryError
	if !errors.As(err, &recoveryErr) {
		t.Fatalf("Create() error = %v, want WorktreeRecoveryError", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("refused reuse deleted the existing checkout: %v", statErr)
	}
}

// TestCheckoutMatchesRepositoryIdentity pins the os.SameFile common-dir
// semantics used to reject an unrelated checkout.
func TestCheckoutMatchesRepositoryIdentity(t *testing.T) {
	repoPath := initGitRepoWithRemote(t)
	otherRepoPath := initGitRepoWithRemote(t)
	worktreePath := filepath.Join(t.TempDir(), "linked")
	runGit(t, repoPath, "worktree", "add", "-b", "identity", worktreePath, "main")

	if !checkoutMatchesRepositoryIdentity(worktreePath, repoPath) {
		t.Fatalf("checkoutMatchesRepositoryIdentity() = false for a worktree of the repository")
	}
	if !checkoutMatchesRepositoryIdentity(repoPath, repoPath) {
		t.Fatalf("checkoutMatchesRepositoryIdentity() = false for the repository itself")
	}
	if checkoutMatchesRepositoryIdentity(worktreePath, otherRepoPath) {
		t.Fatalf("checkoutMatchesRepositoryIdentity() = true for an unrelated repository")
	}
	if checkoutMatchesRepositoryIdentity(t.TempDir(), repoPath) {
		t.Fatalf("checkoutMatchesRepositoryIdentity() = true for a non-Git directory")
	}
}
