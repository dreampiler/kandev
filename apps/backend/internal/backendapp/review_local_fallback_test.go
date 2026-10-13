package backendapp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kandev/kandev/internal/agent/handlers"
	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	"github.com/kandev/kandev/internal/agent/runtime/lifecycle"
	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/review"
	"github.com/kandev/kandev/internal/task/models"
)

type localReviewSessionRepo struct {
	session      *models.TaskSession
	executorType models.ExecutorType
}

type unavailableReviewExecution struct{ execution *lifecycle.AgentExecution }

func (e unavailableReviewExecution) GetExecutionBySessionID(string) (*lifecycle.AgentExecution, bool) {
	return e.execution, true
}
func (e unavailableReviewExecution) GetOrEnsureExecution(context.Context, string) (*lifecycle.AgentExecution, error) {
	return e.execution, nil
}

func (r localReviewSessionRepo) GetTaskSession(context.Context, string) (*models.TaskSession, error) {
	return r.session, nil
}
func (r localReviewSessionRepo) GetExecutor(context.Context, string) (*models.Executor, error) {
	return &models.Executor{Type: r.executorType}, nil
}

func (r localReviewSessionRepo) GetRepository(_ context.Context, id string) (*models.Repository, error) {
	return &models.Repository{ID: id, Name: "repo-" + id}, nil
}

func TestReviewAgentctlUnavailableUsesLocalGit(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init")
	git("config", "user.name", "Review Test")
	git("config", "user.email", "review@example.test")
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "base")
	git("tag", "review-base")
	if err := os.WriteFile(file, []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "committed.go"), []byte("committed change\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "committed.go")
	git("commit", "-m", "branch change")
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	server.Close()
	log, err := logger.NewLogger(logger.LoggingConfig{Level: "error", Format: "json", OutputPath: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	execution := &lifecycle.AgentExecution{SessionID: "s"}
	execution.SetAgentCtlClientForTesting(agentctl.NewClient(u.Hostname(), port, log))
	for _, typ := range []models.ExecutorType{models.ExecutorTypeLocal, models.ExecutorTypeWorktree, models.ExecutorTypeSSH} {
		t.Run(string(typ), func(t *testing.T) {
			r := localReviewSessionRepo{session: &models.TaskSession{ID: "s", ExecutorID: "exec", BaseBranch: "review-base", Worktrees: []*models.TaskEnvironmentRepo{{WorktreePath: dir}}}, executorType: typ}
			src := handlers.NewReviewChangeSource(unavailableReviewExecution{execution}, reviewSessionReader{repo: r})
			files, err := review.CollectChanges(context.Background(), src, "s", "")
			if typ == models.ExecutorTypeSSH {
				if !errors.Is(err, review.ErrWorkspaceUnavailable) {
					t.Fatalf("remote executor must not read host disk: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("local git must work without agentctl: %v", err)
			}
			if len(files) != 2 || files[0].Path != "committed.go" || !strings.Contains(files[1].Diff, "+new") {
				t.Fatalf("files = %+v", files)
			}
		})
	}
	t.Run("repository scopes", func(t *testing.T) {
		r := localReviewSessionRepo{session: &models.TaskSession{ID: "s", ExecutorID: "exec", Worktrees: []*models.TaskEnvironmentRepo{
			{RepositoryID: "one", WorktreePath: dir}, {RepositoryID: "two", WorktreePath: dir},
		}}, executorType: models.ExecutorTypeLocal}
		files, err := review.CollectChanges(context.Background(), handlers.NewReviewChangeSource(nil, reviewSessionReader{repo: r}), "s", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 2 || files[0].RepositoryName == files[1].RepositoryName {
			t.Fatalf("repositories must stay separate: %+v", files)
		}
	})
}
