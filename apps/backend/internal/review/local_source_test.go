package review

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
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

func localRunRequest(t *testing.T, path, base, head string) RunRequest {
	t.Helper()
	b, err := json.Marshal(map[string]any{"TaskID": "task-1", "worktree_path": path, "base_ref": base, "head_ref": head})
	if err != nil {
		t.Fatal(err)
	}
	var req RunRequest
	if err := json.Unmarshal(b, &req); err != nil {
		t.Fatal(err)
	}
	return req
}

func TestRunnerExplicitWorktreeCreatesRun(t *testing.T) {
	dir := localReviewRepo(t)
	h := newRunnerHarness(t, nil, []string{okResponse("a.go", 1)})
	h.changes.statusErr = errors.New("agentctl unreachable")
	run, err := h.runner.Run(context.Background(), localRunRequest(t, dir, "HEAD", ""))
	if err != nil {
		t.Fatalf("explicit worktree must create a review: %v", err)
	}
	if run.Status != models.ReviewRunCompleted || h.inference.promptCount() != 1 {
		t.Fatalf("run did not complete: %+v", run)
	}
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

func TestCollectLocalChangesRejectsInvalidRefs(t *testing.T) {
	dir := localReviewRepo(t)
	for _, source := range []Source{
		{WorktreePath: dir}, {WorktreePath: dir, BaseRef: "missing"}, {WorktreePath: dir, BaseRef: "--help"},
		{WorktreePath: dir, BaseRef: "HEAD", HeadRef: "missing"}, {WorktreePath: `\\server\share`, BaseRef: "HEAD"},
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

type localSourceTaskContext struct{ metadata map[string]any }

func (f localSourceTaskContext) ReviewPromptContext(context.Context, string, string) (PromptContext, error) {
	return PromptContext{}, nil
}
func (f localSourceTaskContext) ReviewSourceMetadata(context.Context, string, string) (map[string]any, error) {
	return f.metadata, nil
}

func TestRunnerTaskReviewSourceWhenWorkspaceEmpty(t *testing.T) {
	dir := localReviewRepo(t)
	h := newRunnerHarness(t, nil, []string{okResponse("a.go", 1)})
	h.runner.taskContext = localSourceTaskContext{map[string]any{"review_source": map[string]any{"worktree_path": dir, "base_ref": "HEAD"}}}
	run, err := h.runner.Run(context.Background(), RunRequest{TaskID: "task-1", Trigger: models.ReviewTriggerWorkflowStep})
	if err != nil {
		t.Fatalf("task source must be used for an empty workspace: %v", err)
	}
	if run.Status != models.ReviewRunCompleted {
		t.Fatalf("run = %+v", run)
	}
}

func TestRunnerExplicitWorktreeInvalidPath(t *testing.T) {
	for _, path := range []string{"relative/path", "https://example.test/repo", t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		t.Run(path, func(t *testing.T) {
			h := newRunnerHarness(t, nil, []string{"OK"})
			_, err := h.runner.Run(context.Background(), localRunRequest(t, path, "HEAD", ""))
			if !errors.Is(err, ErrWorkspaceUnavailable) {
				t.Fatalf("invalid source must return workspace error, got %v", err)
			}
			if h.store.runCount() != 0 {
				t.Fatal("invalid source created a run")
			}
		})
	}
}

func TestRunnerExplicitWorktreeHeadRangeAndWriterExclusion(t *testing.T) {
	dir := localReviewRepo(t)
	base := localReviewGit(t, dir, "rev-parse", "HEAD")
	localReviewGit(t, dir, "add", ".")
	localReviewGit(t, dir, "commit", "-m", "change")
	h := newRunnerHarness(t, nil, []string{okResponse("a.go", 1)})
	h.runner.resolver = NewResolver(fakeProfilesByID{
		"writer":   {AgentID: "claude-acp", Model: "opus"},
		"reviewer": {AgentID: "claude-acp", Model: "sonnet"},
	}, nil, nil)
	req := localRunRequest(t, dir, base, "HEAD")
	req.Origin = &OriginIdentity{ProfileID: "writer", ExecutionProfileID: "writer"}
	req.OriginRequired = true
	req.AgentProfileID = "reviewer"
	run, err := h.runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("committed explicit source: %v", err)
	}
	if run.Model != "sonnet" {
		t.Fatalf("reviewer = %q", run.Model)
	}

	req.TaskID = "same-model"
	req.AgentProfileID = "writer"
	_, err = h.runner.Run(context.Background(), req)
	if !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("writer model must remain excluded: %v", err)
	}
}
