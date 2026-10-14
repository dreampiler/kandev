package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
)

func (s *Service) validateExecutorProfileReferences(ctx context.Context, req *CreateTaskRequest) error {
	ids := []string{req.ExecutorProfileID}
	for _, metadata := range []map[string]interface{}{req.Metadata, req.DeferredLaunch} {
		if id, ok := metadata[models.MetaKeyExecutorProfileID].(string); ok {
			ids = append(ids, id)
		}
	}
	seen := make(map[string]bool)
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if s.executors == nil {
			return errors.New("executor profile lookup is unavailable")
		}
		profile, err := s.executors.GetExecutorProfile(ctx, id)
		if err != nil {
			return fmt.Errorf("load executor profile %q: %w", id, err)
		}
		if profile == nil {
			return fmt.Errorf("%w: %s", repoerrors.ErrExecutorProfileNotFound, id)
		}
	}
	return nil
}
