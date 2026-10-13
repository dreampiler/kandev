package executor

import (
	"context"
	"strings"

	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

type taskRunnerProfileExplicitKey struct{}

// WithTaskRunnerProfileExplicit records whether the launch caller supplied the
// task runner profile. A false value means the profile was resolved from task
// metadata and must be checked again when the session transaction takes the
// task lock.
func WithTaskRunnerProfileExplicit(ctx context.Context, explicit bool) context.Context {
	return context.WithValue(ctx, taskRunnerProfileExplicitKey{}, explicit)
}

func taskRunnerProfileExplicit(ctx context.Context, fallback bool) bool {
	if explicit, ok := ctx.Value(taskRunnerProfileExplicitKey{}).(bool); ok {
		return explicit
	}
	return fallback
}

func taskRunnerProfileID(task *v1.Task) string {
	if task == nil {
		return ""
	}
	profileID, _ := task.Metadata[models.MetaKeyExecutorProfileID].(string)
	return profileID
}

func taskRunnerResolution(task *v1.Task, executorProfileID string, explicit bool) (bool, string) {
	resolvedProfileID := taskRunnerProfileID(task)
	return !explicit && executorProfileID != "" && executorProfileID == resolvedProfileID, resolvedProfileID
}

// runnerRequestedDiffersFromTaskMetadata reports whether a requested executor
// profile is a deliberate selection that differs from the task's stored runner.
// Launch entry points that read the stored runner and hand it back (workflow
// step entry, queued auto-start rebuilds, the frontend echoing task metadata)
// arrive with the stored value and are not a selection; a caller that chose a
// different profile is preserved so an executor transition still elects a new
// backend. The profile, not the executor ID, is authoritative here because
// resolveExecutorConfig's applyProfile lets the profile override the executor
// type.
func runnerRequestedDiffersFromTaskMetadata(task *v1.Task, executorProfileID string) bool {
	if task == nil {
		return false
	}
	requested := strings.TrimSpace(executorProfileID)
	return requested != "" && requested != taskRunnerProfileID(task)
}

func (e *Executor) reloadTaskForRunnerRetry(ctx context.Context, taskID string) (*v1.Task, string, error) {
	refreshed, err := e.repo.GetTask(ctx, taskID)
	if err != nil {
		return nil, "", err
	}
	if refreshed == nil {
		return nil, "", nil
	}
	task := refreshed.ToAPI()
	return task, taskRunnerProfileID(task), nil
}
