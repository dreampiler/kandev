package executor

import (
	"context"
	"fmt"
	"testing"

	"github.com/kandev/kandev/internal/agent/agents"
	"github.com/kandev/kandev/internal/agent/runtime/agentctl"
	"github.com/kandev/kandev/internal/agent/runtime/lifecycle"
	"github.com/kandev/kandev/internal/agentctl/types/streams"
	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// mockAgentManager implements AgentManagerClient for testing
type mockAgentManager struct {
	launchAgentFunc                    func(ctx context.Context, req *LaunchAgentRequest) (*LaunchAgentResponse, error)
	startAgentProcessFunc              func(ctx context.Context, agentExecutionID string) error
	startupDisposition                 lifecycle.AgentStartupDisposition
	stopAgentFunc                      func(ctx context.Context, agentExecutionID string, force bool) error
	stopAgentWithReasonFunc            func(ctx context.Context, agentExecutionID string, reason string, force bool) error
	resolveAgentProfileFunc            func(ctx context.Context, profileID string) (*AgentProfileInfo, error)
	setExecutionDescriptionFunc        func(ctx context.Context, agentExecutionID string, description string) error
	setInitialDeliverySubmissionIDFunc func(ctx context.Context, agentExecutionID, submissionID string) error
	setExecutionEnvFunc                func(ctx context.Context, agentExecutionID string, env map[string]string) error
	executorProfileEnvFunc             func(ctx context.Context, sessionID, taskEnvironmentID string) (map[string]string, error)
	getExecutionIDForSessionFunc       func(ctx context.Context, sessionID string) (string, error)
	listExecutionsForTaskFunc          func(taskID string) []lifecycle.ExecutionReference
	isAgentCommandConfiguredFunc       func(agentExecutionID string) bool
	isAgentRunningForSessionFunc       func(ctx context.Context, sessionID string) bool
	cleanupStaleExecutionFunc          func(ctx context.Context, sessionID string) error
	promptAgentFunc                    func(ctx context.Context, agentExecutionID, prompt string, attachments []v1.MessageAttachment, dispatchOnly bool) (*PromptResult, error)
	isPassthroughSessionFunc           func(ctx context.Context, sessionID string) bool
	writePassthroughStdinFunc          func(ctx context.Context, sessionID, data string) error
	markPassthroughRunningFunc         func(sessionID string) error
	resolvePassthroughConfigFunc       func(ctx context.Context, sessionID string) (agents.PassthroughConfig, error)
	launchAgentCallCount               int
	cleanupStaleExecutionCallCount     int
	isAgentRunningForSessionCallArgs   []string
	promptAgentCallCount               int
	writePassthroughStdinCalls         []passthroughStdinCall
	markPassthroughRunningCalls        []string
}

// passthroughStdinCall captures one invocation of WritePassthroughStdin for assertions.
type passthroughStdinCall struct {
	SessionID string
	Data      string
}

func (m *mockAgentManager) LaunchAgent(ctx context.Context, req *LaunchAgentRequest) (*LaunchAgentResponse, error) {
	m.launchAgentCallCount++
	if m.launchAgentFunc != nil {
		return m.launchAgentFunc(ctx, req)
	}
	return &LaunchAgentResponse{
		AgentExecutionID: "exec-123",
		ContainerID:      "container-123",
		Status:           v1.AgentStatusStarting,
	}, nil
}

func (m *mockAgentManager) SetExecutionDescription(ctx context.Context, agentExecutionID string, description string) error {
	if m.setExecutionDescriptionFunc != nil {
		return m.setExecutionDescriptionFunc(ctx, agentExecutionID, description)
	}
	return nil
}

func (m *mockAgentManager) SetInitialDeliverySubmissionID(ctx context.Context, agentExecutionID, submissionID string) error {
	if m.setInitialDeliverySubmissionIDFunc != nil {
		return m.setInitialDeliverySubmissionIDFunc(ctx, agentExecutionID, submissionID)
	}
	return nil
}
func (m *mockAgentManager) SetExecutionEnv(ctx context.Context, executionID string, env map[string]string) error {
	if m.setExecutionEnvFunc != nil {
		return m.setExecutionEnvFunc(ctx, executionID, env)
	}
	return nil
}

func (m *mockAgentManager) ExecutorProfileEnvForSession(ctx context.Context, sessionID, taskEnvironmentID string) (map[string]string, error) {
	if m.executorProfileEnvFunc != nil {
		return m.executorProfileEnvFunc(ctx, sessionID, taskEnvironmentID)
	}
	return nil, nil
}

func (m *mockAgentManager) SetMcpMode(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *mockAgentManager) StartAgentProcess(ctx context.Context, agentExecutionID string) error {
	if m.startAgentProcessFunc != nil {
		return m.startAgentProcessFunc(ctx, agentExecutionID)
	}
	return nil
}

func (m *mockAgentManager) StartupDisposition(string) lifecycle.AgentStartupDisposition {
	if m.startupDisposition == "" {
		return lifecycle.AgentStartupCreatedByAttempt
	}
	return m.startupDisposition
}

func (m *mockAgentManager) IsAgentCommandConfigured(agentExecutionID string) bool {
	if m.isAgentCommandConfiguredFunc != nil {
		return m.isAgentCommandConfiguredFunc(agentExecutionID)
	}
	return true
}

func (m *mockAgentManager) StopAgent(ctx context.Context, agentExecutionID string, force bool) error {
	if m.stopAgentFunc != nil {
		return m.stopAgentFunc(ctx, agentExecutionID, force)
	}
	return nil
}

func (m *mockAgentManager) StopAgentWithReason(ctx context.Context, agentExecutionID string, reason string, force bool) error {
	if m.stopAgentWithReasonFunc != nil {
		return m.stopAgentWithReasonFunc(ctx, agentExecutionID, reason, force)
	}
	return m.StopAgent(ctx, agentExecutionID, force)
}

func (m *mockAgentManager) PromptAgent(ctx context.Context, agentExecutionID string, prompt string, attachments []v1.MessageAttachment, dispatchOnly bool) (*PromptResult, error) {
	m.promptAgentCallCount++
	if m.promptAgentFunc != nil {
		return m.promptAgentFunc(ctx, agentExecutionID, prompt, attachments, dispatchOnly)
	}
	return nil, nil
}

func (m *mockAgentManager) CancelAgent(ctx context.Context, sessionID string) error {
	return nil
}

func (m *mockAgentManager) RespondToPermissionBySessionID(ctx context.Context, sessionID, pendingID, optionID string, cancelled bool) error {
	return nil
}

func (m *mockAgentManager) ListPendingPermissionsBySessionID(context.Context, string) ([]streams.PendingAgentPermission, error) {
	return nil, nil
}

func (m *mockAgentManager) ResolvePermissionBySessionID(context.Context, string, string, string, string) (*streams.PermissionResolveResponse, error) {
	return nil, nil
}

func (m *mockAgentManager) CancelPermissionBySessionID(context.Context, string, string, string) (*streams.PermissionCancelResponse, error) {
	return nil, nil
}

func (m *mockAgentManager) ProbeBackgroundWorkloads(ctx context.Context, sessionID string) (client.ProbeResult, error) {
	return client.ProbeResultUnknown, nil
}

func (m *mockAgentManager) RestartAgentProcess(ctx context.Context, agentExecutionID string) error {
	return nil
}
func (m *mockAgentManager) ResetAgentContext(ctx context.Context, agentExecutionID string) error {
	return nil
}

func (m *mockAgentManager) SetSessionModelBySessionID(ctx context.Context, sessionID, modelID string) error {
	return fmt.Errorf("not supported")
}

func (m *mockAgentManager) SetSessionModeBySessionID(ctx context.Context, sessionID, modeID string) error {
	return fmt.Errorf("not supported")
}

func (m *mockAgentManager) IsAgentRunningForSession(ctx context.Context, sessionID string) bool {
	m.isAgentRunningForSessionCallArgs = append(m.isAgentRunningForSessionCallArgs, sessionID)
	if m.isAgentRunningForSessionFunc != nil {
		return m.isAgentRunningForSessionFunc(ctx, sessionID)
	}
	return false
}

func (m *mockAgentManager) IsAgentReadyForPrompt(ctx context.Context, sessionID string) bool {
	return m.IsAgentRunningForSession(ctx, sessionID)
}

func (m *mockAgentManager) WasSessionInitialized(_ string) bool { return false }
func (m *mockAgentManager) GetSessionAuthMethods(_ string) []streams.AuthMethodInfo {
	return nil
}
func (m *mockAgentManager) IsPassthroughSession(ctx context.Context, sessionID string) bool {
	if m.isPassthroughSessionFunc != nil {
		return m.isPassthroughSessionFunc(ctx, sessionID)
	}
	return false
}
func (m *mockAgentManager) WritePassthroughStdin(ctx context.Context, sessionID string, data string) error {
	m.writePassthroughStdinCalls = append(m.writePassthroughStdinCalls, passthroughStdinCall{
		SessionID: sessionID,
		Data:      data,
	})
	if m.writePassthroughStdinFunc != nil {
		return m.writePassthroughStdinFunc(ctx, sessionID, data)
	}
	return nil
}
func (m *mockAgentManager) ResolvePassthroughConfig(ctx context.Context, sessionID string) (agents.PassthroughConfig, error) {
	if m.resolvePassthroughConfigFunc != nil {
		return m.resolvePassthroughConfigFunc(ctx, sessionID)
	}
	// Default to a sensible passthrough config (SubmitSequence "\r") when the
	// session is in passthrough mode, so most tests don't need to override.
	if m.isPassthroughSessionFunc != nil && m.isPassthroughSessionFunc(ctx, sessionID) {
		return agents.PassthroughConfig{Supported: true, SubmitSequence: "\r"}, nil
	}
	return agents.PassthroughConfig{}, nil
}
func (m *mockAgentManager) MarkPassthroughRunning(sessionID string) error {
	m.markPassthroughRunningCalls = append(m.markPassthroughRunningCalls, sessionID)
	if m.markPassthroughRunningFunc != nil {
		return m.markPassthroughRunningFunc(sessionID)
	}
	return nil
}

func (m *mockAgentManager) GetRemoteRuntimeStatusBySession(ctx context.Context, sessionID string) (*RemoteRuntimeStatus, error) {
	return nil, nil
}
func (m *mockAgentManager) PollRemoteStatusForRecords(ctx context.Context, records []RemoteStatusPollRequest) {
}
func (m *mockAgentManager) CleanupStaleExecutionBySessionID(ctx context.Context, sessionID string) error {
	m.cleanupStaleExecutionCallCount++
	if m.cleanupStaleExecutionFunc != nil {
		return m.cleanupStaleExecutionFunc(ctx, sessionID)
	}
	return nil
}
func (m *mockAgentManager) EnsureWorkspaceExecutionForSession(ctx context.Context, taskID, sessionID string) error {
	return nil
}
func (m *mockAgentManager) GetExecutionIDForSession(ctx context.Context, sessionID string) (string, error) {
	if m.getExecutionIDForSessionFunc != nil {
		return m.getExecutionIDForSessionFunc(ctx, sessionID)
	}
	return "", fmt.Errorf("no execution found for session %s", sessionID)
}
func (m *mockAgentManager) ListExecutionsForTask(taskID string) []lifecycle.ExecutionReference {
	if m.listExecutionsForTaskFunc != nil {
		return m.listExecutionsForTaskFunc(taskID)
	}
	return nil
}

func (m *mockAgentManager) ResolveAgentProfile(ctx context.Context, profileID string) (*AgentProfileInfo, error) {
	if m.resolveAgentProfileFunc != nil {
		return m.resolveAgentProfileFunc(ctx, profileID)
	}
	return &AgentProfileInfo{
		ProfileID:   profileID,
		ProfileName: "Test Profile",
		AgentID:     "agent-123",
		AgentName:   "Test Agent",
		Model:       "claude-3-opus",
	}, nil
}

func (m *mockAgentManager) GetGitLog(ctx context.Context, sessionID, baseCommit string, limit int, targetBranch string) (*client.GitLogResult, error) {
	return nil, nil
}

func (m *mockAgentManager) GetCumulativeDiff(ctx context.Context, sessionID, baseCommit string) (*client.CumulativeDiffResult, error) {
	return nil, nil
}

func (m *mockAgentManager) GetGitStatus(ctx context.Context, sessionID string) (*client.GitStatusResult, error) {
	// Return a mock git status with a head commit for base commit capture
	return &client.GitStatusResult{
		Success:    true,
		Branch:     "main",
		HeadCommit: "abc123def456",
	}, nil
}

func (m *mockAgentManager) GetGitStatusFresh(ctx context.Context, sessionID string) (*client.GitStatusResult, error) {
	return m.GetGitStatus(ctx, sessionID)
}

func (m *mockAgentManager) WaitForAgentctlReady(ctx context.Context, sessionID string) error {
	// Mock returns immediately
	return nil
}

// mockShellPrefs implements ShellPreferenceProvider
type mockShellPrefs struct{}

func (m *mockShellPrefs) PreferredShell(ctx context.Context) (string, error) {
	return "/bin/bash", nil
}

// mockCapabilities implements ExecutorTypeCapabilities for testing.
type mockCapabilities struct{}

func (m *mockCapabilities) RequiresCloneURL(executorType string) bool {
	switch models.ExecutorType(executorType) {
	case models.ExecutorTypeLocalDocker, models.ExecutorTypeRemoteDocker, models.ExecutorTypeSprites:
		return true
	default:
		return false
	}
}

func (m *mockCapabilities) ShouldApplyPreferredShell(executorType string) bool {
	switch models.ExecutorType(executorType) {
	case models.ExecutorTypeLocal, models.ExecutorTypeWorktree, models.ExecutorTypeMockRemote:
		return true
	default:
		return false
	}
}

// Helper to create a test executor
func newTestExecutor(t *testing.T, agentManager AgentManagerClient, repo executorStore) *Executor {
	t.Helper()
	log, err := logger.NewLogger(logger.LoggingConfig{Level: "error", Format: "json"})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	exec := NewExecutor(agentManager, repo, log, ExecutorConfig{
		ShellPrefs: &mockShellPrefs{},
	})
	exec.SetCapabilities(&mockCapabilities{})
	return exec
}
