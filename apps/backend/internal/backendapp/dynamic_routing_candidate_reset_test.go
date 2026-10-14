package backendapp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	agentsettingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	settingsstore "github.com/kandev/kandev/internal/agent/settings/store"
	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/db"
	"github.com/kandev/kandev/internal/orchestrator"
	"github.com/kandev/kandev/internal/task/models"
	taskreposqlite "github.com/kandev/kandev/internal/task/repository/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// routeActionProviderStateKeys mirrors the provider-owned keys the shared
// orchestrator helper clears on a candidate change. The route-action path must
// clear the same set so a fresh ACP session cannot inherit the previous
// candidate's model, mode, ACP identity or config snapshot.
var routeActionProviderStateKeys = []string{
	"acp_session_id",
	models.SessionMetaKeySessionMode,
	models.SessionMetaKeyRuntimeConfig,
	models.SessionMetaKeyACPConfigBaseline,
	models.SessionMetaKeyACPModelState,
	models.SessionMetaKeyContextWindow,
}

func newRouteActionTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.NewFromZap(zap.NewNop())
	require.NoError(t, err)
	return log
}

// seedRouteActionSession creates a task session carrying a full provider-owned
// metadata set plus an explicit user selection, so a candidate change can be
// observed clearing the provider state while preserving or dropping the
// selection.
func seedRouteActionSession(
	t *testing.T,
	repo *taskreposqlite.Repository,
	sessionID, executionProfileID string,
) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws-1", Name: "WS"}))
	require.NoError(t, repo.CreateWorkflow(ctx, &models.Workflow{ID: "wf-1", WorkspaceID: "ws-1", Name: "WF"}))
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: "task-1", WorkspaceID: "ws-1", WorkflowID: "wf-1", Title: "T", Priority: "medium",
	}))
	require.NoError(t, repo.CreateTaskSession(ctx, &models.TaskSession{
		ID: sessionID, TaskID: "task-1", AgentProfileID: "profile-dynamic",
		ExecutionProfileID: executionProfileID, State: models.TaskSessionStateWaitingForInput,
	}))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, "acp_session_id", "acp-previous"))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeySessionMode, "build"))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyRuntimeConfig,
		models.SessionRuntimeConfig{Model: "previous-candidate-model"}))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyACPConfigBaseline,
		map[string]string{"reasoning": "previous"}))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyACPModelState,
		map[string]interface{}{"current_model_id": "previous-candidate-model"}))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyContextWindow, 12345))
	require.NoError(t, repo.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyRuntimeConfigOverrides,
		models.SessionRuntimeConfig{Model: "user-selected-model"}))
}

func requireRouteActionProviderKeysCleared(t *testing.T, metadata map[string]interface{}) {
	t.Helper()
	for _, key := range routeActionProviderStateKeys {
		_, ok := metadata[key]
		require.Falsef(t, ok, "provider-owned key %q must be cleared on a candidate change", key)
	}
}

func TestPersistDynamicRouteSelectionClearsProviderStateOnSameAgentCandidateChange(t *testing.T) {
	ctx := context.Background()
	_, profileRepo, repo := openTasklessDynamicRepo(t)
	seedRouteActionSession(t, repo, "session-same-agent", "candidate-a")
	require.NoError(t, profileRepo.CreateAgentProfile(ctx, &agentsettingsmodels.AgentProfile{
		ID: "candidate-a", AgentID: "concrete-agent", Name: "A", Model: "model-a", Enabled: true,
	}))
	require.NoError(t, profileRepo.CreateAgentProfile(ctx, &agentsettingsmodels.AgentProfile{
		ID: "candidate-b", AgentID: "concrete-agent", Name: "B", Model: "model-b", Enabled: true,
	}))

	session, err := repo.GetTaskSession(ctx, "session-same-agent")
	require.NoError(t, err)
	decision := agentruntime.ProfileExecution{
		ExecutionProfileID: "candidate-b",
		Generation:         2,
		Profile:            &agentsettingsmodels.AgentProfile{ID: "candidate-b", AgentID: "concrete-agent", Model: "model-b"},
	}
	changed, err := persistDynamicRouteSelection(
		ctx, repo, profileRepo, session, session.State, decision, newRouteActionTestLogger(t),
	)
	require.NoError(t, err)
	require.True(t, changed)

	updated, err := repo.GetTaskSession(ctx, "session-same-agent")
	require.NoError(t, err)
	requireRouteActionProviderKeysCleared(t, updated.Metadata)
	require.Equal(t, "candidate-b", updated.ExecutionProfileID)
	require.Equal(t, int64(2), updated.RouteGeneration)
	require.Empty(t, updated.DownstreamACPSessionID)
	overrides, ok := models.LoadSessionRuntimeConfigOverrides(updated.Metadata)
	require.True(t, ok, "an explicit selection must survive a same-agent candidate change")
	require.Equal(t, "user-selected-model", overrides.Model)
}

func TestPersistDynamicRouteSelectionClearsExplicitSelectionOnCrossAgentCandidateChange(t *testing.T) {
	ctx := context.Background()
	_, profileRepo, repo := openTasklessDynamicRepo(t)
	seedRouteActionSession(t, repo, "session-cross-agent", "candidate-a")
	require.NoError(t, profileRepo.CreateAgentProfile(ctx, &agentsettingsmodels.AgentProfile{
		ID: "candidate-a", AgentID: "concrete-agent", Name: "A", Model: "model-a", Enabled: true,
	}))
	require.NoError(t, profileRepo.CreateAgentProfile(ctx, &agentsettingsmodels.AgentProfile{
		ID: "candidate-b", AgentID: "dynamic", Name: "B", Model: "model-b", Enabled: true,
	}))

	session, err := repo.GetTaskSession(ctx, "session-cross-agent")
	require.NoError(t, err)
	decision := agentruntime.ProfileExecution{
		ExecutionProfileID: "candidate-b",
		Generation:         2,
		Profile:            &agentsettingsmodels.AgentProfile{ID: "candidate-b", AgentID: "dynamic", Model: "model-b"},
	}
	changed, err := persistDynamicRouteSelection(
		ctx, repo, profileRepo, session, session.State, decision, newRouteActionTestLogger(t),
	)
	require.NoError(t, err)
	require.True(t, changed)

	updated, err := repo.GetTaskSession(ctx, "session-cross-agent")
	require.NoError(t, err)
	requireRouteActionProviderKeysCleared(t, updated.Metadata)
	_, ok := models.LoadSessionRuntimeConfigOverrides(updated.Metadata)
	require.False(t, ok, "an explicit selection must be cleared on a cross-agent candidate change")
}

func TestPersistDynamicRouteSelectionSameCandidateRetryKeepsProviderState(t *testing.T) {
	ctx := context.Background()
	_, profileRepo, repo := openTasklessDynamicRepo(t)
	seedRouteActionSession(t, repo, "session-retry", "candidate-a")
	require.NoError(t, profileRepo.CreateAgentProfile(ctx, &agentsettingsmodels.AgentProfile{
		ID: "candidate-a", AgentID: "concrete-agent", Name: "A", Model: "model-a", Enabled: true,
	}))

	session, err := repo.GetTaskSession(ctx, "session-retry")
	require.NoError(t, err)
	decision := agentruntime.ProfileExecution{
		ExecutionProfileID: "candidate-a",
		Generation:         1,
		Profile:            &agentsettingsmodels.AgentProfile{ID: "candidate-a", AgentID: "concrete-agent", Model: "model-a"},
	}
	changed, err := persistDynamicRouteSelection(
		ctx, repo, profileRepo, session, session.State, decision, newRouteActionTestLogger(t),
	)
	require.NoError(t, err)
	require.True(t, changed)

	updated, err := repo.GetTaskSession(ctx, "session-retry")
	require.NoError(t, err)
	for _, key := range routeActionProviderStateKeys {
		_, ok := updated.Metadata[key]
		require.Truef(t, ok, "provider key %q must survive a same-candidate retry", key)
	}
	overrides, ok := models.LoadSessionRuntimeConfigOverrides(updated.Metadata)
	require.True(t, ok)
	require.Equal(t, "user-selected-model", overrides.Model)
}

func TestDynamicCandidateModelReplacementReportsStaleProviderModel(t *testing.T) {
	decision := agentruntime.ProfileExecution{
		ExecutionProfileID: "candidate-b",
		Profile:            &agentsettingsmodels.AgentProfile{ID: "candidate-b", AgentID: "concrete-agent", Model: "candidate-model"},
	}

	stale := &models.TaskSession{
		ID:       "s-stale",
		Metadata: map[string]interface{}{models.SessionMetaKeyRuntimeConfig: models.SessionRuntimeConfig{Model: "previous-model"}},
	}
	previousModel, candidateModel, ok := dynamicCandidateModelReplacement(stale, decision)
	require.True(t, ok)
	require.Equal(t, "previous-model", previousModel)
	require.Equal(t, "candidate-model", candidateModel)

	matching := &models.TaskSession{
		ID:       "s-match",
		Metadata: map[string]interface{}{models.SessionMetaKeyRuntimeConfig: models.SessionRuntimeConfig{Model: "candidate-model"}},
	}
	_, _, ok = dynamicCandidateModelReplacement(matching, decision)
	require.False(t, ok, "an already matching provider model is not a stale replacement")

	explicit := &models.TaskSession{
		ID: "s-explicit",
		Metadata: map[string]interface{}{
			models.SessionMetaKeyRuntimeConfig:          models.SessionRuntimeConfig{Model: "previous-model"},
			models.SessionMetaKeyRuntimeConfigOverrides: models.SessionRuntimeConfig{Model: "user-model"},
		},
	}
	_, _, ok = dynamicCandidateModelReplacement(explicit, decision)
	require.False(t, ok, "an explicit user selection is intentional, not a stale provider model")
}

func TestDynamicCandidateProviderStateKeysMatchesAgentIdentity(t *testing.T) {
	sameAgent := orchestrator.DynamicCandidateProviderStateKeys("agent-x", "agent-x")
	require.NotContains(t, sameAgent, models.SessionMetaKeyRuntimeConfigOverrides)
	require.Contains(t, sameAgent, models.SessionMetaKeyRuntimeConfig)

	crossAgent := orchestrator.DynamicCandidateProviderStateKeys("agent-x", "agent-y")
	require.Contains(t, crossAgent, models.SessionMetaKeyRuntimeConfigOverrides)

	unknown := orchestrator.DynamicCandidateProviderStateKeys("agent-x", "")
	require.Contains(t, unknown, models.SessionMetaKeyRuntimeConfigOverrides)
}

// openRouteActionRepo returns the settings and task stores plus the raw handle so
// a test can fault-inject a store failure by closing the database.
func openRouteActionRepo(t *testing.T) (settingsstore.Repository, *taskreposqlite.Repository, *sqlx.DB) {
	t.Helper()
	dbConn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "route-action-fault.db"))
	require.NoError(t, err)
	sqlxDB := sqlx.NewDb(dbConn, "sqlite3")
	t.Cleanup(func() { _ = sqlxDB.Close() })

	settingsRepo, cleanup, err := settingsstore.Provide(sqlxDB, sqlxDB, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cleanup() })
	taskRepo, err := taskreposqlite.NewWithDB(sqlxDB, sqlxDB, nil)
	require.NoError(t, err)

	ctx := context.Background()
	for _, id := range []string{"dynamic", "concrete-agent"} {
		_, err := sqlxDB.Exec(
			`INSERT INTO agents (id, name, created_at, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			id, id,
		)
		require.NoError(t, err)
	}
	for _, profile := range []*agentsettingsmodels.AgentProfile{
		{ID: "profile-dynamic", AgentID: "dynamic", Name: "Dyn", Enabled: true},
		{ID: "candidate-a", AgentID: "concrete-agent", Name: "A", Model: "model-a", Enabled: true},
		{ID: "candidate-b", AgentID: "concrete-agent", Name: "B", Model: "model-b", Enabled: true},
	} {
		require.NoError(t, settingsRepo.CreateAgentProfile(ctx, profile))
	}
	return settingsRepo, taskRepo, sqlxDB
}

func TestPersistDynamicRouteSelectionSameCandidateStoreFailureWrapsError(t *testing.T) {
	ctx := context.Background()
	profileRepo, repo, sqlxDB := openRouteActionRepo(t)
	seedRouteActionSession(t, repo, "session-fail", "candidate-a")

	session, err := repo.GetTaskSession(ctx, "session-fail")
	require.NoError(t, err)
	decision := agentruntime.ProfileExecution{
		ExecutionProfileID: "candidate-a",
		Generation:         1,
		Profile:            &agentsettingsmodels.AgentProfile{ID: "candidate-a", AgentID: "concrete-agent", Model: "model-a"},
	}
	// Closing the pool makes the guarded write fail, so the same-candidate
	// branch must wrap the raw store error the way the candidate-change branch
	// does.
	require.NoError(t, sqlxDB.Close())

	changed, err := persistDynamicRouteSelection(
		ctx, repo, profileRepo, session, session.State, decision, newRouteActionTestLogger(t),
	)
	require.False(t, changed)
	require.Error(t, err)
	require.Contains(t, err.Error(), "persist route action session state")
}

func TestPersistDynamicRouteSelectionUnchangedDoesNotLogReplacement(t *testing.T) {
	ctx := context.Background()
	profileRepo, repo, _ := openRouteActionRepo(t)
	seedRouteActionSession(t, repo, "session-nolog", "candidate-a")
	// Remove the explicit selection so the provider-recorded model is the only
	// source and the replacement predicate would be true if the write committed.
	require.NoError(t, repo.SetSessionMetadataKey(ctx, "session-nolog", models.SessionMetaKeyRuntimeConfigOverrides, nil))

	session, err := repo.GetTaskSession(ctx, "session-nolog")
	require.NoError(t, err)
	decision := agentruntime.ProfileExecution{
		ExecutionProfileID: "candidate-b",
		Generation:         2,
		Profile:            &agentsettingsmodels.AgentProfile{ID: "candidate-b", AgentID: "concrete-agent", Model: "model-b"},
	}
	core, obs := observer.New(zap.DebugLevel)
	log, err := logger.NewFromZap(zap.New(core))
	require.NoError(t, err)

	// A mismatched expected state makes the guarded write a no-op (changed=false).
	changed, err := persistDynamicRouteSelection(
		ctx, repo, profileRepo, session, models.TaskSessionStateFailed, decision, log,
	)
	require.NoError(t, err)
	require.False(t, changed)
	require.Empty(t, obs.FilterMessage("dynamic candidate change replaces the previous candidate's recorded model").All(),
		"a no-op write must not record a model replacement")
}
