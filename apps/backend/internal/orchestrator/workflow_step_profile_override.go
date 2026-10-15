package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/task/models"
)

// ErrMaterializedRouteProfileChange refuses a start that would persist a
// different logical agent profile on a session whose durable dynamic route is
// already materialized for another logical family. Selecting a different
// dynamic profile is an explicit workflow profile switch / session handoff
// (docs/specs/agents/system-design/dynamic-agent-routing-01.md), not an
// in-place relabel: persisting the new label first would leave the session
// label and the authoritative route disagreeing and silently resume the
// previous family's concrete candidate.
var ErrMaterializedRouteProfileChange = errors.New(
	"agent profile change is not supported on a session with a materialized dynamic route; start a new session instead")

// refuseMaterializedRouteProfileChange rejects a start that would persist a
// changed logical agent profile while the session already has a materialized
// durable dynamic route under a different logical family. It runs before any
// partial session write so the existing session and its authoritative route
// stay intact. A session with no materialized route keeps its legitimate
// first-profile selection, and a route already matching the requested profile
// resumes normally.
func (s *Service) refuseMaterializedRouteProfileChange(
	ctx context.Context,
	session *models.TaskSession,
	effectiveProfileID string,
) error {
	if effectiveProfileID == "" || effectiveProfileID == session.AgentProfileID {
		return nil
	}
	loader, ok := s.repo.(dynamicRouteStateLoader)
	if !ok {
		return nil
	}
	state, err := loader.LoadRouteState(ctx, session.ID)
	if err != nil {
		return fmt.Errorf("load durable dynamic route state: %w", err)
	}
	if state == nil || state.Generation <= 0 || state.LogicalProfileID == "" ||
		state.LogicalProfileID == effectiveProfileID {
		return nil
	}
	s.logger.Warn("refusing agent profile change on a session with a materialized dynamic route",
		zap.String("session_id", session.ID),
		zap.String("current_profile", session.AgentProfileID),
		zap.String("requested_profile", effectiveProfileID),
		zap.String("route_logical_profile", state.LogicalProfileID),
		zap.Int64("route_generation", state.Generation))
	return fmt.Errorf("%w: session %s route %s requested %s",
		ErrMaterializedRouteProfileChange, session.ID, state.LogicalProfileID, effectiveProfileID)
}

// workflowStepProfileOverrideMetadataKeys are the provider-owned session
// metadata keys that belong to one concrete agent profile. They are cleared
// when a workflow step selects a different profile, so the incoming profile
// cannot inherit the previous profile's model, mode, or runtime selection
// (mirrors executor.rebindOfficeSessionExecutionProfile).
func workflowStepProfileOverrideMetadataKeys() []string {
	return []string{
		"acp_session_id",
		models.SessionMetaKeySessionMode,
		models.SessionMetaKeyRuntimeConfig,
		models.SessionMetaKeyRuntimeConfigOverrides,
		models.SessionMetaKeyACPConfigBaseline,
		models.SessionMetaKeyACPModelState,
		models.SessionMetaKeyContextWindow,
		models.SessionMetaKeyLastAgentError,
	}
}

// applyWorkflowStepProfileOverride persists a workflow-step-selected agent
// profile on the session when it differs from the stored profile. The previous
// profile's provider-owned metadata, including explicit runtime overrides, is
// cleared in the same guarded write so the new profile starts clean. It reports
// whether the profile changed.
func (s *Service) applyWorkflowStepProfileOverride(
	ctx context.Context,
	session *models.TaskSession,
	effectiveProfileID string,
) (bool, error) {
	if effectiveProfileID == "" || effectiveProfileID == session.AgentProfileID {
		return false, nil
	}
	observedState := session.State
	s.logger.Info("updating session agent profile for workflow step override",
		zap.String("session_id", session.ID),
		zap.String("old_profile", session.AgentProfileID),
		zap.String("new_profile", effectiveProfileID))
	session.AgentProfileID = effectiveProfileID
	// Re-resolve the agent profile snapshot so the tab shows the correct agent logo/name.
	// Set a minimal snapshot first so stale data is never persisted if resolution fails.
	session.AgentProfileSnapshot = map[string]interface{}{"id": effectiveProfileID}
	if profileInfo, err := s.agentManager.ResolveAgentProfile(ctx, effectiveProfileID); err != nil {
		s.logger.Warn("failed to resolve agent profile snapshot for workflow step override",
			zap.String("session_id", session.ID),
			zap.String("profile_id", effectiveProfileID),
			zap.Error(err))
	} else if profileInfo != nil {
		session.AgentProfileSnapshot = map[string]interface{}{
			"id":             profileInfo.ProfileID,
			"name":           profileInfo.ProfileName,
			"agent_id":       profileInfo.AgentID,
			"agent_name":     profileInfo.AgentName,
			"model":          profileInfo.Model,
			"mode":           profileInfo.Mode,
			"config_options": maps.Clone(profileInfo.ConfigOptions),
		}
	}
	if err := s.persistWorkflowStepProfileOverride(ctx, session, observedState); err != nil {
		return false, err
	}
	return true, nil
}

// persistWorkflowStepProfileOverride writes the new agent profile and removes
// the previous profile's provider-owned metadata keys in one guarded write. The
// in-memory metadata clone is stripped too, so the legacy repository fallback
// inside persistDynamicCandidateSettings does not re-persist the stale keys.
func (s *Service) persistWorkflowStepProfileOverride(
	ctx context.Context,
	session *models.TaskSession,
	expectedState models.TaskSessionState,
) error {
	keys := workflowStepProfileOverrideMetadataKeys()
	session.Metadata = maps.Clone(session.Metadata)
	for _, key := range keys {
		delete(session.Metadata, key)
	}
	if err := s.persistDynamicCandidateSettings(ctx, session, expectedState, keys); err != nil {
		return fmt.Errorf("persist workflow session profile: %w", err)
	}
	return nil
}
