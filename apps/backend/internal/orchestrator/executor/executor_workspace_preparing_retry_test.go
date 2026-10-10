package executor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// workspacePreparingRepo implements the transactional workspace-binding creator
// the production repository provides, so a test can drive PrepareSession through
// the ErrWorkspacePreparing path. Its onBinding hook lets the first binding be
// refused as a sibling materialization and later calls succeed.
type workspacePreparingRepo struct {
	*mockRepository
	onBinding func(call int) error
	calls     int
}

func (r *workspacePreparingRepo) CreateTaskSessionWithWorkspaceBinding(
	ctx context.Context, session *models.TaskSession, candidate *models.TaskEnvironment,
) error {
	r.calls++
	if r.onBinding != nil {
		if err := r.onBinding(r.calls); err != nil {
			return err
		}
	}
	return r.CreateTaskSession(ctx, session)
}

// A launch refused because a sibling session is still materializing the
// workspace must wait for the environment to become ready and retry the binding
// once, instead of failing the run.
func TestPrepareSessionRetriesAfterWorkspacePreparing(t *testing.T) {
	repo := &workspacePreparingRepo{mockRepository: newMockRepository()}
	env := &models.TaskEnvironment{
		ID:                       "env-retry",
		TaskID:                   "task-retry",
		Status:                   models.TaskEnvironmentStatusCreating,
		MaterializationSessionID: "sibling-session",
	}
	repo.taskEnvironments[env.ID] = env
	lookups := 0
	repo.getTaskEnvironmentFunc = func(_ context.Context, id string) (*models.TaskEnvironment, error) {
		require.Equal(t, env.ID, id)
		lookups++
		if lookups > 1 {
			env.Status = models.TaskEnvironmentStatusReady
		}
		return env, nil
	}
	repo.onBinding = func(call int) error {
		if call == 1 {
			return models.ErrWorkspacePreparing
		}
		return nil
	}

	exec := newTestExecutor(t, &mockAgentManager{}, repo)
	task := &v1.Task{ID: "task-retry", WorkspaceID: "ws-retry", Title: "t", Description: "d"}

	sessionID, err := exec.PrepareSession(context.Background(), task, "profile-1", "executor-1", "", "")
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)
	require.Equal(t, 2, repo.calls, "the binding must be retried once after the workspace becomes ready")
	require.GreaterOrEqual(t, lookups, 2, "the retry must wait for the environment to leave creating")
	require.NotNil(t, repo.sessions[sessionID])
}

// A workspace that reaches failed must keep the original refusal so the run
// still fails as it did before.
func TestPrepareSessionKeepsWorkspacePreparingWhenEnvironmentFails(t *testing.T) {
	repo := &workspacePreparingRepo{mockRepository: newMockRepository()}
	env := &models.TaskEnvironment{
		ID:                       "env-failed",
		TaskID:                   "task-failed",
		Status:                   models.TaskEnvironmentStatusCreating,
		MaterializationSessionID: "sibling-session",
	}
	repo.taskEnvironments[env.ID] = env
	lookups := 0
	repo.getTaskEnvironmentFunc = func(_ context.Context, id string) (*models.TaskEnvironment, error) {
		require.Equal(t, env.ID, id)
		lookups++
		if lookups > 1 {
			env.Status = models.TaskEnvironmentStatusFailed
		}
		return env, nil
	}
	repo.onBinding = func(int) error { return models.ErrWorkspacePreparing }

	exec := newTestExecutor(t, &mockAgentManager{}, repo)
	task := &v1.Task{ID: "task-failed", WorkspaceID: "ws-failed", Title: "t", Description: "d"}

	_, err := exec.PrepareSession(context.Background(), task, "profile-1", "executor-1", "", "")
	require.ErrorIs(t, err, models.ErrWorkspacePreparing)
	require.Equal(t, 1, repo.calls, "a failed workspace must not be retried")
}

// Any other binding error must keep its existing single-attempt behavior.
func TestPrepareSessionDoesNotRetryOtherBindingErrors(t *testing.T) {
	repo := &workspacePreparingRepo{mockRepository: newMockRepository()}
	repo.taskEnvironments["env-unsafe"] = &models.TaskEnvironment{
		ID: "env-unsafe", TaskID: "task-unsafe", Status: models.TaskEnvironmentStatusCreating,
		MaterializationSessionID: "sibling-session",
	}
	repo.onBinding = func(int) error { return models.ErrWorkspaceReuseUnsafe }

	exec := newTestExecutor(t, &mockAgentManager{}, repo)
	task := &v1.Task{ID: "task-unsafe", WorkspaceID: "ws-unsafe", Title: "t", Description: "d"}

	_, err := exec.PrepareSession(context.Background(), task, "profile-1", "executor-1", "", "")
	require.ErrorIs(t, err, models.ErrWorkspaceReuseUnsafe)
	require.Equal(t, 1, repo.calls, "a non-preparing binding error must not be retried")
}

// Waiting on the environment this session already owns would wait on itself.
func TestRetryPreparedSessionAfterWorkspaceReadySkipsSelfOwnedEnvironment(t *testing.T) {
	repo := newMockRepository()
	repo.taskEnvironments["env-self"] = &models.TaskEnvironment{
		ID: "env-self", TaskID: "task-self", Status: models.TaskEnvironmentStatusCreating,
		MaterializationSessionID: "session-self",
	}
	exec := newTestExecutor(t, &mockAgentManager{}, repo)
	created := false
	err := exec.retryPreparedSessionAfterWorkspaceReady(
		context.Background(), "task-self", "session-self", models.ErrWorkspacePreparing,
		func() error { created = true; return nil },
	)
	require.ErrorIs(t, err, models.ErrWorkspacePreparing)
	require.False(t, created, "an environment this session owns must not be awaited")
}

// A workspace that never becomes ready within the budget keeps the original
// refusal rather than blocking forever.
func TestRetryPreparedSessionAfterWorkspaceReadyTimesOut(t *testing.T) {
	repo := newMockRepository()
	repo.taskEnvironments["env-stuck"] = &models.TaskEnvironment{
		ID: "env-stuck", TaskID: "task-stuck", Status: models.TaskEnvironmentStatusCreating,
		MaterializationSessionID: "sibling-session",
	}
	exec := newTestExecutor(t, &mockAgentManager{}, repo)
	created := false
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := exec.retryPreparedSessionAfterWorkspaceReady(
		ctx, "task-stuck", "session-stuck", models.ErrWorkspacePreparing,
		func() error { created = true; return nil },
	)
	require.ErrorIs(t, err, models.ErrWorkspacePreparing)
	require.False(t, created, "a workspace that never becomes ready must not be retried")
}
