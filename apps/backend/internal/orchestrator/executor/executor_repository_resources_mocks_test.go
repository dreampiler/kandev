package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	agentdto "github.com/kandev/kandev/internal/agent/dto"
	"github.com/kandev/kandev/internal/task/models"
)

func (m *mockRepository) CreateWorkflow(ctx context.Context, workflow *models.Workflow) error {
	return nil
}
func (m *mockRepository) GetWorkflow(ctx context.Context, id string) (*models.Workflow, error) {
	return nil, nil
}
func (m *mockRepository) UpdateWorkflow(ctx context.Context, workflow *models.Workflow) error {
	return nil
}
func (m *mockRepository) DeleteWorkflow(ctx context.Context, id string) error { return nil }
func (m *mockRepository) ListWorkflows(ctx context.Context, workspaceID string, includeHidden bool) ([]*models.Workflow, error) {
	return nil, nil
}
func (m *mockRepository) ReorderWorkflows(ctx context.Context, workspaceID string, workflowIDs []string) error {
	return nil
}

// Message operations
func (m *mockRepository) CreateMessage(ctx context.Context, message *models.Message) error {
	return nil
}
func (m *mockRepository) GetMessage(ctx context.Context, id string) (*models.Message, error) {
	return nil, nil
}

// GetMessageWithPromptIndex returns the message for id with its derived prompt index, mirroring the repository contract.
func (m *mockRepository) GetMessageWithPromptIndex(ctx context.Context, id string) (*models.Message, error) {
	return nil, nil
}
func (m *mockRepository) GetMessageByToolCallID(ctx context.Context, sessionID, toolCallID string) (*models.Message, error) {
	return nil, nil
}
func (m *mockRepository) GetMessageByPendingID(ctx context.Context, sessionID, pendingID string) (*models.Message, error) {
	return nil, nil
}
func (m *mockRepository) FindMessageByPendingID(ctx context.Context, pendingID string) (*models.Message, error) {
	return nil, nil
}
func (m *mockRepository) FindMessagesByPendingID(ctx context.Context, pendingID string) ([]*models.Message, error) {
	return nil, nil
}
func (m *mockRepository) FindMessageByPendingIDAndQuestion(ctx context.Context, sessionID, pendingID, questionID string) (*models.Message, error) {
	return nil, nil
}
func (m *mockRepository) UpdateMessage(ctx context.Context, message *models.Message) error {
	return nil
}
func (m *mockRepository) ListMessages(ctx context.Context, sessionID string) ([]*models.Message, error) {
	return nil, nil
}
func (m *mockRepository) ListMessagesPaginated(ctx context.Context, sessionID string, opts models.ListMessagesOptions) ([]*models.Message, bool, error) {
	return nil, false, nil
}
func (m *mockRepository) ListMessagesForPlugin(ctx context.Context, filter models.PluginMessageFilter) ([]*models.Message, error) {
	return nil, nil
}
func (m *mockRepository) SearchMessages(ctx context.Context, sessionID string, opts models.SearchMessagesOptions) ([]*models.Message, error) {
	return nil, nil
}
func (m *mockRepository) DeleteMessage(ctx context.Context, id string) error { return nil }

// Turn operations
func (m *mockRepository) CreateTurn(ctx context.Context, turn *models.Turn) error { return nil }
func (m *mockRepository) GetTurn(ctx context.Context, id string) (*models.Turn, error) {
	return nil, nil
}
func (m *mockRepository) GetActiveTurnBySessionID(ctx context.Context, sessionID string) (*models.Turn, error) {
	return nil, nil
}
func (m *mockRepository) UpdateTurn(ctx context.Context, turn *models.Turn) error { return nil }
func (m *mockRepository) PatchTurnMetadata(
	context.Context,
	string,
	string,
	map[string]interface{},
) (bool, time.Time, error) {
	return false, time.Time{}, nil
}
func (m *mockRepository) CompleteTurn(ctx context.Context, id string) error { return nil }
func (m *mockRepository) CompletePendingToolCallsForTurn(ctx context.Context, turnID string) (int64, error) {
	return 0, nil
}
func (m *mockRepository) ListTurnsBySession(ctx context.Context, sessionID string) ([]*models.Turn, error) {
	return nil, nil
}

// Task Session operations
func (m *mockRepository) GetTaskSessionByTaskID(ctx context.Context, taskID string) (*models.TaskSession, error) {
	return nil, nil
}
func (m *mockRepository) GetActiveTaskSessionByTaskID(ctx context.Context, taskID string) (*models.TaskSession, error) {
	return nil, nil
}
func (m *mockRepository) GetTaskSessionByTaskAndAgent(ctx context.Context, taskID, agentInstanceID string) (*models.TaskSession, error) {
	m.mu.Lock()
	fn := m.getTaskSessionByTaskAndAgentFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, taskID, agentInstanceID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.TaskID == taskID && s.AgentProfileID == agentInstanceID {
			return s, nil
		}
	}
	return nil, nil
}
func (m *mockRepository) ListTaskSessions(ctx context.Context, taskID string) ([]*models.TaskSession, error) {
	m.mu.Lock()
	fn := m.listTaskSessionsFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, taskID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	sessions := make([]*models.TaskSession, 0)
	for _, session := range m.sessions {
		if session.TaskID == taskID {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}
func (m *mockRepository) ListActiveTaskSessions(ctx context.Context) ([]*models.TaskSession, error) {
	return nil, nil
}
func (m *mockRepository) ListActiveTaskSessionsByTaskID(ctx context.Context, taskID string) ([]*models.TaskSession, error) {
	m.mu.Lock()
	fn := m.listActiveTaskSessionsByTaskIDFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, taskID)
	}
	return nil, nil
}
func (m *mockRepository) HasActiveTaskSessionsByAgentProfile(ctx context.Context, agentProfileID string) (bool, error) {
	return false, nil
}
func (m *mockRepository) GetActiveTaskInfoByAgentProfile(ctx context.Context, agentProfileID string) ([]agentdto.ActiveTaskInfo, error) {
	return nil, nil
}
func (m *mockRepository) HasActiveTaskSessionsByExecutor(ctx context.Context, executorID string) (bool, error) {
	return false, nil
}
func (m *mockRepository) HasActiveTaskSessionsByEnvironment(ctx context.Context, environmentID string) (bool, error) {
	return false, nil
}
func (m *mockRepository) HasActiveTaskSessionsByRepository(ctx context.Context, repositoryID string) (bool, error) {
	return false, nil
}
func (m *mockRepository) CountActiveTaskSessionsByRepository(ctx context.Context, repositoryID string) (int, error) {
	return 0, nil
}
func (m *mockRepository) DeleteEphemeralTasksByAgentProfile(ctx context.Context, agentProfileID string) (int64, error) {
	return 0, nil
}
func (m *mockRepository) DeleteTaskSession(ctx context.Context, session *models.TaskSession) error {
	return nil
}

// Workflow-related session operations
func (m *mockRepository) GetPrimarySessionByTaskID(ctx context.Context, taskID string) (*models.TaskSession, error) {
	return nil, nil
}
func (m *mockRepository) GetPrimarySessionIDsByTaskIDs(ctx context.Context, taskIDs []string) (map[string]string, error) {
	return nil, nil
}
func (m *mockRepository) GetSessionCountsByTaskIDs(ctx context.Context, taskIDs []string) (map[string]int, error) {
	return nil, nil
}
func (m *mockRepository) GetPrimarySessionInfoByTaskIDs(ctx context.Context, taskIDs []string) (map[string]*models.TaskSession, error) {
	return nil, nil
}
func (m *mockRepository) BatchGetSessionsByTaskIDs(ctx context.Context, taskIDs []string) (map[string][]*models.TaskSession, error) {
	return nil, nil
}
func (m *mockRepository) UpdateSessionWorkflowStep(ctx context.Context, sessionID string, stepID string) error {
	return nil
}
func (m *mockRepository) UpdateSessionReviewStatus(ctx context.Context, sessionID string, status string) error {
	return nil
}
func (m *mockRepository) UpdateSessionMetadata(ctx context.Context, sessionID string, metadata map[string]interface{}) error {
	return nil
}
func (m *mockRepository) SetSessionMetadataKey(ctx context.Context, sessionID, key string, value interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setSessionMetadataKeyCalls = append(m.setSessionMetadataKeyCalls, setSessionMetadataKeyCall{
		SessionID: sessionID,
		Key:       key,
		Value:     value,
	})
	session := m.sessions[sessionID]
	if session == nil {
		return nil
	}
	if session.Metadata == nil {
		session.Metadata = make(map[string]interface{})
	}
	session.Metadata[key] = value
	return nil
}
func (m *mockRepository) GetLastAgentMessage(_ context.Context, _ string) (string, error) {
	return "", nil
}

// Task Session Worktree operations
func (m *mockRepository) ListTaskSessionWorktrees(ctx context.Context, sessionID string) ([]*models.TaskEnvironmentRepo, error) {
	if m.taskEnvironmentRepos == nil {
		return nil, nil
	}
	// Resolve session → environment, then return that environment's repos.
	for _, session := range m.sessions {
		if session == nil || session.ID != sessionID {
			continue
		}
		return m.taskEnvironmentRepos[session.TaskEnvironmentID], nil
	}
	return nil, nil
}
func (m *mockRepository) ListWorktreesBySessionIDs(_ context.Context, _ []string) (map[string][]*models.TaskEnvironmentRepo, error) {
	return make(map[string][]*models.TaskEnvironmentRepo), nil
}

// Git Snapshot operations
func (m *mockRepository) CreateGitSnapshot(ctx context.Context, snapshot *models.GitSnapshot) error {
	return nil
}
func (m *mockRepository) DeleteLiveMonitorSnapshots(ctx context.Context, sessionID string) error {
	return nil
}
func (m *mockRepository) GetLatestGitSnapshot(ctx context.Context, sessionID string) (*models.GitSnapshot, error) {
	return nil, nil
}
func (m *mockRepository) GetFirstGitSnapshot(ctx context.Context, sessionID string) (*models.GitSnapshot, error) {
	return nil, nil
}
func (m *mockRepository) GetGitSnapshotsBySession(ctx context.Context, sessionID string, limit int) ([]*models.GitSnapshot, error) {
	return nil, nil
}

// Session Commit operations
func (m *mockRepository) CreateSessionCommit(ctx context.Context, commit *models.SessionCommit) (bool, error) {
	return true, nil
}
func (m *mockRepository) GetSessionCommits(ctx context.Context, sessionID string) ([]*models.SessionCommit, error) {
	return nil, nil
}
func (m *mockRepository) GetLatestSessionCommit(ctx context.Context, sessionID string) (*models.SessionCommit, error) {
	return nil, nil
}
func (m *mockRepository) DeleteSessionCommit(ctx context.Context, id string) error { return nil }

// Repository operations
func (m *mockRepository) CreateRepository(ctx context.Context, repository *models.Repository) error {
	return nil
}
func (m *mockRepository) UpdateRepository(ctx context.Context, repository *models.Repository) error {
	return nil
}
func (m *mockRepository) DeleteRepository(ctx context.Context, id string) error { return nil }
func (m *mockRepository) ListRepositories(ctx context.Context, workspaceID string) ([]*models.Repository, error) {
	return nil, nil
}

// Repository script operations
func (m *mockRepository) CreateRepositoryScript(ctx context.Context, script *models.RepositoryScript) error {
	return nil
}
func (m *mockRepository) GetRepositoryScript(ctx context.Context, id string) (*models.RepositoryScript, error) {
	return nil, nil
}
func (m *mockRepository) UpdateRepositoryScript(ctx context.Context, script *models.RepositoryScript) error {
	return nil
}
func (m *mockRepository) DeleteRepositoryScript(ctx context.Context, id string) error { return nil }
func (m *mockRepository) ListRepositoryScripts(ctx context.Context, repositoryID string) ([]*models.RepositoryScript, error) {
	return nil, nil
}
func (m *mockRepository) ListScriptsByRepositoryIDs(_ context.Context, _ []string) (map[string][]*models.RepositoryScript, error) {
	return make(map[string][]*models.RepositoryScript), nil
}
func (m *mockRepository) GetRepositoryByProviderIdentity(_ context.Context, _ models.ProviderRepositoryIdentity) (*models.Repository, error) {
	return nil, nil
}
func (m *mockRepository) GetRepositoryByLocalPath(_ context.Context, _, _ string) (*models.Repository, error) {
	return nil, nil
}

// Executor operations
func (m *mockRepository) CreateExecutor(ctx context.Context, executor *models.Executor) error {
	return nil
}
func (m *mockRepository) UpdateExecutor(ctx context.Context, executor *models.Executor) error {
	return nil
}
func (m *mockRepository) DeleteExecutor(ctx context.Context, id string) error { return nil }
func (m *mockRepository) ListExecutors(ctx context.Context) ([]*models.Executor, error) {
	return nil, nil
}

// Executor running operations
func (m *mockRepository) ListExecutorsRunning(ctx context.Context) ([]*models.ExecutorRunning, error) {
	return nil, nil
}
func (m *mockRepository) GetExecutorRunningBySessionID(ctx context.Context, sessionID string) (*models.ExecutorRunning, error) {
	if m.getExecutorRunningFunc != nil {
		return m.getExecutorRunningFunc(ctx, sessionID)
	}
	if running, ok := m.executorsRunning[sessionID]; ok {
		return running, nil
	}
	return nil, nil
}
func (m *mockRepository) ListExecutorsRunningByTaskID(ctx context.Context, taskID string) ([]*models.ExecutorRunning, error) {
	if m.listExecutorsRunningByTaskIDFunc != nil {
		return m.listExecutorsRunningByTaskIDFunc(ctx, taskID)
	}
	var result []*models.ExecutorRunning
	for _, running := range m.executorsRunning {
		if running != nil && running.TaskID == taskID {
			result = append(result, running)
		}
	}
	return result, nil
}
func (m *mockRepository) DeleteExecutorRunningBySessionID(ctx context.Context, sessionID string) error {
	return nil
}
func (m *mockRepository) HasExecutorRunningRow(ctx context.Context, sessionID string) (bool, error) {
	if m.hasExecutorRunningFunc != nil {
		return m.hasExecutorRunningFunc(ctx, sessionID)
	}
	if m.executorsRunning != nil {
		_, ok := m.executorsRunning[sessionID]
		return ok, nil
	}
	return false, nil
}

// UpdateExecutorRunningStatus mirrors the production sqlite repo: returns
// ErrExecutorRunningNotFound when no row exists for the session. Tests that
// exercise the "no row" warning-log path can rely on this, and tests that want
// the status flip to land must seed m.executorsRunning[sessionID] first.
func (m *mockRepository) UpdateExecutorRunningStatus(ctx context.Context, sessionID, status string) error {
	if m.executorsRunning == nil {
		return models.ErrExecutorRunningNotFound
	}
	row, ok := m.executorsRunning[sessionID]
	if !ok {
		return models.ErrExecutorRunningNotFound
	}
	row.Status = status
	return nil
}

// Environment operations
func (m *mockRepository) CreateEnvironment(ctx context.Context, environment *models.Environment) error {
	return nil
}
func (m *mockRepository) GetEnvironment(ctx context.Context, id string) (*models.Environment, error) {
	return nil, nil
}
func (m *mockRepository) UpdateEnvironment(ctx context.Context, environment *models.Environment) error {
	return nil
}
func (m *mockRepository) DeleteEnvironment(ctx context.Context, id string) error { return nil }
func (m *mockRepository) ListEnvironments(ctx context.Context) ([]*models.Environment, error) {
	return nil, nil
}

// Task environment operations
func (m *mockRepository) GetTaskEnvironment(ctx context.Context, id string) (*models.TaskEnvironment, error) {
	if m.getTaskEnvironmentFunc != nil {
		return m.getTaskEnvironmentFunc(ctx, id)
	}
	if env, ok := m.taskEnvironments[id]; ok {
		return env, nil
	}
	return nil, nil
}
func (m *mockRepository) GetTaskEnvironmentByTaskID(ctx context.Context, taskID string) (*models.TaskEnvironment, error) {
	if m.getTaskEnvironmentByTaskIDFunc != nil {
		return m.getTaskEnvironmentByTaskIDFunc(ctx, taskID)
	}
	for _, env := range m.taskEnvironments {
		if env.TaskID == taskID {
			return env, nil
		}
	}
	return nil, nil
}
func (m *mockRepository) ListTaskEnvironmentTaskDirNames(_ context.Context, taskID string) ([]string, error) {
	var names []string
	for _, env := range m.taskEnvironments {
		if env.TaskID == taskID && env.TaskDirName != "" {
			names = append(names, env.TaskDirName)
		}
	}
	return names, nil
}
func (m *mockRepository) CreateTaskEnvironment(_ context.Context, env *models.TaskEnvironment) error {
	if env.ID == "" {
		env.ID = "env-" + env.TaskID
	}
	m.createTaskEnvironmentCalls = append(m.createTaskEnvironmentCalls, env)
	m.taskEnvironments[env.ID] = env
	for _, r := range env.Repos {
		r.TaskEnvironmentID = env.ID
		if r.ID == "" {
			r.ID = env.ID + "-repo-" + r.RepositoryID
		}
	}
	if len(env.Repos) > 0 {
		m.taskEnvironmentRepos[env.ID] = append(m.taskEnvironmentRepos[env.ID], env.Repos...)
	}
	return nil
}
func (m *mockRepository) UpdateTaskEnvironment(_ context.Context, env *models.TaskEnvironment) error {
	if env.ID == "" {
		return nil
	}
	m.writeCallLog = append(m.writeCallLog, "update_env")
	m.updateTaskEnvironmentCalls = append(m.updateTaskEnvironmentCalls, env)
	m.taskEnvironments[env.ID] = env
	return nil
}
func (m *mockRepository) RetireUnusableTaskEnvironment(_ context.Context, environmentID string) (bool, error) {
	return false, nil
}
func (m *mockRepository) ReclaimFailedTaskEnvironmentMaterialization(ctx context.Context, environmentID, sessionID string) (bool, error) {
	if m.reclaimFailedTaskEnvironmentFunc != nil {
		return m.reclaimFailedTaskEnvironmentFunc(ctx, environmentID, sessionID)
	}
	env, ok := m.taskEnvironments[environmentID]
	if !ok || env.Status != models.TaskEnvironmentStatusFailed || env.MaterializationSessionID != "" ||
		env.WorkspacePath != "" || env.ContainerID != "" || env.SandboxID != "" {
		return false, nil
	}
	for _, repo := range env.Repos {
		if repo != nil && repo.DeletedAt == nil {
			return false, nil
		}
	}
	env.Status = models.TaskEnvironmentStatusCreating
	env.MaterializationSessionID = sessionID
	env.ContainerID = ""
	env.ContainerBootstrapNonceSecretID = ""
	env.ContainerControlAuthTokenSecretID = ""
	env.SandboxID = ""
	return true, nil
}
func (m *mockRepository) SetTaskEnvironmentTaskDirNameIfEmpty(_ context.Context, environmentID, taskDirName string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	env := m.taskEnvironments[environmentID]
	if env == nil {
		return false, fmt.Errorf("task environment not found: %s", environmentID)
	}
	if env.TaskDirName != "" {
		return false, nil
	}
	env.TaskDirName = taskDirName
	m.writeCallLog = append(m.writeCallLog, "stamp_task_dir")
	return true, nil
}
func (m *mockRepository) FinalizeTaskEnvironmentMaterialization(_ context.Context, env *models.TaskEnvironment, repos []*models.TaskEnvironmentRepo, _ string) error {
	if m.finalizeTaskEnvironmentErr != nil {
		return m.finalizeTaskEnvironmentErr
	}
	m.finalizeTaskEnvironmentCalls = append(m.finalizeTaskEnvironmentCalls, env)
	m.taskEnvironments[env.ID] = env
	m.taskEnvironmentRepos[env.ID] = repos
	return nil
}
func (m *mockRepository) CreateTaskEnvironmentRepo(_ context.Context, repo *models.TaskEnvironmentRepo) error {
	if m.createTaskEnvironmentRepoErr != nil {
		return m.createTaskEnvironmentRepoErr
	}
	if repo.ID == "" {
		repo.ID = repo.TaskEnvironmentID + "-repo-" + repo.RepositoryID
		if repo.BranchSlug != "" {
			repo.ID += "-branch-" + repo.BranchSlug
		}
	}
	m.writeCallLog = append(m.writeCallLog, "create_repo")
	m.taskEnvironmentRepos[repo.TaskEnvironmentID] = append(m.taskEnvironmentRepos[repo.TaskEnvironmentID], repo)
	return nil
}
func (m *mockRepository) ListTaskEnvironmentRepos(_ context.Context, envID string) ([]*models.TaskEnvironmentRepo, error) {
	return m.taskEnvironmentRepos[envID], nil
}
func (m *mockRepository) UpdateTaskEnvironmentRepo(_ context.Context, repo *models.TaskEnvironmentRepo) error {
	rows := m.taskEnvironmentRepos[repo.TaskEnvironmentID]
	for i, row := range rows {
		if row.ID == repo.ID {
			rows[i] = repo
			m.taskEnvironmentRepos[repo.TaskEnvironmentID] = rows
			return nil
		}
	}
	m.taskEnvironmentRepos[repo.TaskEnvironmentID] = append(rows, repo)
	return nil
}
func (m *mockRepository) RepairWorkspaceInventory(ctx context.Context, repair *models.WorkspaceInventoryRepair) (*models.WorkspaceInventoryRecoveryReceipt, error) {
	if m.repairWorkspaceInventoryFunc != nil {
		return m.repairWorkspaceInventoryFunc(ctx, repair)
	}
	if m.workspaceInventoryReceipts == nil {
		return nil, models.ErrWorkspaceInventoryRecoveryInvalid
	}
	key := repair.TaskID + "\x00" + repair.IdempotencyKey
	if existing, ok := m.workspaceInventoryReceipts[key]; ok {
		if existing.RequestHash != repair.RequestHash {
			return nil, models.ErrWorkspaceInventoryRecoveryIdempotencyConflict
		}
		deduplicated := *existing
		deduplicated.ResultCode = models.WorkspaceInventoryRecoveryDeduplicated
		return &deduplicated, nil
	}
	receipt := &models.WorkspaceInventoryRecoveryReceipt{
		ID: uuid.NewString(), TaskID: repair.TaskID, WorkspaceID: repair.WorkspaceID,
		SessionID: repair.SessionID, TaskEnvironmentID: repair.TaskEnvironmentID,
		TaskRepositoryID: repair.TaskRepositoryID, EnvironmentRepoID: repair.EnvironmentRepoID,
		RepositoryID: repair.RepositoryID, IdempotencyKey: repair.IdempotencyKey,
		RequestHash: repair.RequestHash, ResultCode: models.WorkspaceInventoryRecoveryRepaired,
		ExpectedEnvironmentUpdatedAt:  repair.ExpectedEnvironmentUpdatedAt,
		ExpectedTaskRepositoryUpdate:  repair.ExpectedTaskRepositoryUpdate,
		ExpectedEnvironmentRepoUpdate: repair.ExpectedEnvironmentRepoUpdate,
		Preservation:                  repair.Preservation, CreatedAt: time.Now().UTC(),
	}
	// Store an independent copy: like the real repository, the returned
	// in-memory receipt is decoupled from committed storage once the
	// transaction returns, so a caller mutating fields on its own copy (e.g.
	// surfacing post-repair attestation results) never silently taints what
	// a concurrent or later read observes as durably persisted.
	stored := *receipt
	m.workspaceInventoryReceipts[key] = &stored
	rows := m.taskEnvironmentRepos[repair.TaskEnvironmentID]
	repaired := &models.TaskEnvironmentRepo{
		ID: repair.EnvironmentRepoID, TaskEnvironmentID: repair.TaskEnvironmentID,
		RepositoryID: repair.RepositoryID, BranchSlug: repair.BranchSlug,
		WorktreeID: repair.WorktreeID, WorktreePath: repair.WorktreePath,
		WorktreeBranch: repair.WorktreeBranch, Position: repair.Position,
	}
	updated := false
	for i, row := range rows {
		if row != nil && row.ID == repair.EnvironmentRepoID {
			rows[i] = repaired
			updated = true
			break
		}
	}
	if !updated {
		rows = append(rows, repaired)
	}
	m.taskEnvironmentRepos[repair.TaskEnvironmentID] = rows
	return receipt, nil
}
func (m *mockRepository) GetWorkspaceInventoryRepairReceipt(ctx context.Context, taskID, idempotencyKey string) (*models.WorkspaceInventoryRecoveryReceipt, error) {
	if m.getWorkspaceInventoryRepairReceiptFunc != nil {
		return m.getWorkspaceInventoryRepairReceiptFunc(ctx, taskID, idempotencyKey)
	}
	if m.workspaceInventoryReceipts == nil {
		return nil, nil
	}
	existing, ok := m.workspaceInventoryReceipts[taskID+"\x00"+idempotencyKey]
	if !ok {
		return nil, nil
	}
	found := *existing
	found.ResultCode = models.WorkspaceInventoryRecoveryDeduplicated
	return &found, nil
}

// GetWorkspaceInventoryRepairReceiptForRow returns the latest receipt
// recorded against a specific environment-repo row, regardless of which
// session or idempotency key produced it — mirrors the sqlite repository's
// row-scoped lookup used to gate an already-valid canonical row on durable
// attestation independent of the current caller's own idempotency key.
func (m *mockRepository) GetWorkspaceInventoryRepairReceiptForRow(ctx context.Context, taskID, environmentRepoID string) (*models.WorkspaceInventoryRecoveryReceipt, error) {
	if m.getWorkspaceInventoryRepairReceiptForRowFunc != nil {
		return m.getWorkspaceInventoryRepairReceiptForRowFunc(ctx, taskID, environmentRepoID)
	}
	if m.workspaceInventoryReceipts == nil {
		return nil, nil
	}
	var found *models.WorkspaceInventoryRecoveryReceipt
	for _, existing := range m.workspaceInventoryReceipts {
		if existing == nil || existing.TaskID != taskID || existing.EnvironmentRepoID != environmentRepoID {
			continue
		}
		if found == nil || existing.CreatedAt.After(found.CreatedAt) {
			found = existing
		}
	}
	if found == nil {
		return nil, nil
	}
	copied := *found
	return &copied, nil
}

func (m *mockRepository) RecordWorkspaceInventoryPostRepairAttestation(
	ctx context.Context,
	taskID, idempotencyKey string,
	evidence *models.WorkspaceInventoryPreservation,
	matched bool,
	verifiedAt time.Time,
) error {
	if m.recordWorkspaceInventoryPostRepairAttestationFunc != nil {
		return m.recordWorkspaceInventoryPostRepairAttestationFunc(ctx, taskID, idempotencyKey, evidence, matched, verifiedAt)
	}
	if m.workspaceInventoryReceipts == nil {
		return models.ErrWorkspaceInventoryRecoveryInvalid
	}
	existing, ok := m.workspaceInventoryReceipts[taskID+"\x00"+idempotencyKey]
	if !ok {
		return models.ErrWorkspaceInventoryRecoveryInvalid
	}
	updated := *existing
	updated.PostRepairEvidence = evidence
	updated.PostRepairMatched = matched
	verified := verifiedAt
	updated.PostRepairVerifiedAt = &verified
	m.workspaceInventoryReceipts[taskID+"\x00"+idempotencyKey] = &updated
	return nil
}

// Task Plan operations
func (m *mockRepository) CreateTaskPlan(_ context.Context, plan *models.TaskPlan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plans[plan.TaskID] = plan
	return nil
}
func (m *mockRepository) GetTaskPlan(ctx context.Context, taskID string) (*models.TaskPlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.plans[taskID], nil
}
func (m *mockRepository) UpdateTaskPlan(_ context.Context, plan *models.TaskPlan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plans[plan.TaskID] = plan
	return nil
}
func (m *mockRepository) DeleteTaskPlan(_ context.Context, taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.plans, taskID)
	return nil
}

// Session File Review operations
func (m *mockRepository) UpsertSessionFileReview(ctx context.Context, review *models.SessionFileReview) error {
	return nil
}
func (m *mockRepository) GetSessionFileReviews(ctx context.Context, sessionID string) ([]*models.SessionFileReview, error) {
	return nil, nil
}
func (m *mockRepository) DeleteSessionFileReviews(ctx context.Context, sessionID string) error {
	return nil
}
func (m *mockRepository) CountTasksByWorkflow(ctx context.Context, workflowID string) (int, error) {
	return 0, nil
}
func (m *mockRepository) CountTasksByWorkflowStep(ctx context.Context, stepID string) (int, error) {
	return 0, nil
}

// Executor profile operations
func (m *mockRepository) CreateExecutorProfile(ctx context.Context, profile *models.ExecutorProfile) error {
	return nil
}
func (m *mockRepository) GetExecutorProfile(ctx context.Context, id string) (*models.ExecutorProfile, error) {
	if p, ok := m.executorProfiles[id]; ok {
		return p, nil
	}
	return nil, nil
}
func (m *mockRepository) UpdateExecutorProfile(ctx context.Context, profile *models.ExecutorProfile) error {
	return nil
}
func (m *mockRepository) UpdateExecutorProfileIfUnmodified(ctx context.Context, profile *models.ExecutorProfile, expectedUpdatedAt time.Time) error {
	return nil
}
func (m *mockRepository) DeleteExecutorProfile(ctx context.Context, id string) error { return nil }
func (m *mockRepository) ListExecutorProfiles(ctx context.Context, executorID string) ([]*models.ExecutorProfile, error) {
	return nil, nil
}
func (m *mockRepository) ListAllExecutorProfiles(ctx context.Context) ([]*models.ExecutorProfile, error) {
	return nil, nil
}

// Close operation
func (m *mockRepository) Close() error { return nil }

func (m *mockRepository) FailTaskEnvironmentMaterialization(_ context.Context, sessionID string) (bool, error) {
	m.failMaterializationCalls = append(m.failMaterializationCalls, sessionID)
	for _, env := range m.taskEnvironments {
		if env.Status == models.TaskEnvironmentStatusCreating && env.MaterializationSessionID == sessionID {
			env.Status = models.TaskEnvironmentStatusFailed
			env.MaterializationSessionID = ""
			return true, nil
		}
	}
	return false, nil
}

func (m *mockRepository) ReelectFailedTaskEnvironmentMaterialization(ctx context.Context, environmentID, sessionID string) (bool, error) {
	m.reelectFailedCalls = append(m.reelectFailedCalls, environmentID)
	if m.reelectFailedFunc != nil {
		return m.reelectFailedFunc(ctx, environmentID, sessionID)
	}
	env := m.taskEnvironments[environmentID]
	if env == nil || env.Status != models.TaskEnvironmentStatusFailed || env.MaterializationSessionID != "" ||
		env.WorkspacePath != "" || env.ContainerID != "" || env.SandboxID != "" ||
		len(env.Repos) > 0 || len(m.taskEnvironmentRepos[environmentID]) > 0 {
		return false, nil
	}
	env.Status = models.TaskEnvironmentStatusCreating
	env.MaterializationSessionID = sessionID
	return true, nil
}
