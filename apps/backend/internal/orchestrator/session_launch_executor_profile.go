package orchestrator

import (
	"context"
	"errors"
	"fmt"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
)

func (s *Service) validateLaunchExecutorProfile(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	reader, ok := s.repo.(interface {
		GetExecutorProfile(context.Context, string) (*models.ExecutorProfile, error)
	})
	if !ok {
		return errors.New("executor profile lookup is unavailable")
	}
	profile, err := reader.GetExecutorProfile(ctx, id)
	if err != nil {
		return fmt.Errorf("load executor profile %q: %w", id, err)
	}
	if profile == nil {
		return fmt.Errorf("%w: %s", repoerrors.ErrExecutorProfileNotFound, id)
	}
	return nil
}
