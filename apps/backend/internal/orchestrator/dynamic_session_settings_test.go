package orchestrator

import (
	"context"
	"testing"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	"github.com/stretchr/testify/require"
)

// agentIdentityByProfileManager resolves a profile ID to its owning agent
// identity from a fixed map, so a test can model a same-agent candidate swap
// (two candidate IDs on one agent) and a cross-agent swap.
type agentIdentityByProfileManager struct {
	*mockAgentManager
	agentByProfile map[string]string
}

func (m *agentIdentityByProfileManager) ResolveAgentProfile(_ context.Context, profileID string) (*executor.AgentProfileInfo, error) {
	return &executor.AgentProfileInfo{AgentID: m.agentByProfile[profileID]}, nil
}

const (
	testCodexAgentID    = "codex-acp"
	testOpencodeAgentID = "opencode-acp"
)

var candidateCleanupProviderKeys = []string{
	"acp_session_id",
	models.SessionMetaKeySessionMode,
	models.SessionMetaKeyRuntimeConfig,
	models.SessionMetaKeyACPConfigBaseline,
	models.SessionMetaKeyACPModelState,
	models.SessionMetaKeyContextWindow,
}

func seedCandidateCleanupSession(t *testing.T, repo *sqliterepo.Repository, sessionID, executionProfileID string) models.SessionRuntimeConfig {
	t.Helper()
	ctx := context.Background()
	seedTaskAndSession(t, repo, "t1", sessionID, models.TaskSessionStateWaitingForInput)

	session, err := repo.GetTaskSession(ctx, sessionID)
	require.NoError(t, err)
	session.ExecutionProfileID = executionProfileID
	require.NoError(t, repo.UpdateTaskSession(ctx, session))

	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, "acp_session_id", "acp-1"))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeySessionMode, "default"))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyRuntimeConfig,
		models.SessionRuntimeConfig{Model: "provider-default"}))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyACPConfigBaseline,
		map[string]string{"reasoning": "provider"}))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyACPModelState,
		map[string]interface{}{"current": "provider-default"}))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyContextWindow, 12345))

	overrides := models.SessionRuntimeConfig{
		Model:         "gpt-6.1-sol",
		ConfigOptions: map[string]string{"reasoning_effort": "high"},
	}
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyRuntimeConfigOverrides, overrides))
	return overrides
}

func requireProviderKeysCleared(t *testing.T, metadata map[string]interface{}) {
	t.Helper()
	for _, key := range candidateCleanupProviderKeys {
		_, ok := metadata[key]
		require.Falsef(t, ok, "provider-owned key %q must be cleared on a candidate change", key)
	}
}

func TestPersistDynamicLaunchDecision_SameAgentCandidatePreservesExplicitSelection(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	overrides := seedCandidateCleanupSession(t, repo, "s-same-agent", "candidate-codex-1")

	mgr := &agentIdentityByProfileManager{
		mockAgentManager: &mockAgentManager{},
		agentByProfile: map[string]string{
			"candidate-codex-1": testCodexAgentID,
			"candidate-codex-2": testCodexAgentID,
		},
	}
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), newMockTaskRepo(), mgr)

	err := svc.persistDynamicLaunchDecision(ctx, "s-same-agent", dynamicruntime.RouteDecision{
		ExecutionProfileID: "candidate-codex-2",
		Generation:         2,
		Status:             "starting",
		Reason:             "candidate_order",
	})
	require.NoError(t, err)

	updated, err := repo.GetTaskSession(ctx, "s-same-agent")
	require.NoError(t, err)
	requireProviderKeysCleared(t, updated.Metadata)
	got, ok := models.LoadSessionRuntimeConfigOverrides(updated.Metadata)
	require.True(t, ok, "explicit selection must survive a same-agent candidate change")
	require.Equal(t, overrides, got)
}

func TestPersistDynamicLaunchDecision_CrossAgentCandidateClearsExplicitSelection(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedCandidateCleanupSession(t, repo, "s-cross-agent", "candidate-codex-1")

	mgr := &agentIdentityByProfileManager{
		mockAgentManager: &mockAgentManager{},
		agentByProfile: map[string]string{
			"candidate-codex-1":    testCodexAgentID,
			"candidate-opencode-1": testOpencodeAgentID,
		},
	}
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), newMockTaskRepo(), mgr)

	err := svc.persistDynamicLaunchDecision(ctx, "s-cross-agent", dynamicruntime.RouteDecision{
		ExecutionProfileID: "candidate-opencode-1",
		Generation:         2,
		Status:             "starting",
		Reason:             "candidate_order",
	})
	require.NoError(t, err)

	updated, err := repo.GetTaskSession(ctx, "s-cross-agent")
	require.NoError(t, err)
	requireProviderKeysCleared(t, updated.Metadata)
	_, ok := models.LoadSessionRuntimeConfigOverrides(updated.Metadata)
	require.False(t, ok, "explicit selection must be cleared on a cross-agent candidate change")
}

func TestPersistDynamicLaunchDecision_UnresolvedAgentClearsExplicitSelection(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedCandidateCleanupSession(t, repo, "s-unresolved", "candidate-codex-1")

	// The successor identity cannot be resolved; the selection must fail closed
	// rather than carry over to an unknown agent.
	mgr := &agentIdentityByProfileManager{
		mockAgentManager: &mockAgentManager{},
		agentByProfile: map[string]string{
			"candidate-codex-1": testCodexAgentID,
		},
	}
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), newMockTaskRepo(), mgr)

	err := svc.persistDynamicLaunchDecision(ctx, "s-unresolved", dynamicruntime.RouteDecision{
		ExecutionProfileID: "candidate-unknown",
		Generation:         2,
		Status:             "starting",
		Reason:             "candidate_order",
	})
	require.NoError(t, err)

	updated, err := repo.GetTaskSession(ctx, "s-unresolved")
	require.NoError(t, err)
	_, ok := models.LoadSessionRuntimeConfigOverrides(updated.Metadata)
	require.False(t, ok, "an unresolved agent identity must clear the explicit selection")
}

func TestUpdateDynamicLaunchSession_SameCandidateRetryKeepsEverything(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	overrides := seedCandidateCleanupSession(t, repo, "s-retry", "candidate-codex-1")

	mgr := &agentIdentityByProfileManager{
		mockAgentManager: &mockAgentManager{},
		agentByProfile:   map[string]string{"candidate-codex-1": testCodexAgentID},
	}
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), newMockTaskRepo(), mgr)

	session, err := repo.GetTaskSession(ctx, "s-retry")
	require.NoError(t, err)
	// Same-candidate retry: the previous execution profile equals the session's
	// current one, so nothing is cleared.
	err = svc.updateDynamicLaunchSession(ctx, session, "candidate-codex-1", session.State, testCodexAgentID, testCodexAgentID)
	require.NoError(t, err)

	updated, err := repo.GetTaskSession(ctx, "s-retry")
	require.NoError(t, err)
	for _, key := range candidateCleanupProviderKeys {
		_, ok := updated.Metadata[key]
		require.Truef(t, ok, "provider key %q must survive a same-candidate retry", key)
	}
	got, ok := models.LoadSessionRuntimeConfigOverrides(updated.Metadata)
	require.True(t, ok)
	require.Equal(t, overrides, got)
}
