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

// dynamicCandidateProviderStateKeys are the provider-owned session metadata keys
// that belong to one concrete route candidate and must be cleared whenever the
// owning candidate changes. runtime_config_overrides is handled separately: an
// explicit user selection survives a same-agent candidate swap.
var dynamicCandidateProviderStateKeys = []string{
	"acp_session_id",
	models.SessionMetaKeySessionMode,
	models.SessionMetaKeyRuntimeConfig,
	models.SessionMetaKeyACPConfigBaseline,
	models.SessionMetaKeyACPModelState,
	models.SessionMetaKeyContextWindow,
}

// DynamicCandidateProviderStateKeys returns the provider-owned metadata keys to
// clear when a session moves to a different concrete candidate. It is the single
// source of the profile-isolation invariant, shared by the conductor launch
// path (updateDynamicLaunchSession) and the manual route-action path
// (persistDynamicRouteSelection), so both clear the previous candidate's model,
// mode, ACP identity, and config snapshot the same way.
//
// runtime_config_overrides is cleared only when the agent identity changes: a
// same-agent candidate swap keeps an explicit user selection, but a different
// agent may not advertise the persisted values.
func DynamicCandidateProviderStateKeys(previousAgentID, newAgentID string) []string {
	keys := make([]string, len(dynamicCandidateProviderStateKeys), len(dynamicCandidateProviderStateKeys)+1)
	copy(keys, dynamicCandidateProviderStateKeys)
	if !sameAgentIdentity(previousAgentID, newAgentID) {
		keys = append(keys, models.SessionMetaKeyRuntimeConfigOverrides)
	}
	return keys
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
	keys := DynamicCandidateProviderStateKeys(previousAgentID, newAgentID)
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
