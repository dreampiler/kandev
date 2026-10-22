package backendapp

import (
	"context"

	"github.com/kandev/kandev/internal/authz"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
	taskservice "github.com/kandev/kandev/internal/task/service"
)

func pluginChangeRequestAuthorizer(tasks *taskservice.Service) func(context.Context, string, string) error {
	return func(ctx context.Context, workspaceID, taskID string) error {
		if err := tasks.AuthorizeWorkspaceScope(ctx, workspaceID, authz.ScopeTaskWrite); err != nil {
			return err
		}
		if taskID == "" {
			return nil
		}
		identity, err := tasks.GetTaskMutationIdentity(ctx, taskID)
		if err != nil {
			return err
		}
		if identity == nil || identity.WorkspaceID != workspaceID {
			return repoerrors.ErrTaskNotFound
		}
		return nil
	}
}
