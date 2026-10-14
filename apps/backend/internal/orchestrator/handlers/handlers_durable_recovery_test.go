package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/db"
	"github.com/kandev/kandev/internal/events/bus"
	"github.com/kandev/kandev/internal/orchestrator"
	"github.com/kandev/kandev/internal/orchestrator/executor"
	taskmodels "github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	ws "github.com/kandev/kandev/pkg/websocket"
	"github.com/stretchr/testify/require"
)

// @covers AC-PLATFORM-DURABLE-AGENT-DELIVERY-006.2
func TestRestoreRequiredRecoveryResponseExposesUnresolvedDelivery(t *testing.T) {
	msg := createTestMessage(t, ws.ActionSessionLaunch, map[string]interface{}{})
	err := fmt.Errorf("launch failed: %w", restoreRequiredTestError{reason: "unresolved_durable_work"})
	response, responseErr := restoreRequiredRecoveryResponse(msg, err, "session-interrupted")
	require.NoError(t, responseErr)
	require.NotNil(t, response, "an interrupted durable prompt needs an explicit recovery action")
	payload := parseError(t, response)
	require.Equal(t, ws.ErrorCodeConflict, payload.Code)
	require.Equal(t, "session_restore_required", payload.Details["kind"])
	require.Equal(t, "continue_from_history", payload.Details["recovery_action"])
	require.Equal(t, "unresolved_durable_work", payload.Details["reason"])
	require.Equal(t, "session-interrupted", payload.Details["session_id"])
}

// @covers AC-AGENTS-HARNESS-SESSION-CONTINUITY-006.2
func TestWSLaunchSession_DurableRecovery(t *testing.T) {
	ctx := context.Background()
	dbConn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	sqlxDB := sqlx.NewDb(dbConn, "sqlite3")
	t.Cleanup(func() { _ = sqlxDB.Close() })
	repo, cleanup, err := repository.Provide(sqlxDB, sqlxDB, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cleanup() })
	now := time.Now().UTC()
	require.NoError(t, repo.CreateWorkspace(ctx, &taskmodels.Workspace{ID: "workspace-interrupted", Name: "Test", CreatedAt: now, UpdatedAt: now}))
	require.NoError(t, repo.CreateWorkflow(ctx, &taskmodels.Workflow{ID: "workflow-interrupted", WorkspaceID: "workspace-interrupted", Name: "Test", CreatedAt: now, UpdatedAt: now}))
	require.NoError(t, repo.CreateTask(ctx, &taskmodels.Task{
		ID: "task-interrupted", WorkspaceID: "workspace-interrupted", WorkflowID: "workflow-interrupted",
		Title: "Interrupted task", State: v1.TaskStateInProgress, CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, repo.CreateTaskSession(ctx, &taskmodels.TaskSession{
		ID: "session-interrupted", TaskID: "task-interrupted", State: taskmodels.TaskSessionStateWaitingForInput,
		AgentProfileID: "profile-1", StartedAt: now, UpdatedAt: now,
	}))
	session, err := repo.GetTaskSession(ctx, "session-interrupted")
	require.NoError(t, err)
	continuity := repo
	require.NoError(t, continuity.CreateHarnessSessionGeneration(ctx, &taskmodels.HarnessSessionGeneration{
		SessionID: session.ID, IncarnationID: session.QueueIncarnationID, Generation: 1,
		NativeSessionID: "native-interrupted", AgentType: "codex", CreationReason: "native_started",
		CreatedAt: now, CommittedAt: now,
	}))
	require.NoError(t, continuity.UpsertSessionRecoveryBlock(ctx, &taskmodels.SessionRecoveryBlock{
		ID: "block-interrupted", SessionID: session.ID, IncarnationID: session.QueueIncarnationID,
		ExpectedGeneration: 1, Reason: "unresolved_durable_work", State: taskmodels.RecoveryBlockOpen,
		ConsumerReference: "agent_delivery", CreatedAt: now, UpdatedAt: now,
	}))
	log, err := logger.NewLogger(logger.LoggingConfig{Level: "error", Format: "console", OutputPath: "stderr"})
	require.NoError(t, err)
	service := orchestrator.NewService(orchestrator.ServiceConfig{}, bus.NewMemoryEventBus(log),
		&archivedLaunchAgentManager{}, nil, repo, nil, nil, nil, log)
	handlers := NewHandlers(service, log)
	response, err := handlers.wsLaunchSession(ctx, createTestMessage(t, ws.ActionSessionLaunch, map[string]interface{}{
		"task_id": "task-interrupted", "session_id": session.ID, "intent": string(orchestrator.IntentResume),
	}))
	require.NoError(t, err)
	payload := parseError(t, response)
	require.Equal(t, ws.ErrorCodeConflict, payload.Code)
	require.Equal(t, "continue_from_history", payload.Details["recovery_action"])
	require.Equal(t, "unresolved_durable_work", payload.Details["reason"])
	block, err := continuity.GetOpenSessionRecoveryBlock(ctx, session.ID, session.QueueIncarnationID, 1)
	require.NoError(t, err)
	require.Equal(t, taskmodels.RecoveryBlockOpen, block.State, "showing recovery must not authorize it")
}

type recoveryRecipientAgentManager struct {
	archivedLaunchAgentManager
	runningSession string
	prompts        chan [2]string
	profileErr     error
}

func (m *recoveryRecipientAgentManager) IsAgentRunningForSession(_ context.Context, id string) bool {
	return id == m.runningSession
}
func (m *recoveryRecipientAgentManager) IsAgentReadyForPrompt(_ context.Context, id string) bool {
	return id == m.runningSession
}
func (*recoveryRecipientAgentManager) IsPassthroughSession(context.Context, string) bool {
	return false
}
func (m *recoveryRecipientAgentManager) ResolveAgentProfile(_ context.Context, id string) (*executor.AgentProfileInfo, error) {
	if m.profileErr != nil {
		return nil, m.profileErr
	}
	return &executor.AgentProfileInfo{ProfileID: id, AgentID: "mock", AgentName: "mock"}, nil
}
func (*recoveryRecipientAgentManager) GetExecutionIDForSession(_ context.Context, id string) (string, error) {
	return "execution-" + id, nil
}
func (m *recoveryRecipientAgentManager) PromptAgent(_ context.Context, id, prompt string, _ []v1.MessageAttachment, _ bool) (*executor.PromptResult, error) {
	m.prompts <- [2]string{id, prompt}
	return &executor.PromptResult{}, nil
}
func (m *recoveryRecipientAgentManager) PromptAgentWithDispatchCallback(ctx context.Context, id, prompt string, attachments []v1.MessageAttachment, dispatchOnly bool, dispatched func()) (*executor.PromptResult, error) {
	dispatched()
	return m.PromptAgent(ctx, id, prompt, attachments, dispatchOnly)
}

type recoveryRecipientSteps struct {
	steps map[string]*wfmodels.WorkflowStep
}

func (g recoveryRecipientSteps) GetStep(_ context.Context, id string) (*wfmodels.WorkflowStep, error) {
	return g.steps[id], nil
}
func (g recoveryRecipientSteps) GetNextStepByPosition(context.Context, string, int) (*wfmodels.WorkflowStep, error) {
	return nil, nil
}
func (g recoveryRecipientSteps) GetPreviousStepByPosition(context.Context, string, int) (*wfmodels.WorkflowStep, error) {
	return nil, nil
}
func (g recoveryRecipientSteps) GetWorkflowMeta(context.Context, string) (orchestrator.WorkflowMeta, error) {
	return orchestrator.WorkflowMeta{}, nil
}

type recoveryRecipientTasks struct{ *sqliterepo.Repository }

func (r recoveryRecipientTasks) GetTask(ctx context.Context, id string) (*v1.Task, error) {
	task, err := r.Repository.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	return &v1.Task{ID: task.ID, WorkspaceID: task.WorkspaceID, WorkflowID: task.WorkflowID, Title: task.Title, Description: task.Description, State: task.State}, nil
}
func (r recoveryRecipientTasks) UpdateTaskStateIfCurrentIn(ctx context.Context, id string, state v1.TaskState, allowed []v1.TaskState) (bool, error) {
	_, changed, err := r.Repository.UpdateTaskStateIfCurrentIn(ctx, id, state, allowed)
	return changed, err
}
func (r recoveryRecipientTasks) UpdateTaskStateIfNotArchived(ctx context.Context, id string, state v1.TaskState) (bool, error) {
	_, changed, err := r.Repository.UpdateTaskStateIfNotArchived(ctx, id, state)
	return changed, err
}
func (r recoveryRecipientTasks) UpdateTaskStateIfSessionState(ctx context.Context, taskID, sessionID string, expected taskmodels.TaskSessionState, state v1.TaskState) (bool, error) {
	_, changed, err := r.Repository.UpdateTaskStateIfSessionState(ctx, taskID, sessionID, expected, state)
	return changed, err
}

// @covers AC-TASKS-WORKFLOW-PROFILE-SESSIONS-004.1
func TestWSRecoveryRecipientRegistersExplicitSuccession(t *testing.T) {
	ctx := context.Background()
	conn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "recovery.db"))
	require.NoError(t, err)
	database := sqlx.NewDb(conn, "sqlite3")
	t.Cleanup(func() { _ = database.Close() })
	repo, cleanup, err := repository.Provide(database, database, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cleanup() })
	require.NoError(t, repo.CreateTask(ctx, &taskmodels.Task{ID: "recovery-task", Title: "Recovery"}))
	for _, session := range []*taskmodels.TaskSession{
		{ID: "original", TaskID: "recovery-task", AgentProfileID: "old-profile", State: taskmodels.TaskSessionStateFailed, IsPrimary: true},
		{ID: "replacement", TaskID: "recovery-task", AgentProfileID: "new-profile", State: taskmodels.TaskSessionStateWaitingForInput},
	} {
		require.NoError(t, repo.CreateTaskSession(ctx, session))
	}
	before, err := repo.GetTask(ctx, "recovery-task")
	require.NoError(t, err)
	snapshot, ok := taskmodels.LoadWorkflowInitialSessionSnapshot(before.Metadata)
	require.True(t, ok)
	log, err := logger.NewLogger(logger.LoggingConfig{Level: "error", Format: "console", OutputPath: "stderr"})
	require.NoError(t, err)
	agentManager := &recoveryRecipientAgentManager{prompts: make(chan [2]string, 2)}
	service := orchestrator.NewService(orchestrator.ServiceConfig{}, bus.NewMemoryEventBus(log),
		agentManager, recoveryRecipientTasks{repo}, repo, nil, nil, nil, log)
	handlers := NewHandlers(service, log)
	ordinary, err := handlers.wsSetPrimarySession(ctx, createTestMessage(t, ws.ActionSessionSetPrimary, map[string]interface{}{"session_id": "replacement"}))
	require.NoError(t, err)
	require.NotEqual(t, ws.MessageTypeError, ordinary.Type)
	promoted, err := repo.GetTask(ctx, "recovery-task")
	require.NoError(t, err)
	require.NotContains(t, promoted.Metadata, taskmodels.MetaKeyWorkflowInitialRecipientBinding)
	priorRoute := taskmodels.WorkflowSessionRoute{
		OperationID: "prior-entry", DestinationStepID: "prior-review", TargetKind: "initial",
		AgentProfileID: "old-profile", DestinationID: "original", Phase: "committed",
	}
	require.NoError(t, repo.SetTaskMetadataKey(ctx, "recovery-task", taskmodels.MetaKeyWorkflowSessionRoute, priorRoute))
	agentManager.profileErr = fmt.Errorf("profile removed")
	rejected, err := handlers.wsSetPrimarySession(ctx, createTestMessage(t, ws.ActionSessionSetPrimary, map[string]interface{}{
		"session_id": "replacement", "replaces_initial_recipient_session_id": "original",
	}))
	require.NoError(t, err)
	require.Equal(t, ws.MessageTypeError, rejected.Type)
	agentManager.profileErr = nil
	for range 2 {
		response, err := handlers.wsSetPrimarySession(ctx, createTestMessage(t, ws.ActionSessionSetPrimary, map[string]interface{}{
			"session_id": "replacement", "replaces_initial_recipient_session_id": "original",
		}))
		require.NoError(t, err)
		var result struct{ Success bool }
		require.NoError(t, json.Unmarshal(response.Payload, &result))
		require.True(t, result.Success, string(response.Payload))
		after, err := repo.GetTask(ctx, "recovery-task")
		require.NoError(t, err)
		require.Contains(t, after.Metadata, "workflow_initial_recipient_binding")
		saved, ok := taskmodels.LoadWorkflowInitialSessionSnapshot(after.Metadata)
		require.True(t, ok)
		require.Equal(t, snapshot, saved)
		savedRoute, ok := taskmodels.LoadWorkflowSessionRoute(after.Metadata)
		require.True(t, ok)
		require.Equal(t, priorRoute, savedRoute)
	}
	work := &wfmodels.WorkflowStep{}
	require.NoError(t, json.Unmarshal([]byte(`{"id":"work","workflow_id":"recovery-workflow","name":"Work","position":0,"profile_session_end_policy":"park","events":{"on_turn_start":[{"type":"move_to_step","config":{"step_id":"review"}}]}}`), work))
	review := &wfmodels.WorkflowStep{}
	require.NoError(t, json.Unmarshal([]byte(`{"id":"review","workflow_id":"recovery-workflow","name":"Review","position":1,"session_target":{"kind":"initial"},"prompt":"registered successor receives this step","events":{"on_enter":[{"type":"auto_start_agent"}]}}`), review))
	require.NoError(t, repo.CreateWorkspace(ctx, &taskmodels.Workspace{ID: "recovery-workspace", Name: "Recovery"}))
	require.NoError(t, repo.CreateWorkflow(ctx, &taskmodels.Workflow{ID: "recovery-workflow", WorkspaceID: "recovery-workspace", Name: "Recovery"}))
	task, err := repo.GetTask(ctx, "recovery-task")
	require.NoError(t, err)
	task.WorkflowID, task.WorkflowStepID, task.WorkspaceID = "recovery-workflow", work.ID, "recovery-workspace"
	task.State = v1.TaskStateInProgress
	require.NoError(t, repo.UpdateTask(ctx, task))
	service.SetWorkflowStepGetter(recoveryRecipientSteps{steps: map[string]*wfmodels.WorkflowStep{work.ID: work, review.ID: review}})
	agentManager.runningSession = "replacement"
	require.NoError(t, repo.UpsertExecutorRunning(ctx, &taskmodels.ExecutorRunning{
		ID: "replacement-runtime", SessionID: "replacement", TaskID: task.ID,
		AgentExecutionID: "execution-replacement", Status: "ready",
	}))
	_, err = service.ProcessOnTurnStart(ctx, task.ID, "replacement")
	require.NoError(t, err)
	response, err := handlers.wsLaunchSession(ctx, createTestMessage(t, ws.ActionSessionLaunch, map[string]interface{}{
		"task_id": task.ID, "session_id": "replacement", "intent": "workflow_step", "workflow_step_id": review.ID,
	}))
	require.NoError(t, err)
	require.NotEqual(t, ws.MessageTypeError, response.Type, string(response.Payload))
	select {
	case delivered := <-agentManager.prompts:
		require.Equal(t, "execution-replacement", delivered[0])
		require.Contains(t, delivered[1], review.Prompt)
	case <-time.After(5 * time.Second):
		t.Fatal("normal workflow entry did not deliver its prompt")
	}
	savedTask, err := repo.GetTask(ctx, task.ID)
	require.NoError(t, err)
	route, ok := taskmodels.LoadWorkflowSessionRoute(savedTask.Metadata)
	require.True(t, ok)
	require.Equal(t, "replacement", route.DestinationID)
	require.Equal(t, "committed", route.Phase)
	select {
	case duplicate := <-agentManager.prompts:
		t.Fatalf("duplicate entry prompt: %v", duplicate)
	default:
	}
}
