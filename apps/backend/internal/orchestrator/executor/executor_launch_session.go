package executor

import (
	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
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

// retryPreparedSessionAfterWorkspaceReady and refreshRetriedSessionOrder are
// defined in executor_execute.go; #319 (79a459ec30) re-added them here with a
// divergent signature while the originals stayed, which left the package with
// duplicate method declarations. The duplicates are removed so the package
// compiles; the live callers keep using the executor_execute.go definitions.
