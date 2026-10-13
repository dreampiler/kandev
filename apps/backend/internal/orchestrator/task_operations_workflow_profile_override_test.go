package orchestrator

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	"github.com/stretchr/testify/require"
)

// seedWorkflowProfileOverrideSession creates a waiting session that carries a
// creator profile's provider-owned metadata, so a profile override can be
// observed clearing (or preserving) it.
func seedWorkflowProfileOverrideSession(
	t *testing.T, repo *sqliterepo.Repository, sessionID, agentProfileID string,
) models.SessionRuntimeConfig {
	t.Helper()
	ctx := context.Background()
	seedTaskAndSession(t, repo, "task-"+sessionID, sessionID, models.TaskSessionStateWaitingForInput)

	session, err := repo.GetTaskSession(ctx, sessionID)
	require.NoError(t, err)
	session.AgentProfileID = agentProfileID
	require.NoError(t, repo.UpdateTaskSession(ctx, session))

	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, "acp_session_id", "acp-1"))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeySessionMode, "default"))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyRuntimeConfig,
		models.SessionRuntimeConfig{Model: "creator-model"}))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyACPConfigBaseline,
		map[string]string{"reasoning": "creator"}))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyACPModelState,
		map[string]interface{}{"current_model_id": "creator-model"}))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyContextWindow, 12345))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyLastAgentError,
		models.LastAgentError{Message: "creator failure"}))

	overrides := models.SessionRuntimeConfig{
		Model:         "creator-override-model",
		ConfigOptions: map[string]string{"reasoning_effort": "high"},
	}
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyRuntimeConfigOverrides, overrides))
	return overrides
}

func TestApplyWorkflowStepProfileOverrideClearsCreatorRuntimeSelection(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedWorkflowProfileOverrideSession(t, repo, "s-profile-change", "creator-profile")

	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{})
	session, err := repo.GetTaskSession(ctx, "s-profile-change")
	require.NoError(t, err)

	switched, err := svc.applyWorkflowStepProfileOverride(ctx, session, "step-profile")
	require.NoError(t, err)
	require.True(t, switched, "a different workflow-step profile must switch the session")

	updated, err := repo.GetTaskSession(ctx, "s-profile-change")
	require.NoError(t, err)
	require.Equal(t, "step-profile", updated.AgentProfileID)
	for _, key := range workflowStepProfileOverrideMetadataKeys() {
		_, ok := updated.Metadata[key]
		require.Falsef(t, ok, "provider-owned key %q must be cleared when the step profile changes", key)
	}
	_, ok := models.LoadSessionRuntimeConfigOverrides(updated.Metadata)
	require.False(t, ok, "the creator's explicit runtime selection must not carry into the step profile")
}

func TestApplyWorkflowStepProfileOverrideKeepsRuntimeSelectionForSameProfile(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	overrides := seedWorkflowProfileOverrideSession(t, repo, "s-profile-same", "creator-profile")

	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{})
	session, err := repo.GetTaskSession(ctx, "s-profile-same")
	require.NoError(t, err)

	switched, err := svc.applyWorkflowStepProfileOverride(ctx, session, "creator-profile")
	require.NoError(t, err)
	require.False(t, switched, "an unchanged profile must not switch the session")

	updated, err := repo.GetTaskSession(ctx, "s-profile-same")
	require.NoError(t, err)
	require.Equal(t, "creator-profile", updated.AgentProfileID)
	for _, key := range workflowStepProfileOverrideMetadataKeys() {
		_, ok := updated.Metadata[key]
		require.Truef(t, ok, "provider-owned key %q must survive when the profile is unchanged", key)
	}
	got, ok := models.LoadSessionRuntimeConfigOverrides(updated.Metadata)
	require.True(t, ok, "the creator's explicit runtime selection must survive when the profile is unchanged")
	require.Equal(t, overrides, got)
}
