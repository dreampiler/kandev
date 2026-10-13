package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
)

// localChangedRepo is a git repo with one modified tracked file, so a review
// against HEAD finds exactly that file.
func localChangedRepo(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	localReviewGit(t, dir, "init")
	localReviewGit(t, dir, "config", "user.email", "review@example.test")
	localReviewGit(t, dir, "config", "user.name", "Review Test")
	if err := os.WriteFile(filepath.Join(dir, name), []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	localReviewGit(t, dir, "add", ".")
	localReviewGit(t, dir, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, name), []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// localCleanRepo is a git repo with a single commit and no working-tree changes,
// so a review against HEAD finds nothing.
func localCleanRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	localReviewGit(t, dir, "init")
	localReviewGit(t, dir, "config", "user.email", "review@example.test")
	localReviewGit(t, dir, "config", "user.name", "Review Test")
	if err := os.WriteFile(filepath.Join(dir, "clean.go"), []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	localReviewGit(t, dir, "add", ".")
	localReviewGit(t, dir, "commit", "-m", "base")
	return dir
}

// A saved review_source is the operator's deliberate scope, so it must be read
// instead of the session workspace, whose untracked files would otherwise
// replace the intended range and review the wrong change set.
func TestRunnerSavedReviewSourceWinsOverWorkspaceChanges(t *testing.T) {
	saved := localChangedRepo(t, "saved.go")
	h := newRunnerHarness(t,
		map[string]any{"workspace.go": fileEntry("workspace.go", "@@ -1 +1,2 @@\n x\n+y\n", "", "")},
		[]string{okResponse("saved.go", 1)},
	)
	h.runner.taskContext = localSourceTaskContext{map[string]any{
		"review_source": map[string]any{"worktree_path": saved, "base_ref": "HEAD"},
	}}

	run, err := h.runner.Run(context.Background(), RunRequest{TaskID: "task-1", Trigger: models.ReviewTriggerWorkflowStep})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run.Status != models.ReviewRunCompleted {
		t.Fatalf("run = %+v", run)
	}
	if h.inference.promptCount() == 0 {
		t.Fatal("expected the saved source to be reviewed")
	}
	prompt := h.inference.prompts[0]
	if !strings.Contains(prompt, "saved.go") || strings.Contains(prompt, "workspace.go") {
		t.Fatalf("saved review_source must win over the workspace, prompt=%q", prompt)
	}
}

// An explicit request range is the caller's own scope and outranks the saved
// review_source.
func TestRunnerExplicitRangeWinsOverSavedReviewSource(t *testing.T) {
	explicit := localChangedRepo(t, "explicit.go")
	saved := localChangedRepo(t, "saved.go")
	h := newRunnerHarness(t, nil, []string{okResponse("explicit.go", 1)})
	h.runner.taskContext = localSourceTaskContext{map[string]any{
		"review_source": map[string]any{"worktree_path": saved, "base_ref": "HEAD"},
	}}

	run, err := h.runner.Run(context.Background(), localRunRequest(t, explicit, "HEAD", ""))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run.Status != models.ReviewRunCompleted {
		t.Fatalf("run = %+v", run)
	}
	prompt := h.inference.prompts[0]
	if !strings.Contains(prompt, "explicit.go") || strings.Contains(prompt, "saved.go") {
		t.Fatalf("the explicit request range must win over the saved source, prompt=%q", prompt)
	}
}

// With no saved review_source, the session workspace remains the scope.
func TestRunnerNoSavedReviewSourceUsesWorkspace(t *testing.T) {
	h := newRunnerHarness(t,
		map[string]any{"workspace.go": fileEntry("workspace.go", "@@ -1 +1,2 @@\n x\n+y\n", "", "")},
		[]string{okResponse("workspace.go", 1)},
	)

	run, err := h.runner.Run(context.Background(), RunRequest{TaskID: "task-1"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run.Status != models.ReviewRunCompleted {
		t.Fatalf("run = %+v", run)
	}
	if h.inference.promptCount() == 0 {
		t.Fatal("expected the workspace set to be reviewed")
	}
	if prompt := h.inference.prompts[0]; !strings.Contains(prompt, "workspace.go") {
		t.Fatalf("the workspace set must be reviewed without a saved source, prompt=%q", prompt)
	}
}

// A saved review_source that resolves to no files must end the request with the
// existing no-changes reason instead of silently falling through to the
// workspace and reviewing unrelated files.
func TestRunnerSavedReviewSourceEmptyRangeDoesNotFallThrough(t *testing.T) {
	clean := localCleanRepo(t)
	h := newRunnerHarness(t,
		map[string]any{"workspace.go": fileEntry("workspace.go", "@@ -1 +1,2 @@\n x\n+y\n", "", "")},
		[]string{okResponse("workspace.go", 1)},
	)
	h.runner.taskContext = localSourceTaskContext{map[string]any{
		"review_source": map[string]any{"worktree_path": clean, "base_ref": "HEAD"},
	}}

	_, err := h.runner.Run(context.Background(), RunRequest{TaskID: "task-1"})
	if !errors.Is(err, ErrNoChanges) {
		t.Fatalf("an empty saved range must be ErrNoChanges, got %v", err)
	}
	if h.store.runCount() != 0 {
		t.Fatalf("an empty saved range must not create a run, got %d", h.store.runCount())
	}
	if h.inference.promptCount() != 0 {
		t.Fatal("an empty saved range must not fall through to the workspace and call the reviewer")
	}
}

// The explicit scope label must name the fields actually set: a base/head pair
// reads as a range, a worktree-only request reads as the path rather than an
// empty "base..head".
func TestExplicitSourceLabelUsesFieldsActuallySet(t *testing.T) {
	if got := explicitSourceLabel(RunRequest{BaseRef: "main", HeadRef: "abc123"}); got != "explicit range main..abc123" {
		t.Fatalf("range label = %q", got)
	}
	if got := explicitSourceLabel(RunRequest{WorktreePath: `C:\worktrees\slot`}); got != `explicit worktree C:\worktrees\slot` {
		t.Fatalf("worktree label = %q", got)
	}
}
