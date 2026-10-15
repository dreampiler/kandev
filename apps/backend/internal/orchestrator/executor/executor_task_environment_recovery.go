package executor

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	mcpprofile "github.com/kandev/kandev/internal/mcp/profile"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/worktree"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	"go.uber.org/zap"
)

// FailOwnedCreatingTaskEnvironment delegates to the owner-fenced materialization release.
func (e *Executor) FailOwnedCreatingTaskEnvironment(ctx context.Context, taskID, sessionID, reason string) {
	e.ReleaseTaskEnvironmentMaterialization(ctx, taskID, sessionID)
}

// existingEnvironmentRunner returns the task's canonical environment when its
// runner must take precedence over task metadata, or nil. A task that has
// already materialized a canonical environment has a frozen runner
// (EvaluateRunnerMutability rejects a runner switch once any session or
// environment exists). A surviving task-metadata executor_profile_id is then
// stale evidence, not a selection.
func (e *Executor) existingEnvironmentRunner(ctx context.Context, task *v1.Task) *models.TaskEnvironment {
	if e.repo == nil || task == nil {
		return nil
	}
	env, err := e.repo.GetTaskEnvironmentByTaskID(ctx, task.ID)
	if err != nil || env == nil || env.ExecutorType == "" {
		return nil
	}
	return env
}

// canonicalEnvironmentRunner overrides a requested runner with the canonical
// environment's runner unless the caller deliberately selected a different
// profile (a real executor transition). Once an environment exists the runner is
// frozen (EvaluateRunnerMutability rejects a switch after session or environment
// creation), so a requested profile that merely echoes the task's stored
// metadata is stale evidence and must not force a second workspace under a
// different backend. The environment is the durable runner authority.
func canonicalEnvironmentRunner(
	task *v1.Task,
	env *models.TaskEnvironment,
	executorID, executorProfileID string,
) (string, string) {
	if env == nil || env.ExecutorType == "" {
		return executorID, executorProfileID
	}
	if runnerRequestedDiffersFromTaskMetadata(task, executorProfileID) {
		return executorID, executorProfileID
	}
	if env.ExecutorID != "" {
		executorID = env.ExecutorID
	}
	return executorID, env.ExecutorProfileID
}

// admitEnvironmentRecovery retires a task's canonical environment when it is a
// failed, consumer-free row, so the affected session can be created/bound to a
// fresh canonical environment instead of refusing. It is a no-op when the task
// has no active environment or the active environment is not failed. A failed
// environment with a live consumer is left in place, and the caller's normal
// attach-only refusal then applies (fail-closed).
//
// It is best-effort: a lookup or storage failure is logged and swallowed so it
// cannot preempt the normal launch path's sanitized error handling, and the
// unusable row still blocks reuse until it is retired.
func (e *Executor) admitEnvironmentRecovery(ctx context.Context, taskID string) {
	if e.repo == nil || taskID == "" {
		return
	}
	env, err := e.repo.GetTaskEnvironmentByTaskID(ctx, taskID)
	if err != nil || env == nil || env.Status != models.TaskEnvironmentStatusFailed {
		return
	}
	if _, err := e.repo.RetireUnusableTaskEnvironment(ctx, env.ID); err != nil {
		e.logger.Warn("failed to retire unusable task environment",
			zap.String("task_id", taskID), zap.String("environment_id", env.ID), zap.Error(err))
	}
}

// resolveFreshTaskDirName returns the per-task directory name to use when a
// session is about to (re)materialize a worktree environment. It reuses a name
// already recorded on the environment; otherwise it prefers the deterministic
// semantic name. When that root is already occupied by a preserved (retained)
// environment of the same task, it selects a distinct fresh root tied to the
// requesting session so the preserved checkout is never overwritten. Every
// candidate, including the session-suffixed one, is checked against the
// recorded names, so a second replacement of the same session cannot reselect
// an already-used root. A lookup error fails closed to a fully fresh root
// rather than risk clobbering a preserved checkout.
func (e *Executor) resolveFreshTaskDirName(ctx context.Context, task *v1.Task, session *models.TaskSession, existingEnv *models.TaskEnvironment) string {
	if existingEnv != nil && existingEnv.TaskDirName != "" {
		return existingEnv.TaskDirName
	}
	if task == nil {
		return ""
	}
	base := worktree.SemanticWorktreeName(task.Title, worktree.TaskDirSuffix(task.ID))
	if base == "" {
		return ""
	}
	names, err := e.listTaskDirNames(ctx, task.ID)
	if err != nil {
		// Cannot prove the base or a previously used session suffix is free;
		// fall back to a fully fresh root instead of risking a clobber.
		return base + "_" + uuid.New().String()[:8]
	}
	if !taskDirNameInList(names, base) {
		return base
	}
	suffix := ""
	if session != nil {
		suffix = worktree.TaskDirSuffix(session.ID)
	}
	if suffix == "" {
		suffix = uuid.New().String()[:8]
	}
	candidate := base + "_" + suffix
	for attempt := 2; taskDirNameInList(names, candidate); attempt++ {
		candidate = fmt.Sprintf("%s_%s_%d", base, suffix, attempt)
	}
	return candidate
}

// listTaskDirNames returns the per-task directory names recorded for a task
// across active and retained environments. A nil repository yields no names.
func (e *Executor) listTaskDirNames(ctx context.Context, taskID string) ([]string, error) {
	if e.repo == nil || taskID == "" {
		return nil, nil
	}
	return e.repo.ListTaskEnvironmentTaskDirNames(ctx, taskID)
}

func taskDirNameInList(names []string, name string) bool {
	for _, existing := range names {
		if existing == name {
			return true
		}
	}
	return false
}

// officeCrossWorkspaceCapabilities returns the Office cross-workspace MCP
// capabilities for a task session: the read and message tools for an Office
// task whose workspace turned the setting on, and the handoff report tool for
// a Kanban task whose workspace such an Office workspace reaches.
func (e *Executor) officeCrossWorkspaceCapabilities(
	ctx context.Context, surface mcpprofile.Surface, workspaceID string,
) []mcpprofile.Capability {
	if e == nil || e.officeCrossWorkspace == nil || workspaceID == "" {
		return nil
	}
	switch surface {
	case mcpprofile.SurfaceOfficeTask:
		if e.officeCrossWorkspace.Enabled(ctx, workspaceID) {
			return []mcpprofile.Capability{mcpprofile.CapabilityOfficeCrossWorkspace}
		}
	case mcpprofile.SurfaceKanbanTask:
		if e.officeCrossWorkspace.ReceivesHandoffReports(ctx, workspaceID) {
			return []mcpprofile.Capability{mcpprofile.CapabilityOfficeHandoffReport}
		}
	}
	return nil
}
