package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	"github.com/stretchr/testify/require"
)

type failingProfileStore struct {
	executorStore
	err error
}

func (r failingProfileStore) GetExecutorProfile(context.Context, string) (*models.ExecutorProfile, error) {
	return nil, r.err
}

func TestExecutorProfileReferenceAdmission(t *testing.T) {
	ctx := context.Background()
	for _, office := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "office"}[office], func(t *testing.T) {
			repo := newMockRepository()
			e := newTestExecutor(t, &mockAgentManager{}, repo)
			task := &v1.Task{ID: "profile-task", WorkspaceID: "profile-ws"}
			var err error
			if office {
				_, err = e.createOfficeSession(ctx, task, "agent", "agent", "", "missing-profile")
			} else {
				_, err = e.PrepareSession(ctx, task, "agent", "", "missing-profile", "")
			}
			require.ErrorIs(t, err, repoerrors.ErrExecutorProfileNotFound)
			require.Contains(t, err.Error(), "missing-profile")
			require.Empty(t, repo.sessions)
			if office {
				_, err = e.createOfficeSession(ctx, task, "agent", "agent", "", "")
			} else {
				_, err = e.PrepareSession(ctx, task, "agent", "", "", "")
			}
			require.NoError(t, err)
			require.Len(t, repo.sessions, 1)
		})
	}
	t.Run("retained_and_read_error", func(t *testing.T) {
		repo := newMockRepository()
		repo.executors["local"] = &models.Executor{ID: "local", Type: models.ExecutorTypeLocal}
		e := newTestExecutor(t, &mockAgentManager{}, repo)
		task := &models.Task{ID: "task", WorkspaceID: "ws"}
		session := &models.TaskSession{ID: "session", ExecutorID: "local", ExecutorProfileID: "deleted-profile"}
		cfg, err := e.resolveModelSwitchExecutorConfig(ctx, task, session, nil)
		require.NoError(t, err)
		require.Equal(t, "local", cfg.ExecutorID)
		readErr := errors.New("profile store unavailable")
		e.repo = failingProfileStore{executorStore: repo, err: readErr}
		_, err = e.resolveModelSwitchExecutorConfig(ctx, task, session, nil)
		require.ErrorIs(t, err, readErr)
	})
}
