package review

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func localReviewGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func localReviewRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	localReviewGit(t, dir, "init")
	localReviewGit(t, dir, "config", "user.email", "review@example.test")
	localReviewGit(t, dir, "config", "user.name", "Review Test")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	localReviewGit(t, dir, "add", ".")
	localReviewGit(t, dir, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCollectLocalChangesIncludesUntrackedAndKeepsWorkingTreeContent(t *testing.T) {
	dir := localReviewRepo(t)
	name := "space [literal].go"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("untracked content\n"), 0600); err != nil {
		t.Fatal(err)
	}
	localReviewGit(t, dir, "add", "a.go")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("latest working tree\n"), 0600); err != nil {
		t.Fatal(err)
	}
	files, err := CollectLocalChanges(context.Background(), Source{WorktreePath: dir, BaseRef: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || !strings.Contains(files[0].Diff, "+latest working tree") || files[1].Path != name || !strings.Contains(files[1].Diff, "+untracked content") {
		t.Fatalf("files = %+v", files)
	}
}

func TestCollectLocalChangesIncludesStagedOnlyChange(t *testing.T) {
	dir := localReviewRepo(t)
	localReviewGit(t, dir, "add", "a.go")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	files, err := CollectLocalChanges(context.Background(), Source{WorktreePath: dir, BaseRef: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !strings.Contains(files[0].Diff, "+new") {
		t.Fatalf("files = %+v", files)
	}
}

func TestCollectLocalChangesRejectsInvalidRefs(t *testing.T) {
	dir := localReviewRepo(t)
	for _, source := range []Source{
		{WorktreePath: dir}, {WorktreePath: dir, BaseRef: "missing"}, {WorktreePath: dir, BaseRef: "--help"},
		{WorktreePath: dir, BaseRef: "HEAD", HeadRef: "missing"}, {WorktreePath: `\\server\share`, BaseRef: "HEAD"},
		{WorktreePath: dir + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(dir), BaseRef: "HEAD"},
	} {
		_, err := CollectLocalChanges(context.Background(), source)
		if !errors.Is(err, ErrWorkspaceUnavailable) {
			t.Fatalf("invalid source %+v: %v", source, err)
		}
	}
}

func TestCollectLocalChangesLinkedWorktree(t *testing.T) {
	dir := localReviewRepo(t)
	gate := filepath.Join(t.TempDir(), "gate-slot")
	localReviewGit(t, dir, "worktree", "add", "--detach", gate, "HEAD")
	if err := os.WriteFile(filepath.Join(gate, "a.go"), []byte("gate change\n"), 0600); err != nil {
		t.Fatal(err)
	}
	files, err := CollectLocalChanges(context.Background(), Source{WorktreePath: gate, BaseRef: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !strings.Contains(files[0].Diff, "+gate change") {
		t.Fatalf("files = %+v", files)
	}
}
