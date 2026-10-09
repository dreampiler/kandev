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
	previousAgentID := s.resolveExecutionProfileAgentID(ctx, previousProfile)
	newAgentID := ""
	if resolved.Profile != nil {
		newAgentID = resolved.Profile.AgentID
	}
	applyResolvedExecution(session, resolved)
	return s.updateDynamicLaunchSession(ctx, session, previousProfile, previousState, previousAgentID, newAgentID)
}

func (s *Service) updateDynamicLaunchSession(
	ctx context.Context,
	session *models.TaskSession,
	previousProfile string,
	previousState models.TaskSessionState,
	previousAgentID, newAgentID string,
) error {
	if previousProfile == "" || previousProfile == session.ExecutionProfileID {
		// Same-candidate retries retain both provider state and explicit user
		// selections.
		return s.repo.UpdateTaskSession(ctx, session)
	}
	// Provider-owned settings belong to one concrete candidate and are always
	// cleared when the owning candidate changes.
	keys := []string{
		"acp_session_id",
		models.SessionMetaKeySessionMode,
		models.SessionMetaKeyRuntimeConfig,
		models.SessionMetaKeyACPConfigBaseline,
		models.SessionMetaKeyACPModelState,
		models.SessionMetaKeyContextWindow,
	}
	// An explicit user selection (model/mode/config overrides) stays valid only
	// while the candidate change keeps the same agent; a different agent may not
	// advertise the persisted values, so the selection is cleared with the
	// provider state instead of being carried across agents.
	if !sameAgentIdentity(previousAgentID, newAgentID) {
		keys = append(keys, models.SessionMetaKeyRuntimeConfigOverrides)
	}
	if err := s.persistDynamicCandidateSettings(ctx, session, previousState, keys); err != nil {
		return err
	}
	for _, key := range keys {
		delete(session.Metadata, key)
	}
	return nil
}

// sameAgentIdentity reports whether two resolved profile agent identities name
// the same agent. An empty identity is never a match, so a failed lookup keeps
// the previous clear-everything behavior.
func sameAgentIdentity(previousAgentID, newAgentID string) bool {
	return previousAgentID != "" && previousAgentID == newAgentID
}

// resolveExecutionProfileAgentID resolves the agent identity that owns a
// concrete execution profile, so a candidate change can distinguish a same-agent
// swap (an explicit selection stays valid) from a cross-agent swap (it must be
// cleared). A missing manager or a failed lookup returns "".
func (s *Service) resolveExecutionProfileAgentID(ctx context.Context, profileID string) string {
	if profileID == "" || s.agentManager == nil {
		return ""
	}
	info, err := s.agentManager.ResolveAgentProfile(ctx, profileID)
	if err != nil || info == nil {
		return ""
	}
	return info.AgentID
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
