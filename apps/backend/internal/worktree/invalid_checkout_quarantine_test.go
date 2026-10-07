package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuarantineInvalidCheckout_MovesEmptyDirectoryAside(t *testing.T) {
	cfg := newTestConfig(t)
	mgr, err := NewManager(cfg, newMockStore(), newTestLogger())
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	path := filepath.Join(cfg.TasksBasePath, "empty-checkout")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	aside, err := mgr.quarantineInvalidCheckout(path)
	if err != nil {
		t.Fatalf("quarantineInvalidCheckout failed: %v", err)
	}
	if aside == "" {
		t.Fatal("expected non-empty aside path")
	}
	if !strings.Contains(aside, ".invalid-") {
		t.Fatalf("aside path %q does not contain .invalid-", aside)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("original path still exists: %v", err)
	}
	if _, err := os.Lstat(aside); err != nil {
		t.Fatalf("aside path does not exist: %v", err)
	}
}

func TestQuarantineInvalidCheckout_MovesBrokenGitPointerAside(t *testing.T) {
	cfg := newTestConfig(t)
	mgr, err := NewManager(cfg, newMockStore(), newTestLogger())
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	path := filepath.Join(cfg.TasksBasePath, "broken-pointer")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, ".git"), []byte("gitdir: /nonexistent/.git/worktrees/test\n"), 0644); err != nil {
		t.Fatalf("write .git: %v", err)
	}

	aside, err := mgr.quarantineInvalidCheckout(path)
	if err != nil {
		t.Fatalf("quarantineInvalidCheckout failed: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("original path still exists: %v", err)
	}
	if _, err := os.Lstat(aside); err != nil {
		t.Fatalf("aside path does not exist: %v", err)
	}
}

func TestQuarantineInvalidCheckout_MovesUnrelatedCheckoutAside(t *testing.T) {
	cfg := newTestConfig(t)
	mgr, err := NewManager(cfg, newMockStore(), newTestLogger())
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	repoPath := initGitRepoForWorktreeTest(t)
	otherRepoPath := initGitRepoForWorktreeTest(t)

	path := filepath.Join(cfg.TasksBasePath, "unrelated-checkout")
	runGit(t, otherRepoPath, "worktree", "add", path, "feature/pr-branch")

	slot := &RecoverySlot{
		Worktree: &Worktree{
			Path:         path,
			RepositoryID: "repo-1",
		},
		RepositoryPath: repoPath,
	}

	if !shouldQuarantinePresentCheckout(slot) {
		t.Fatal("shouldQuarantinePresentCheckout = false for unrelated checkout")
	}

	aside, err := mgr.quarantineInvalidCheckout(path)
	if err != nil {
		t.Fatalf("quarantineInvalidCheckout failed: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("original path still exists: %v", err)
	}
	if _, err := os.Lstat(aside); err != nil {
		t.Fatalf("aside path does not exist: %v", err)
	}
}

func TestShouldQuarantinePresentCheckout_ValidCheckoutNotQuarantined(t *testing.T) {
	cfg := newTestConfig(t)
	repoPath := initGitRepoForWorktreeTest(t)

	path := filepath.Join(cfg.TasksBasePath, "valid-checkout")
	runGit(t, repoPath, "worktree", "add", path, "feature/pr-branch")

	slot := &RecoverySlot{
		Worktree: &Worktree{
			Path:         path,
			RepositoryID: "repo-1",
		},
		RepositoryPath: repoPath,
	}

	if shouldQuarantinePresentCheckout(slot) {
		t.Fatal("shouldQuarantinePresentCheckout = true for valid checkout")
	}
}

func TestShouldQuarantinePresentCheckout_CloneRelocationNotQuarantined(t *testing.T) {
	cfg := newTestConfig(t)
	repoPath := initGitRepoForWorktreeTest(t)

	path := filepath.Join(cfg.TasksBasePath, "relocation-checkout")
	runGit(t, repoPath, "worktree", "add", path, "feature/pr-branch")

	slot := &RecoverySlot{
		Worktree: &Worktree{
			Path:         path,
			RepositoryID: "repo-1",
		},
		RepositoryPath: repoPath,
		CloneRelocation: &ManagedCloneRelocationProof{
			ManagedRoot: "/managed/root",
			Identity:    ManagedRepositoryIdentity{Provider: "github", Host: "github.com", Owner: "test", Name: "repo"},
		},
	}

	if shouldQuarantinePresentCheckout(slot) {
		t.Fatal("shouldQuarantinePresentCheckout = true for clone relocation slot")
	}
}

func TestQuarantineInvalidCheckout_FailsClosedOnMissingPath(t *testing.T) {
	cfg := newTestConfig(t)
	mgr, err := NewManager(cfg, newMockStore(), newTestLogger())
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	_, err = mgr.quarantineInvalidCheckout("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestShouldQuarantinePresentCheckout_DetectsInvalidCheckouts(t *testing.T) {
	cfg := newTestConfig(t)
	repoPath := initGitRepoForWorktreeTest(t)

	tests := []struct {
		name    string
		prepare func(t *testing.T, path string)
	}{
		{
			name: "empty-directory",
			prepare: func(t *testing.T, path string) {
				if err := os.MkdirAll(path, 0755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			},
		},
		{
			name: "broken-git-pointer",
			prepare: func(t *testing.T, path string) {
				if err := os.MkdirAll(path, 0755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(path, ".git"), []byte("not a git pointer\n"), 0644); err != nil {
					t.Fatalf("write .git: %v", err)
				}
			},
		},
		{
			name: "unrelated-checkout",
			prepare: func(t *testing.T, path string) {
				otherRepo := initGitRepoForWorktreeTest(t)
				runGit(t, otherRepo, "worktree", "add", path, "feature/pr-branch")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(cfg.TasksBasePath, tt.name)
			tt.prepare(t, path)
			slot := &RecoverySlot{
				Worktree:       &Worktree{Path: path, RepositoryID: "repo-1"},
				RepositoryPath: repoPath,
			}
			if !shouldQuarantinePresentCheckout(slot) {
				t.Fatalf("shouldQuarantinePresentCheckout = false for %s", tt.name)
			}
		})
	}
}

// TestAdmitRecoveryQuarantinesInvalidCheckout exercises the resume admission
// path: a present checkout that is not a valid checkout of the recorded
// repository is preserved aside and a fresh checkout is materialized at the
// recorded path, so the resume no longer fails with ErrReuseWorktreeUnavailable.
func TestAdmitRecoveryQuarantinesInvalidCheckout(t *testing.T) {
	tests := []struct {
		name    string
		corrupt func(t *testing.T, fixture *missingCheckoutFixture)
	}{
		{
			name: "empty-directory",
			corrupt: func(t *testing.T, fixture *missingCheckoutFixture) {
				t.Helper()
				replaceCheckoutWithEmptyDir(t, fixture.worktreePath)
			},
		},
		{
			name: "broken-git-pointer",
			corrupt: func(t *testing.T, fixture *missingCheckoutFixture) {
				t.Helper()
				replaceCheckoutWithEmptyDir(t, fixture.worktreePath)
				if err := os.WriteFile(filepath.Join(fixture.worktreePath, ".git"), []byte("not a git pointer\n"), 0644); err != nil {
					t.Fatalf("write .git: %v", err)
				}
			},
		},
		{
			name: "unrelated-checkout",
			corrupt: func(t *testing.T, fixture *missingCheckoutFixture) {
				t.Helper()
				if err := os.RemoveAll(fixture.worktreePath); err != nil {
					t.Fatalf("remove checkout: %v", err)
				}
				otherRepo := initGitRepoForWorktreeTest(t)
				runGit(t, otherRepo, "worktree", "add", fixture.worktreePath, "feature/pr-branch")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newMissingCheckoutFixture(t)
			tt.corrupt(t, fixture)

			admission, err := fixture.manager.AdmitRecovery(ctx, fixture.request)
			if err != nil {
				t.Fatalf("AdmitRecovery failed: %v", err)
			}
			if admission == nil {
				t.Fatal("AdmitRecovery returned no admission for an invalid present checkout")
			}
			defer func() {
				if err := admission.Release(ctx); err != nil {
					t.Errorf("release recovery admission: %v", err)
				}
			}()

			matches, err := filepath.Glob(fixture.worktreePath + ".invalid-*")
			if err != nil {
				t.Fatalf("glob quarantine: %v", err)
			}
			if len(matches) == 0 {
				t.Fatal("invalid checkout was not preserved aside")
			}
			if head := strings.TrimSpace(runGit(t, fixture.worktreePath, "rev-parse", "HEAD")); head != fixture.branchHead {
				t.Fatalf("restored HEAD = %q, want recorded branch head %q", head, fixture.branchHead)
			}
		})
	}
}

func replaceCheckoutWithEmptyDir(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("remove checkout: %v", err)
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir empty checkout: %v", err)
	}
}
