package worktree

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestCreate_ReuseRequiredRepreparesUnrecoverableBranch verifies that an
// attach-only reuse whose recorded branch no longer exists locally or on
// origin re-prepares a fresh task copy instead of failing the launch. The
// stable worktree identity is retained and the original checkout is left in
// place.
func TestCreate_ReuseRequiredRepreparesUnrecoverableBranch(t *testing.T) {
	repoPath := initGitRepoWithRemote(t)
	runGit(t, repoPath, "push", "origin", "--delete", "feature/pr-branch")
	archiveDeletesLocalBranch(t, repoPath, "feature/pr-branch")
	runGit(t, repoPath, "fetch", "--prune", "origin")

	cfg := newTestConfig(t)
	store := newMockStore()
	mgr, err := NewManager(cfg, store, newTestLogger())
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	existing := &Worktree{
		ID:                "wt-reuse-reprepare",
		SessionID:         "session-reuse-reprepare",
		TaskID:            "task-reuse-reprepare",
		TaskDirName:       "task-reuse-reprepare",
		TaskEnvironmentID: "env-reuse-reprepare",
		RepositoryID:      "repo-1",
		RepositoryPath:    repoPath,
		Path:              filepath.Join(cfg.TasksBasePath, "task-reuse-reprepare", "repo-1-primary"),
		Branch:            "feature/pr-branch",
		BranchSlug:        "primary",
		BaseBranch:        "main",
		Status:            StatusActive,
	}
	store.worktrees[existing.ID] = existing

	wt, err := mgr.Create(context.Background(), CreateRequest{
		TaskID:             existing.TaskID,
		SessionID:          existing.SessionID,
		TaskEnvironmentID:  existing.TaskEnvironmentID,
		RepositoryID:       existing.RepositoryID,
		RepositoryPath:     repoPath,
		BaseBranch:         "main",
		TaskDirName:        existing.TaskDirName,
		RepoName:           "repo-1",
		WorktreeID:         existing.ID,
		BranchIdentitySlug: existing.BranchSlug,
		ReuseRequired:      true,
	})
	if err != nil {
		t.Fatalf("Create(ReuseRequired) error = %v, want a re-prepared worktree", err)
	}
	if wt == nil {
		t.Fatal("Create(ReuseRequired) returned a nil worktree")
	}
	if wt.ID != existing.ID {
		t.Fatalf("re-prepared worktree ID = %q, want stable ID %q", wt.ID, existing.ID)
	}
	if wt.Branch == existing.Branch {
		t.Fatalf("re-prepared branch = %q, want a fresh branch", wt.Branch)
	}
	if wt.Path == existing.Path {
		t.Fatalf("re-prepared path = %q, want a fresh path", wt.Path)
	}
	if wt.Status != StatusActive || wt.DeletedAt != nil {
		t.Fatalf("re-prepared status = %q, deleted_at = %v, want active and nil", wt.Status, wt.DeletedAt)
	}
	if got := strings.TrimSpace(runGit(t, wt.Path, "rev-parse", "--abbrev-ref", "HEAD")); got != wt.Branch {
		t.Fatalf("re-prepared HEAD branch = %q, want %q", got, wt.Branch)
	}
	stored := store.worktrees[existing.ID]
	if stored == nil || stored.Branch != wt.Branch || stored.Path != wt.Path {
		t.Fatalf("stored re-prepared worktree = %#v, want branch %q and path %q", stored, wt.Branch, wt.Path)
	}
}
