package orchestrator

import (
	"context"
	"fmt"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	"github.com/kandev/kandev/internal/task/models"
)

type dynamicSessionSettingsUpdater interface {
	UpdateTaskSessionIfCurrentStateRemovingMetadataKeys(context.Context, *models.TaskSession, models.TaskSessionState, []string) (bool, error)
}

func (s *Service) persistResolvedExecution(ctx context.Context, session *models.TaskSession, resolved agentruntime.ProfileExecution) error {
	previousProfile, previousState := session.ExecutionProfileID, session.State
	applyResolvedExecution(session, resolved)
	return s.updateDynamicLaunchSession(ctx, session, previousProfile, previousState)
}

func (s *Service) updateDynamicLaunchSession(ctx context.Context, session *models.TaskSession, previousProfile string, previousState models.TaskSessionState) error {
	if previousProfile == "" || previousProfile == session.ExecutionProfileID {
		return s.repo.UpdateTaskSession(ctx, session)
	}
	// Provider settings belong to one concrete candidate. Same-candidate retries
	// retain both provider state and explicit user selections.
	keys := []string{
		"acp_session_id",
		models.SessionMetaKeySessionMode,
		models.SessionMetaKeyRuntimeConfig,
		models.SessionMetaKeyRuntimeConfigOverrides,
		models.SessionMetaKeyACPConfigBaseline,
		models.SessionMetaKeyACPModelState,
		models.SessionMetaKeyContextWindow,
	}
	if err := s.persistDynamicCandidateSettings(ctx, session, previousState, keys); err != nil {
		return err
	}
	for _, key := range keys {
		delete(session.Metadata, key)
	}
	return nil
}

func (s *Service) persistDynamicCandidateSettings(ctx context.Context, session *models.TaskSession, previousState models.TaskSessionState, keys []string) error {
	updater, ok := s.repo.(dynamicSessionSettingsUpdater)
	if !ok {
		// Legacy repository adapters expose metadata writes separately.
		for _, key := range keys {
			if err := s.repo.SetSessionMetadataKey(ctx, session.ID, key, nil); err != nil {
				return err
			}
		}
		return s.repo.UpdateTaskSession(ctx, session)
	}
	changed, err := updater.UpdateTaskSessionIfCurrentStateRemovingMetadataKeys(ctx, session, previousState, keys)
	if err != nil {
		return err
	}
	if !changed {
		return fmt.Errorf("dynamic candidate settings changed before launch for session %s", session.ID)
	}
	return nil
}
