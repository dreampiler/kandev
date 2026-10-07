package service

import (
	"context"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	"go.uber.org/zap"
)

// defaultMaterializationTimeout is how long a task environment may stay in the
// creating state with a claimed materialization owner before the reconciliation
// sweep treats the owner as gone. A launch that neither reaches READY nor FAILED
// (route resource waits, abandoned retries, shared-group rebinds) leaves the
// environment stuck: nothing else in any request path finalizes it, so later
// session launches fail with workspace-preparing forever.
const defaultMaterializationTimeout = 10 * time.Minute

// materializationTimeout returns the configured creating-environment timeout,
// falling back to the default when the setter was never called.
func (s *Service) materializationTimeout() time.Duration {
	if s.creatingEnvironmentTimeout > 0 {
		return s.creatingEnvironmentTimeout
	}
	return defaultMaterializationTimeout
}

// staleCreatingEnvironmentRepository is the narrow capability the creating
// environment sweep needs off its task-environment repository. Kept optional —
// type-asserted off s.taskEnvironments — so lightweight test doubles that do
// not exercise this sweep keep their compile compatibility.
type staleCreatingEnvironmentRepository interface {
	ListStaleCreatingTaskEnvironments(ctx context.Context, createdBefore time.Time) ([]*models.TaskEnvironment, error)
	FailStaleCreatingTaskEnvironment(ctx context.Context, environmentID, materializationSessionID string, createdBefore time.Time) (bool, error)
}

// runCreatingEnvironmentReconciliation is the session reconciliation sweep's
// creating-environment pass: an environment that has owned a materialization
// session for longer than the materialization timeout without reaching READY
// returns to FAILED so the next launch provisions a fresh workspace.
//
// An owner is left alone while its launch is still live in this process. The
// repository's guarded write additionally refuses to reclaim an environment
// whose owner session is STARTING/RUNNING with a durable executors_running row,
// covering a genuinely running turn this process did not register. Environments
// with no live launch and no such owner — waiting, terminal, deleted, or never
// started — are reclaimed once past the timeout.
func (s *Service) runCreatingEnvironmentReconciliation(ctx context.Context, now time.Time) {
	if s.taskEnvironments == nil {
		return
	}
	repo, ok := s.taskEnvironments.(staleCreatingEnvironmentRepository)
	if !ok {
		return
	}
	createdBefore := now.UTC().Add(-s.materializationTimeout())
	environments, err := repo.ListStaleCreatingTaskEnvironments(ctx, createdBefore)
	if err != nil {
		s.logger.Error("creating-environment reconciliation: failed to list candidates", zap.Error(err))
		return
	}
	for _, env := range environments {
		if env == nil || env.ID == "" || env.MaterializationSessionID == "" {
			continue
		}
		if s.creatingEnvironmentLaunchLive(env.MaterializationSessionID) {
			continue
		}
		changed, err := repo.FailStaleCreatingTaskEnvironment(ctx, env.ID, env.MaterializationSessionID, createdBefore)
		if err != nil {
			s.logger.Warn("creating-environment reconciliation: failed to fail stale environment",
				zap.String("task_id", env.TaskID),
				zap.String("env_id", env.ID),
				zap.Error(err))
			continue
		}
		if !changed {
			// Raced to ready/failed, or its owner became active between the
			// read and the guarded write; its new owner is responsible now.
			continue
		}
		s.logger.Warn("creating-environment reconciliation: failed stale workspace materialization",
			zap.String("task_id", env.TaskID),
			zap.String("env_id", env.ID),
			zap.String("materialization_session_id", env.MaterializationSessionID))
	}
}

// creatingEnvironmentLaunchLive reports whether the owner session still has a
// live in-memory execution registered by the agent runtime's execution store.
// It is the sweep's only in-process liveness signal; the durable
// STARTING/RUNNING plus executors_running owner guard lives in the repository's
// conditional write. A nil checker cannot prove a launch is dead, so the
// pass defers entirely to the repository guard rather than guessing.
func (s *Service) creatingEnvironmentLaunchLive(sessionID string) bool {
	if s.executionLivenessChecker == nil {
		return false
	}
	return s.executionLivenessChecker.HasLiveExecution(sessionID)
}
