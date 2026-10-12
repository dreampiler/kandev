package executor

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// A task that already materialized a canonical environment has a frozen runner
// (EvaluateRunnerMutability rejects a switch after session or environment
// creation). A requested runner that merely echoes the task's stored metadata
// profile - which is what a workflow step entry, a queued auto-start rebuild, or
// the frontend echoing task metadata does - is therefore stale evidence: it must
// not override the environment's executor and force a second workspace/session
// under a different backend. A caller that deliberately selected a different
// profile is an executor transition and is preserved. These tests pin the
// resolution at both seams: session creation (canonicalEnvironmentRunner) and
// launch (bindSessionLaunchRunner).

func staleEntryTask() *v1.Task {
	return &v1.Task{
		ID:          "task-1",
		WorkspaceID: "workspace-1",
		Title:       "Entry continuity",
		Metadata:    map[string]interface{}{models.MetaKeyExecutorProfileID: "worktree-profile"},
	}
}

func TestCanonicalEnvironmentRunnerPrefersEnvironmentUnlessSelected(t *testing.T) {
	env := &models.TaskEnvironment{
		ExecutorType:      string(models.ExecutorTypeLocal),
		ExecutorID:        "local-exec",
		ExecutorProfileID: "local-profile",
	}

	t.Run("stale metadata profile echoed by the entry loses to the environment", func(t *testing.T) {
		// The workflow entry reads executor_profile_id from task metadata and
		// hands it back verbatim; the ready canonical environment must win.
		gotID, gotProfile := canonicalEnvironmentRunner(staleEntryTask(), env, "worktree-exec", "worktree-profile")
		if gotID != "local-exec" || gotProfile != "local-profile" {
			t.Fatalf("runner = (%q, %q), want the canonical environment runner", gotID, gotProfile)
		}
	})

	t.Run("environment wins when no profile was requested", func(t *testing.T) {
		gotID, gotProfile := canonicalEnvironmentRunner(staleEntryTask(), env, "worktree-exec", "")
		if gotID != "local-exec" || gotProfile != "local-profile" {
			t.Fatalf("runner = (%q, %q), want the canonical environment runner", gotID, gotProfile)
		}
	})

	t.Run("deliberately selected different profile is preserved", func(t *testing.T) {
		gotID, gotProfile := canonicalEnvironmentRunner(staleEntryTask(), env, "worktree-exec", "transition-profile")
		if gotID != "worktree-exec" || gotProfile != "transition-profile" {
			t.Fatalf("runner = (%q, %q), want the deliberately selected runner", gotID, gotProfile)
		}
	})

	t.Run("no environment keeps the caller runner", func(t *testing.T) {
		gotID, gotProfile := canonicalEnvironmentRunner(staleEntryTask(), nil, "worktree-exec", "worktree-profile")
		if gotID != "worktree-exec" || gotProfile != "worktree-profile" {
			t.Fatalf("runner = (%q, %q), want the caller runner", gotID, gotProfile)
		}
	})
}

func TestBindSessionLaunchRunnerClearsStaleMetadataProfile(t *testing.T) {
	env := &models.TaskEnvironment{
		ExecutorType:      string(models.ExecutorTypeLocal),
		ExecutorID:        "local-exec",
		ExecutorProfileID: "",
	}
	// The prepared session adopted the environment runner (local default, no
	// profile), while task metadata still carries the stale worktree profile.
	session := &models.TaskSession{ID: "sess-2", TaskID: "task-1", TaskEnvironmentID: env.ID}

	t.Run("environment runner replaces the stale profile at launch", func(t *testing.T) {
		gotID, gotMeta := bindSessionLaunchRunner(staleEntryTask(), env, session, "worktree-exec", cloneMetadata(staleEntryTask().Metadata))
		if gotID != "local-exec" {
			t.Fatalf("executor id = %q, want local-exec", gotID)
		}
		if got := gotMeta["executor_profile_id"]; got != "" {
			t.Fatalf("executor_profile_id = %v, want cleared so applyProfile cannot pick worktree", got)
		}
	})

	t.Run("deliberately selected transition keeps the selected profile", func(t *testing.T) {
		transitionSession := &models.TaskSession{ID: "sess-3", TaskID: "task-1", ExecutorProfileID: "transition-profile"}
		gotID, gotMeta := bindSessionLaunchRunner(staleEntryTask(), env, transitionSession, "worktree-exec", cloneMetadata(staleEntryTask().Metadata))
		if gotID != "worktree-exec" || gotMeta["executor_profile_id"] != "transition-profile" {
			t.Fatalf("runner = (%q, %v), want the deliberate transition preserved", gotID, gotMeta["executor_profile_id"])
		}
	})

	t.Run("no environment keeps the session profile behavior", func(t *testing.T) {
		sess := &models.TaskSession{ID: "sess-2", TaskID: "task-1", ExecutorProfileID: "session-profile"}
		gotID, gotMeta := bindSessionLaunchRunner(staleEntryTask(), nil, sess, "exec-1", nil)
		if gotID != "exec-1" || gotMeta["executor_profile_id"] != "session-profile" {
			t.Fatalf("runner = (%q, %v), want the session profile", gotID, gotMeta["executor_profile_id"])
		}
	})
}

// buildLaunchAgentRequest is the launch-time decision point: the resolved
// ExecutorType must follow the bound canonical environment, not a stale
// task-metadata profile, so resolveExecutorConfig does not authorize a second
// materialization under the wrong backend.
func TestBuildLaunchAgentRequestFollowsCanonicalEnvironmentRunner(t *testing.T) {
	repo := newMockRepository()
	repo.executors["local-exec"] = &models.Executor{ID: "local-exec", Type: models.ExecutorTypeLocal}
	repo.executors[models.ExecutorIDWorktree] = &models.Executor{ID: models.ExecutorIDWorktree, Type: models.ExecutorTypeWorktree}
	repo.executorProfiles["worktree-profile"] = &models.ExecutorProfile{ID: "worktree-profile", ExecutorID: models.ExecutorIDWorktree}
	repo.executorProfiles["transition-worktree-profile"] = &models.ExecutorProfile{ID: "transition-worktree-profile", ExecutorID: models.ExecutorIDWorktree}
	repo.taskRepositories["task-repo"] = &models.TaskRepository{ID: "task-repo", TaskID: "task-1", RepositoryID: "repo-1"}

	e := newTestExecutor(t, &mockAgentManager{}, repo)
	task := staleEntryTask()
	env := &models.TaskEnvironment{
		ID:           "env-1",
		TaskID:       "task-1",
		ExecutorType: string(models.ExecutorTypeLocal),
		ExecutorID:   "local-exec",
		Status:       models.TaskEnvironmentStatusReady,
	}
	info := &repoInfo{
		RepositoryID:   "repo-1",
		RepositoryPath: t.TempDir(),
		Repository:     &models.Repository{ID: "repo-1", Name: "widget"},
	}

	// A session whose prepared runner is the environment's local default (no
	// profile) while task metadata still names the worktree profile.
	session := &models.TaskSession{ID: "sess-2", TaskID: "task-1", TaskEnvironmentID: env.ID}

	req, _, err := e.buildLaunchAgentRequest(context.Background(), task, session, "agent-1", "", "prompt", info, []*repoInfo{info}, false, env)
	if err != nil {
		t.Fatalf("buildLaunchAgentRequest: %v", err)
	}
	if req.ExecutorType != string(models.ExecutorTypeLocal) {
		t.Fatalf("ExecutorType = %q, want local: the canonical environment must not be overridden by stale task metadata", req.ExecutorType)
	}

	// A deliberate transition selects a profile that differs from the stored
	// task metadata; the new backend must be honored.
	transitionSession := &models.TaskSession{ID: "sess-3", TaskID: "task-1", TaskEnvironmentID: env.ID, ExecutorProfileID: "transition-worktree-profile"}
	req, _, err = e.buildLaunchAgentRequest(context.Background(), task, transitionSession, "agent-1", "", "prompt", info, []*repoInfo{info}, false, env)
	if err != nil {
		t.Fatalf("buildLaunchAgentRequest (transition): %v", err)
	}
	if req.ExecutorType != string(models.ExecutorTypeWorktree) {
		t.Fatalf("ExecutorType = %q, want worktree: a deliberately selected transition must be preserved", req.ExecutorType)
	}
}

// TestRunnerRequestedDiffersFromTaskMetadata pins the deliberate-selection
// boundary directly: an echoed metadata profile (or an empty request) is not a
// selection, while a different non-empty profile is.
func TestRunnerRequestedDiffersFromTaskMetadata(t *testing.T) {
	task := staleEntryTask()
	if runnerRequestedDiffersFromTaskMetadata(task, "worktree-profile") {
		t.Fatal("echoed metadata profile must not count as a deliberate selection")
	}
	if runnerRequestedDiffersFromTaskMetadata(task, "") {
		t.Fatal("empty request must not count as a deliberate selection")
	}
	if !runnerRequestedDiffersFromTaskMetadata(task, "transition-profile") {
		t.Fatal("a different non-empty profile is a deliberate selection")
	}
	if runnerRequestedDiffersFromTaskMetadata(nil, "transition-profile") {
		t.Fatal("a nil task has no stored runner to differ from")
	}
}

// TestPrepareSessionEntryPrefersCanonicalEnvironmentRunner drives the real
// no-session entry path (PrepareSession) for a task that already materialized a
// local canonical environment while task metadata still carries a stale worktree
// profile. The created session must adopt the environment's runner, and the
// launch metadata must not retain the stale profile that applyProfile would
// otherwise turn back into a second worktree materialization.
func TestPrepareSessionEntryPrefersCanonicalEnvironmentRunner(t *testing.T) {
	repo := newMockRepository()
	repo.executors["local-exec"] = &models.Executor{ID: "local-exec", Type: models.ExecutorTypeLocal}
	repo.taskEnvironments["env-1"] = &models.TaskEnvironment{
		ID:           "env-1",
		TaskID:       "task-1",
		ExecutorType: string(models.ExecutorTypeLocal),
		ExecutorID:   "local-exec",
		Status:       models.TaskEnvironmentStatusReady,
	}
	exec := newTestExecutor(t, &mockAgentManager{}, repo)
	task := staleEntryTask()

	// The workflow entry marks the metadata-derived profile as "explicit" (it
	// came from the caller), which is exactly the signal the old guard trusted.
	ctx := WithTaskRunnerProfileExplicit(context.Background(), true)
	sessionID, err := exec.PrepareSession(ctx, task, "agent-profile", "", "worktree-profile", "")
	if err != nil {
		t.Fatalf("PrepareSession: %v", err)
	}
	created, err := repo.GetTaskSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	if created.ExecutorID != "local-exec" {
		t.Fatalf("session ExecutorID = %q, want the canonical environment's local-exec", created.ExecutorID)
	}
	if created.ExecutorProfileID != "" {
		t.Fatalf("session ExecutorProfileID = %q, want empty (local default)", created.ExecutorProfileID)
	}
	if got := created.Metadata["executor_profile_id"]; got != "" {
		t.Fatalf("session metadata executor_profile_id = %v, want cleared so the stale worktree profile cannot resurface", got)
	}
}
