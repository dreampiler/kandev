package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	"go.uber.org/zap"
)

// bindSessionLaunchRunner resolves the runner for a prepared-session launch.
// The session's canonical environment is the durable runner authority for any
// launch whose requested profile merely echoes the task's stored runner
// metadata: a stale task-metadata executor_profile_id must not override it at
// launch time, or resolveExecutorConfig would apply the wrong executor via
// applyProfile and a second workspace would be materialized under a different
// backend. A caller that deliberately selected a different profile (a real
// executor transition) is preserved. The effective profile is projected onto
// the launch metadata (an empty value is meaningful: local default, no profile)
// so the metadata fallback cannot resurrect a stale value. When no environment
// is bound, the session's own stored profile keeps the prior behavior.
func bindSessionLaunchRunner(
	task *v1.Task,
	env *models.TaskEnvironment,
	session *models.TaskSession,
	executorID string,
	metadata map[string]interface{},
) (string, map[string]interface{}) {
	sessionProfileID := ""
	if session != nil {
		sessionProfileID = session.ExecutorProfileID
	}
	if env != nil && env.ExecutorType != "" && !runnerRequestedDiffersFromTaskMetadata(task, sessionProfileID) {
		if env.ExecutorID != "" {
			executorID = env.ExecutorID
		}
		return executorID, setLaunchMetadataProfile(metadata, env.ExecutorProfileID)
	}
	if session != nil {
		return executorID, setLaunchMetadataProfile(metadata, sessionProfileID)
	}
	return executorID, metadata
}

func setLaunchMetadataProfile(metadata map[string]interface{}, profileID string) map[string]interface{} {
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["executor_profile_id"] = profileID
	return metadata
}

// retryPreparedSessionAfterWorkspaceReady waits for the elected environment,
// then retries binding once. Shared groups use the refusal's environment ID;
// legacy same-task refusals use the task's own row. A self-owned, failed, or
// unready environment keeps the original refusal.
func (e *Executor) retryPreparedSessionAfterWorkspaceReady(
	ctx context.Context,
	taskID, sessionID string,
	preparingErr error,
	create func() error,
) error {
	var env *models.TaskEnvironment
	var err error
	var preparing *models.WorkspacePreparingError
	if errors.As(preparingErr, &preparing) && preparing.EnvironmentID != "" {
		env, err = e.repo.GetTaskEnvironment(ctx, preparing.EnvironmentID)
	} else {
		env, err = e.repo.GetTaskEnvironmentByTaskID(ctx, taskID)
	}
	if err != nil || env == nil || env.ID == "" {
		return preparingErr
	}
	// Waiting on the environment this session already owns would wait on
	// itself; anything not creating/ready/stopped (a failed row another launch
	// is reclaiming) keeps the original refusal.
	if env.MaterializationSessionID == sessionID {
		return preparingErr
	}
	if _, err := e.waitForTaskEnvironmentReady(ctx, env.ID); err != nil {
		e.logger.Warn("workspace was not ready after the binding election; keeping the preparing refusal",
			zap.String("session_id", sessionID),
			zap.String("task_environment_id", env.ID),
			zap.Error(err))
		return preparingErr
	}
	return create()
}

// refreshRetriedSessionOrder re-derives creation order and primary ownership
// before a binding retry. The election winner may have created the task's first
// session while this start waited, so the retry must neither claim the
// first-session origin nor take the primary flag from the winner.
func (e *Executor) refreshRetriedSessionOrder(
	ctx context.Context,
	session *models.TaskSession,
	wasPrimary bool,
	appliedInitialRuntimeConfig bool,
) (bool, error) {
	sessions, err := e.repo.ListTaskSessions(ctx, session.TaskID)
	if err != nil {
		return wasPrimary, fmt.Errorf("list task sessions before workspace retry: %w", err)
	}
	if len(sessions) > 0 {
		delete(session.Metadata, models.SessionMetaKeyOrigin)
		if appliedInitialRuntimeConfig {
			delete(session.Metadata, models.SessionMetaKeyRuntimeConfigOverrides)
		}
	}
	primary := wasPrimary
	for _, existing := range sessions {
		if existing.IsPrimary {
			primary = false
			break
		}
	}
	session.IsPrimary = primary
	return primary, nil
}
