package executor

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"encoding/json"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// mockRepository implements executorStore for testing
type mockRepository struct {
	// Membership is not exercised by this fake; the embedded default
	// reports no membership, which is the narrower answer.
	repository.UnsupportedWorkspaceMembers
	mu                   sync.Mutex
	sessions             map[string]*models.TaskSession
	tasks                map[string]*models.Task
	taskRepositories     map[string]*models.TaskRepository
	repositories         map[string]*models.Repository
	executors            map[string]*models.Executor
	executorProfiles     map[string]*models.ExecutorProfile
	executorsRunning     map[string]*models.ExecutorRunning
	taskEnvironments     map[string]*models.TaskEnvironment
	taskEnvironmentRepos map[string][]*models.TaskEnvironmentRepo // env_id → rows
	plans                map[string]*models.TaskPlan              // task_id → plan

	// Optional hook to inject behavior into GetTaskSession (e.g. simulate a
	// transient DB error); if nil, the default map lookup is used.
	getTaskSessionFunc             func(ctx context.Context, id string) (*models.TaskSession, error)
	getTaskFunc                    func(ctx context.Context, id string) (*models.Task, error)
	getTaskEnvironmentFunc         func(ctx context.Context, id string) (*models.TaskEnvironment, error)
	getTaskEnvironmentByTaskIDFunc func(ctx context.Context, taskID string) (*models.TaskEnvironment, error)
	// reclaimFailedTaskEnvironmentFunc, when non-nil, overrides
	// ReclaimFailedTaskEnvironmentMaterialization entirely — used to simulate a
	// concurrent launch winning the reclaim CAS.
	reclaimFailedTaskEnvironmentFunc func(ctx context.Context, environmentID, sessionID string) (bool, error)
	reelectFailedFunc                func(ctx context.Context, environmentID, sessionID string) (bool, error)
	reelectFailedCalls               []string
	failMaterializationCalls         []string
	getExecutorRunningFunc           func(ctx context.Context, sessionID string) (*models.ExecutorRunning, error)
	hasExecutorRunningFunc           func(ctx context.Context, sessionID string) (bool, error)
	createTaskEnvironmentRepoErr     error
	finalizeTaskEnvironmentErr       error
	createTaskSessionFunc            func(ctx context.Context, session *models.TaskSession) error
	// listTaskRepositoriesFunc, when non-nil, overrides ListTaskRepositories
	// entirely — used to simulate a transient attachment-set read failure.
	listTaskRepositoriesFunc func(ctx context.Context, taskID string) ([]*models.TaskRepository, error)
	// getTaskSessionByTaskAndAgentFunc, when non-nil, overrides
	// GetTaskSessionByTaskAndAgent entirely — used to simulate a transient
	// lookup failure (e.g. the AC-003.7 re-read-after-conflict arm in
	// createOfficeSessionWithBoundedRecovery), which the default map lookup
	// can never produce on its own.
	getTaskSessionByTaskAndAgentFunc      func(ctx context.Context, taskID, agentInstanceID string) (*models.TaskSession, error)
	updateTaskSessionStateFunc            func(ctx context.Context, sessionID string, state models.TaskSessionState, errorMessage string) error
	updateTaskSessionWorkspaceBindingFunc func(
		ctx context.Context,
		session *models.TaskSession,
		expected models.TaskSessionState,
		attemptID string,
	) (bool, time.Time, error)
	updateTaskSessionResumeStateIfCurrentAttemptFunc func(
		ctx context.Context,
		taskID, sessionID, attemptID string,
		expected, next models.TaskSessionState,
		errorMessage string,
		updateState bool,
		restoreCredentialSnapshot, credentialSnapshotPresent bool,
		credentialSnapshot interface{},
	) (bool, time.Time, error)
	listActiveTaskSessionsByTaskIDFunc func(ctx context.Context, taskID string) ([]*models.TaskSession, error)
	// listTaskSessionsFunc, when non-nil, overrides ListTaskSessions
	// entirely — used to simulate a transient sibling-session read failure
	// (the session-coresidency observation's fail-closed skip path).
	listTaskSessionsFunc                              func(ctx context.Context, taskID string) ([]*models.TaskSession, error)
	listExecutorsRunningByTaskIDFunc                  func(ctx context.Context, taskID string) ([]*models.ExecutorRunning, error)
	repairWorkspaceInventoryFunc                      func(ctx context.Context, repair *models.WorkspaceInventoryRepair) (*models.WorkspaceInventoryRecoveryReceipt, error)
	getWorkspaceInventoryRepairReceiptFunc            func(ctx context.Context, taskID, idempotencyKey string) (*models.WorkspaceInventoryRecoveryReceipt, error)
	getWorkspaceInventoryRepairReceiptForRowFunc      func(ctx context.Context, taskID, environmentRepoID string) (*models.WorkspaceInventoryRecoveryReceipt, error)
	recordWorkspaceInventoryPostRepairAttestationFunc func(ctx context.Context, taskID, idempotencyKey string, evidence *models.WorkspaceInventoryPreservation, matched bool, verifiedAt time.Time) error
	// workspaceInventoryReceipts is the default in-memory store used by
	// RepairWorkspaceInventory/GetWorkspaceInventoryRepairReceipt/
	// RecordWorkspaceInventoryPostRepairAttestation when a test does not
	// override the corresponding *Func hook, so tests can exercise the real
	// executor-level idempotency short-circuit and attestation wiring
	// end-to-end against the mock alone.
	workspaceInventoryReceipts map[string]*models.WorkspaceInventoryRecoveryReceipt
	inventoryClaims            map[string]*models.TaskEnvironmentRecoveryClaim
	// Optional hook invoked at the top of UpdateTaskStateIfCurrentIn, before
	// it reads task state/archived_at. Lets tests simulate the exact TOCTOU
	// window this CAS closes: an earlier (non-transactional) archived-state
	// guard already passed, then an archive commits in the gap before this
	// call — the hook mutates the task here to model that gap. If nil, the
	// CAS runs immediately against the task as currently seeded.
	preCASHook func(taskID string)
	// updateTaskStateIfNotArchivedCh, when non-nil, receives a non-blocking
	// signal every time UpdateTaskStateIfNotArchived records a call — lets
	// tests wait via select instead of polling with time.Sleep for a
	// background goroutine's call to land. Buffered so a signal is never
	// lost even if the test hasn't started waiting yet.
	updateTaskStateIfNotArchivedCh chan struct{}

	// Track calls for verification
	createTaskSessionCalls                 []*models.TaskSession
	updateTaskSessionCalls                 []*models.TaskSession
	updateTaskSessionSnapshots             []*models.TaskSession
	workspaceBindingWrites                 []workspaceBindingWrite
	updateTaskSessionIfCurrentCalls        int
	updateTaskSessionIfCurrentFailOn       int
	updateTaskSessionIfCurrentFailErr      error
	updateTaskSessionStateIfCurrentCalls   int
	updateTaskSessionStateIfCurrentFailOn  int
	updateTaskSessionStateIfCurrentFailErr error
	setSessionMetadataKeyCalls             []setSessionMetadataKeyCall
	setSessionPrimaryCalls                 []string
	createTaskEnvironmentCalls             []*models.TaskEnvironment
	sharedWorkspaceBindingCalls            []sharedWorkspaceBindingCall
	updateTaskEnvironmentCalls             []*models.TaskEnvironment
	finalizeTaskEnvironmentCalls           []*models.TaskEnvironment
	updateTaskStateIfCurrentInCalls        []updateTaskStateIfCurrentInCall
	updateTaskStateIfNotArchivedCalls      []updateTaskStateIfNotArchivedCall

	// writeCallLog records the relative order of environment-row and
	// environment-status writes (e.g. "create_repo", "update_env") so tests
	// can pin ordering invariants that a call-count assertion alone cannot
	// catch — see TestPersistTaskEnvironment_NonMaterializerSiblingPersistsReposBeforeReady.
	writeCallLog []string
}

type sharedWorkspaceBindingCall struct {
	Session *models.TaskSession
	GroupID string
}

type workspaceBindingWrite struct {
	SessionID         string
	TaskEnvironmentID string
	WorkspacePath     string
	ExpectedState     models.TaskSessionState
	AttemptID         string
}

// updateTaskStateIfCurrentInCall records one UpdateTaskStateIfCurrentIn
// invocation so tests can assert callers route guarded REVIEW writes
// through the archive-aware CAS instead of the unconditional
// UpdateTaskState.
type updateTaskStateIfCurrentInCall struct {
	TaskID  string
	State   v1.TaskState
	Allowed []v1.TaskState
}

// updateTaskStateIfNotArchivedCall records one UpdateTaskStateIfNotArchived
// invocation — the IN_PROGRESS/FAILED-writer analog of
// updateTaskStateIfCurrentInCall, used to assert those callers route
// through the archive-aware CAS instead of the unconditional UpdateTaskState.
type updateTaskStateIfNotArchivedCall struct {
	TaskID string
	State  v1.TaskState
}

type setSessionMetadataKeyCall struct {
	SessionID string
	Key       string
	Value     interface{}
}

func newMockRepository() *mockRepository {
	return &mockRepository{
		sessions:                       make(map[string]*models.TaskSession),
		tasks:                          make(map[string]*models.Task),
		taskRepositories:               make(map[string]*models.TaskRepository),
		repositories:                   make(map[string]*models.Repository),
		executors:                      make(map[string]*models.Executor),
		executorProfiles:               make(map[string]*models.ExecutorProfile),
		executorsRunning:               make(map[string]*models.ExecutorRunning),
		taskEnvironments:               make(map[string]*models.TaskEnvironment),
		taskEnvironmentRepos:           make(map[string][]*models.TaskEnvironmentRepo),
		workspaceInventoryReceipts:     make(map[string]*models.WorkspaceInventoryRecoveryReceipt),
		plans:                          make(map[string]*models.TaskPlan),
		updateTaskStateIfNotArchivedCh: make(chan struct{}, 8),
	}
}

// Implement required repository methods

func (m *mockRepository) GetPrimaryTaskRepository(ctx context.Context, taskID string) (*models.TaskRepository, error) {
	// Delegate to ListTaskRepositories, mirroring the sqlite implementation
	// (GetPrimaryTaskRepository there is a thin wrapper over
	// ListTaskRepositories()[0]) so the mock's primary selection agrees with
	// its own ordering instead of raw map iteration order.
	repos, err := m.ListTaskRepositories(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return nil, nil
	}
	return repos[0], nil
}

func (m *mockRepository) GetRepository(ctx context.Context, id string) (*models.Repository, error) {
	if repo, ok := m.repositories[id]; ok {
		return repo, nil
	}
	return nil, nil
}

func (m *mockRepository) CreateTaskSession(ctx context.Context, session *models.TaskSession) error {
	m.mu.Lock()
	m.createTaskSessionCalls = append(m.createTaskSessionCalls, session)
	fn := m.createTaskSessionFunc
	if fn == nil {
		m.sessions[session.ID] = session
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	return fn(ctx, session)
}

func (m *mockRepository) CreateTaskSessionWithSharedGroupWorkspaceBinding(ctx context.Context, session *models.TaskSession, environment *models.TaskEnvironment, groupID string) error {
	m.mu.Lock()
	m.sharedWorkspaceBindingCalls = append(m.sharedWorkspaceBindingCalls, sharedWorkspaceBindingCall{Session: session, GroupID: groupID})
	if environment.ID == "" {
		environment.ID = "environment-" + session.ID
	}
	session.TaskEnvironmentID = environment.ID
	m.taskEnvironments[environment.ID] = environment
	m.mu.Unlock()
	return m.CreateTaskSession(ctx, session)
}

func (m *mockRepository) GetTaskSession(ctx context.Context, id string) (*models.TaskSession, error) {
	m.mu.Lock()
	fn := m.getTaskSessionFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, id)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if session, ok := m.sessions[id]; ok {
		// SQLite scans each query into a fresh value. Match that ownership
		// boundary so concurrent executor paths cannot share mutable rows.
		return cloneMockTaskSession(session), nil
	}
	return nil, nil
}

func (m *mockRepository) UpdateTaskSession(ctx context.Context, session *models.TaskSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateTaskSessionCalls = append(m.updateTaskSessionCalls, session)
	m.sessions[session.ID] = cloneMockTaskSession(session)
	return nil
}

func (m *mockRepository) UpdateTaskSessionIfCurrentState(
	_ context.Context,
	session *models.TaskSession,
	expected models.TaskSessionState,
) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.sessions[session.ID]
	if !ok {
		return false, models.ErrTaskSessionNotFound
	}
	if current.State != expected {
		return false, nil
	}
	m.updateTaskSessionIfCurrentCalls++
	if m.updateTaskSessionIfCurrentFailOn > 0 &&
		m.updateTaskSessionIfCurrentCalls == m.updateTaskSessionIfCurrentFailOn {
		return false, m.updateTaskSessionIfCurrentFailErr
	}
	m.updateTaskSessionSnapshots = append(m.updateTaskSessionSnapshots, cloneMockTaskSession(session))
	m.updateTaskSessionCalls = append(m.updateTaskSessionCalls, session)
	m.sessions[session.ID] = cloneMockTaskSession(session)
	return true, nil
}

func (m *mockRepository) UpdateTaskSessionIfCurrentStateWithStartAttempt(
	ctx context.Context,
	session *models.TaskSession,
	expected models.TaskSessionState,
	attemptID string,
) (bool, error) {
	if session.Metadata == nil {
		session.Metadata = make(map[string]interface{})
	}
	session.Metadata[models.SessionMetaKeyAgentStartAttemptID] = attemptID
	return m.UpdateTaskSessionIfCurrentState(ctx, session, expected)
}

func (m *mockRepository) UpdateTaskSessionWorkspaceBindingIfCurrentAttempt(
	ctx context.Context,
	session *models.TaskSession,
	expected models.TaskSessionState,
	attemptID string,
) (bool, time.Time, error) {
	m.mu.Lock()
	fn := m.updateTaskSessionWorkspaceBindingFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, session, expected, attemptID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.sessions[session.ID]
	if !ok {
		return false, time.Time{}, nil
	}
	if current.TaskID != session.TaskID || current.State != expected {
		return false, time.Time{}, nil
	}
	if attemptID != "" && models.StringFromAny(current.Metadata[models.SessionMetaKeyAgentStartAttemptID]) != attemptID {
		return false, time.Time{}, nil
	}
	now := time.Now().UTC()
	m.workspaceBindingWrites = append(m.workspaceBindingWrites, workspaceBindingWrite{
		SessionID:         session.ID,
		TaskEnvironmentID: session.TaskEnvironmentID,
		WorkspacePath:     session.WorkspacePath,
		ExpectedState:     expected,
		AttemptID:         attemptID,
	})
	current.TaskEnvironmentID = session.TaskEnvironmentID
	current.WorkspacePath = session.WorkspacePath
	current.UpdatedAt = now
	session.UpdatedAt = now
	return true, now, nil
}

func (m *mockRepository) UpdateTaskSessionResumeStateIfCurrentAttempt(
	ctx context.Context,
	taskID, sessionID, attemptID string,
	expected, next models.TaskSessionState,
	errorMessage string,
	updateState bool,
	restoreCredentialSnapshot, credentialSnapshotPresent bool,
	credentialSnapshot interface{},
) (bool, time.Time, error) {
	m.mu.Lock()
	fn := m.updateTaskSessionResumeStateIfCurrentAttemptFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, taskID, sessionID, attemptID, expected, next, errorMessage, updateState,
			restoreCredentialSnapshot, credentialSnapshotPresent, credentialSnapshot)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.sessions[sessionID]
	if !ok || current == nil || current.TaskID != taskID || current.State != expected ||
		models.StringFromAny(current.Metadata[models.SessionMetaKeyAgentStartAttemptID]) != attemptID {
		return false, time.Time{}, nil
	}
	now := time.Now().UTC()
	if updateState {
		current.State = next
		current.ErrorMessage = errorMessage
		if next == models.TaskSessionStateCompleted || next == models.TaskSessionStateFailed ||
			next == models.TaskSessionStateCancelled {
			current.CompletedAt = &now
		} else {
			current.CompletedAt = nil
		}
	}
	current.UpdatedAt = now
	if restoreCredentialSnapshot {
		if current.Metadata == nil {
			current.Metadata = make(map[string]interface{})
		} else {
			current.Metadata = cloneMockSessionMap(current.Metadata)
		}
		if credentialSnapshotPresent {
			current.Metadata[models.SessionMetaKeyGitCredentialSnapshot] = credentialSnapshot
		} else {
			delete(current.Metadata, models.SessionMetaKeyGitCredentialSnapshot)
		}
	}
	return true, now, nil
}

// UpdateTaskSessionStateIfCurrent mirrors the production narrow-CAS
// semantics: only state/error_message/completed_at/updated_at change, so a
// concurrent update to any other field on the stored session survives.
func (m *mockRepository) UpdateTaskSessionStateIfCurrent(
	_ context.Context,
	id string,
	expected, status models.TaskSessionState,
	errorMessage string,
) (bool, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.sessions[id]
	if !ok {
		return false, time.Time{}, models.ErrTaskSessionNotFound
	}
	if current.State != expected {
		return false, time.Time{}, nil
	}
	m.updateTaskSessionStateIfCurrentCalls++
	if m.updateTaskSessionStateIfCurrentFailOn > 0 &&
		m.updateTaskSessionStateIfCurrentCalls == m.updateTaskSessionStateIfCurrentFailOn {
		return false, time.Time{}, m.updateTaskSessionStateIfCurrentFailErr
	}
	now := time.Now().UTC()
	current.State = status
	current.ErrorMessage = errorMessage
	current.UpdatedAt = now
	if status == models.TaskSessionStateCompleted ||
		status == models.TaskSessionStateFailed ||
		status == models.TaskSessionStateCancelled {
		current.CompletedAt = &now
	} else {
		current.CompletedAt = nil
	}
	return true, now, nil
}

// CommitBootstrapFailureIfCurrentExecution mirrors the production admission
// boundary for executor tests. The fake has no separate executor table, so the
// session's execution identity is the equivalent ownership fence.
func (m *mockRepository) CommitBootstrapFailureIfCurrentExecution(
	_ context.Context,
	taskID, sessionID, agentExecutionID string,
	expectedState models.TaskSessionState,
	expectedStamp string,
	errorValue models.LastAgentError,
) (bool, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[sessionID]
	if !ok || session == nil || (session.TaskID != "" && session.TaskID != taskID) || session.State != expectedState {
		return false, time.Time{}, nil
	}
	if agentExecutionID != "" && session.AgentExecutionID != "" && session.AgentExecutionID != agentExecutionID {
		return false, time.Time{}, nil
	}
	current, hasCurrent := models.LoadLastAgentError(session.Metadata)
	if expectedStamp == "" {
		if hasCurrent {
			return false, time.Time{}, nil
		}
	} else if !hasCurrent || current.Stamp() != expectedStamp {
		return false, time.Time{}, nil
	}
	if session.Metadata == nil {
		session.Metadata = make(map[string]interface{})
	}
	session.Metadata[models.SessionMetaKeyLastAgentError] = errorValue
	now := time.Now().UTC()
	session.State = models.TaskSessionStateFailed
	session.ErrorMessage = errorValue.Message
	session.CompletedAt = &now
	session.UpdatedAt = now
	return true, now, nil
}

func (m *mockRepository) UpdateTaskSessionIfCurrentStateRemovingMetadataKeys(
	ctx context.Context,
	session *models.TaskSession,
	expected models.TaskSessionState,
	keys []string,
) (bool, error) {
	updated := cloneMockTaskSession(session)
	for _, key := range keys {
		delete(updated.Metadata, key)
	}
	return m.UpdateTaskSessionIfCurrentState(ctx, updated, expected)
}

func cloneMockTaskSession(session *models.TaskSession) *models.TaskSession {
	if session == nil {
		return nil
	}
	clone := *session
	clone.Metadata = cloneMockSessionMap(session.Metadata)
	clone.AgentProfileSnapshot = cloneMockSessionMap(session.AgentProfileSnapshot)
	clone.ExecutorSnapshot = cloneMockSessionMap(session.ExecutorSnapshot)
	clone.EnvironmentSnapshot = cloneMockSessionMap(session.EnvironmentSnapshot)
	clone.RepositorySnapshot = cloneMockSessionMap(session.RepositorySnapshot)
	if session.Worktrees != nil {
		clone.Worktrees = make([]*models.TaskEnvironmentRepo, len(session.Worktrees))
		for i, worktree := range session.Worktrees {
			if worktree == nil {
				continue
			}
			worktreeClone := *worktree
			clone.Worktrees[i] = &worktreeClone
		}
	}
	if session.CompletedAt != nil {
		completedAt := *session.CompletedAt
		clone.CompletedAt = &completedAt
	}
	return &clone
}

func cloneMockSessionMap(values map[string]interface{}) map[string]interface{} {
	if values == nil {
		return nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		panic(fmt.Sprintf("marshal mock task session map: %v", err))
	}
	var clone map[string]interface{}
	if err := json.Unmarshal(encoded, &clone); err != nil {
		panic(fmt.Sprintf("unmarshal mock task session map: %v", err))
	}
	return clone
}

func (m *mockRepository) UpdateTaskSessionAgentProfileSnapshot(
	_ context.Context,
	sessionID string,
	snapshot map[string]interface{},
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	session.AgentProfileSnapshot = snapshot
	return nil
}

func (m *mockRepository) SetSessionPrimary(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setSessionPrimaryCalls = append(m.setSessionPrimaryCalls, sessionID)
	for _, session := range m.sessions {
		session.IsPrimary = session.ID == sessionID
	}
	return nil
}

func (m *mockRepository) UpdateTaskStateIfPrimarySessionState(
	_ context.Context,
	taskID, sessionID string,
	expectedSessionState models.TaskSessionState,
	state v1.TaskState,
) (v1.TaskState, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[taskID]
	session := m.sessions[sessionID]
	if task == nil || session == nil {
		return "", false, nil
	}
	oldState := task.State
	if task.ArchivedAt != nil || session.TaskID != taskID ||
		session.State != expectedSessionState || !session.IsPrimary {
		return oldState, false, nil
	}
	task.State = state
	return oldState, true, nil
}

func (m *mockRepository) UpdateTaskSessionState(ctx context.Context, sessionID string, state models.TaskSessionState, errorMessage string) error {
	m.mu.Lock()
	fn := m.updateTaskSessionStateFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, sessionID, state, errorMessage)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if session, ok := m.sessions[sessionID]; ok {
		session.State = state
		session.ErrorMessage = errorMessage
	}
	return nil
}

func (m *mockRepository) UpdateTaskSessionBaseCommit(ctx context.Context, sessionID string, baseCommitSHA string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session, ok := m.sessions[sessionID]; ok {
		session.BaseCommitSHA = baseCommitSHA
	}
	return nil
}

func (m *mockRepository) GetExecutor(ctx context.Context, id string) (*models.Executor, error) {
	if exec, ok := m.executors[id]; ok {
		return exec, nil
	}
	return nil, nil
}

func (m *mockRepository) UpsertExecutorRunning(ctx context.Context, running *models.ExecutorRunning) error {
	return nil
}

func (m *mockRepository) UpdateTaskState(ctx context.Context, taskID string, state v1.TaskState) error {
	return nil
}

// UpdateTaskStateIfCurrentIn mirrors the real repository's archive-aware CAS
// semantics (task.go's UpdateTaskStateIfCurrentIn): the write only lands when
// the task's current state is in allowed AND the task is not archived.
func (m *mockRepository) UpdateTaskStateIfCurrentIn(
	ctx context.Context, taskID string, state v1.TaskState, allowed []v1.TaskState,
) (v1.TaskState, bool, error) {
	if m.preCASHook != nil {
		m.preCASHook(taskID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateTaskStateIfCurrentInCalls = append(m.updateTaskStateIfCurrentInCalls, updateTaskStateIfCurrentInCall{
		TaskID: taskID, State: state, Allowed: allowed,
	})
	task, ok := m.tasks[taskID]
	if !ok {
		return "", false, fmt.Errorf("task not found: %s", taskID)
	}
	currentState := task.State
	if task.ArchivedAt != nil {
		return currentState, false, nil
	}
	for _, candidate := range allowed {
		if currentState == candidate {
			task.State = state
			return currentState, true, nil
		}
	}
	return currentState, false, nil
}

// UpdateTaskStateIfNotArchived mirrors the real repository's archive-aware
// CAS semantics (task.go's UpdateTaskStateIfNotArchived): unlike
// UpdateTaskStateIfCurrentIn, there is no "allowed" prior-state set — the
// write lands whenever the task is not archived. Reuses preCASHook so tests
// can model the same TOCTOU window (archive commits between an earlier
// non-transactional guard read and this call) for IN_PROGRESS/FAILED writers.
func (m *mockRepository) UpdateTaskStateIfNotArchived(
	ctx context.Context, taskID string, state v1.TaskState,
) (v1.TaskState, bool, error) {
	if m.preCASHook != nil {
		m.preCASHook(taskID)
	}
	defer m.notifyUpdateTaskStateIfNotArchived()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateTaskStateIfNotArchivedCalls = append(m.updateTaskStateIfNotArchivedCalls, updateTaskStateIfNotArchivedCall{
		TaskID: taskID, State: state,
	})
	task, ok := m.tasks[taskID]
	if !ok {
		return "", false, fmt.Errorf("task not found: %s", taskID)
	}
	currentState := task.State
	if task.ArchivedAt != nil {
		return currentState, false, nil
	}
	task.State = state
	return currentState, true, nil
}

// notifyUpdateTaskStateIfNotArchived signals updateTaskStateIfNotArchivedCh
// (if the test wired one) that a call just landed. Non-blocking so it never
// stalls the caller when nothing is listening.
func (m *mockRepository) notifyUpdateTaskStateIfNotArchived() {
	if m.updateTaskStateIfNotArchivedCh == nil {
		return
	}
	select {
	case m.updateTaskStateIfNotArchivedCh <- struct{}{}:
	default:
	}
}
func (m *mockRepository) ArchiveTask(ctx context.Context, id string) error { return nil }
func (m *mockRepository) ListTasksForAutoArchive(ctx context.Context) ([]*models.Task, error) {
	return nil, nil
}
func (m *mockRepository) ListArchivedTasksWithActiveSessions(ctx context.Context) ([]string, error) {
	return nil, nil
}
func (m *mockRepository) ListUnarchivedTasksWithActiveSessions(ctx context.Context) ([]*models.Task, error) {
	return nil, nil
}

func (m *mockRepository) GetWorkspace(ctx context.Context, id string) (*models.Workspace, error) {
	return nil, nil
}

// Stub implementations for additional repository methods

// Workspace operations
func (m *mockRepository) CreateWorkspace(ctx context.Context, workspace *models.Workspace) error {
	return nil
}
func (m *mockRepository) UpdateWorkspaceFields(_ context.Context, _ string, _ models.WorkspaceFieldUpdate, _ *time.Time) (*models.Workspace, error) {
	return nil, nil
}
func (m *mockRepository) UpdateWorkspace(ctx context.Context, workspace *models.Workspace) error {
	return nil
}
func (m *mockRepository) DeleteWorkspace(ctx context.Context, id string) error { return nil }
func (m *mockRepository) DeleteWorkspaceCascadeWithName(ctx context.Context, id, name string) ([]*models.Task, []*models.Workflow, error) {
	return nil, nil, m.DeleteWorkspace(ctx, id)
}
func (m *mockRepository) ListWorkspaces(ctx context.Context) ([]*models.Workspace, error) {
	return nil, nil
}

// Task operations
func (m *mockRepository) CreateTask(ctx context.Context, task *models.Task) error { return nil }
func (m *mockRepository) GetTask(ctx context.Context, id string) (*models.Task, error) {
	if m.getTaskFunc != nil {
		return m.getTaskFunc(ctx, id)
	}
	if task, ok := m.tasks[id]; ok {
		return task, nil
	}
	return nil, nil
}
func (m *mockRepository) GetTasksByIDs(ctx context.Context, ids []string) ([]*models.Task, error) {
	var out []*models.Task
	for _, id := range ids {
		if task, ok := m.tasks[id]; ok {
			out = append(out, task)
		}
	}
	return out, nil
}
func (m *mockRepository) UpdateTask(ctx context.Context, task *models.Task) error { return nil }

func (m *mockRepository) UpdateTaskFieldsWithParentAdmission(context.Context, string, models.TaskFieldUpdate, repository.TaskParentValidator) (*models.TaskFieldUpdateResult, error) {
	return nil, fmt.Errorf("field updates are not supported by this test repository")
}
func (m *mockRepository) UpdateTaskWithExplicitPosition(ctx context.Context, task *models.Task) error {
	return nil
}
func (m *mockRepository) UpdateTaskPreservingDeferredLaunch(ctx context.Context, task *models.Task) error {
	return nil
}
func (m *mockRepository) DeleteTask(ctx context.Context, id string) error { return nil }
func (m *mockRepository) ListTasks(ctx context.Context, workflowID string) ([]*models.Task, error) {
	return nil, nil
}
func (m *mockRepository) ListTasksByWorkspace(ctx context.Context, workspaceID, workflowID, repositoryID, query string, page, pageSize int, sort string, includeArchived, includeEphemeral, onlyEphemeral, excludeConfig bool) ([]*models.Task, int, error) {
	return nil, 0, nil
}
func (m *mockRepository) ListTasksByWorkflowStep(ctx context.Context, workflowStepID string) ([]*models.Task, error) {
	return nil, nil
}
func (m *mockRepository) AddTaskToWorkflow(ctx context.Context, taskID, workflowID, workflowStepID string, position int) error {
	return nil
}
func (m *mockRepository) RemoveTaskFromWorkflow(ctx context.Context, taskID, workflowID string) error {
	return nil
}

// TaskRepository operations
func (m *mockRepository) ReplaceTaskRepositories(context.Context, string, func(models.TaskRepositoryReplacementSnapshot) ([]*models.TaskRepository, error)) ([]*models.TaskRepository, error) {
	return nil, fmt.Errorf("complete repository replacement is unsupported by this executor fixture")
}

func (m *mockRepository) CreateTaskRepository(ctx context.Context, taskRepo *models.TaskRepository) error {
	return nil
}
func (m *mockRepository) GetTaskRepository(ctx context.Context, id string) (*models.TaskRepository, error) {
	return nil, nil
}
func (m *mockRepository) ListTaskRepositories(ctx context.Context, taskID string) ([]*models.TaskRepository, error) {
	if m.listTaskRepositoriesFunc != nil {
		return m.listTaskRepositoriesFunc(ctx, taskID)
	}
	var out []*models.TaskRepository
	for _, tr := range m.taskRepositories {
		if tr.TaskID == taskID {
			out = append(out, tr)
		}
	}
	// Order by Position, then CreatedAt, then ID, mirroring the production
	// ORDER BY position ASC, created_at ASC, id ASC: map iteration order is
	// random, so ties left unbroken would make tests flaky rather than
	// deterministic.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
func (m *mockRepository) ListTaskWorkspaceFolders(context.Context, string) ([]*models.TaskWorkspaceFolder, error) {
	return nil, nil
}
func (m *mockRepository) ListTaskRepositoriesByTaskIDs(_ context.Context, _ []string) (map[string][]*models.TaskRepository, error) {
	return make(map[string][]*models.TaskRepository), nil
}
func (m *mockRepository) UpdateTaskRepository(ctx context.Context, taskRepo *models.TaskRepository) error {
	return nil
}
func (m *mockRepository) DeleteTaskRepository(ctx context.Context, id string) error { return nil }
func (m *mockRepository) DeleteTaskRepositoriesByTask(ctx context.Context, taskID string) error {
	return nil
}

// Workflow operations
