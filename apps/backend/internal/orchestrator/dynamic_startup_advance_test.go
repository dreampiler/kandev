package orchestrator

import (
	"context"
	"errors"
	"testing"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/lifecycle"
	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// projectStartupAdvanceSession claims generation 1 for a two-candidate dynamic
// profile and projects the claim onto the task session.
func projectStartupAdvanceSession(
	t *testing.T,
	ctx context.Context,
	repo *sqliterepo.Repository,
	resolver *agentruntime.ProfileExecutionResolver,
	dynamicID, sessionID, executionID string,
) {
	t.Helper()
	execution, err := resolver.Resolve(ctx, sessionID, dynamicID, 0, "")
	if err != nil {
		t.Fatalf("initial resolve: %v", err)
	}
	session, err := repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	session.AgentProfileID = dynamicID
	session.ExecutionProfileID = execution.ExecutionProfileID
	session.RouteGeneration = execution.Generation
	session.RouteState = execution.Decision.Status
	session.AgentExecutionID = executionID
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatalf("UpdateTaskSession: %v", err)
	}
}

// A low-confidence process-start failure is presented to the conductor as a
// transient provider failure, so the candidate policy advances to the next
// candidate inside the same launch instead of stopping for manual recovery.
func TestStartupLaunchFailureAdvancesToNextCandidateInSameLaunch(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-startup-advance-sync"
		sessionID   = "session-startup-advance-sync"
		executionID = "execution-startup-advance-sync"
		dynamicID   = "dynamic-startup-advance-sync"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateStarting)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)

	var launchedProfiles []string
	agentManager := &mockAgentManager{
		launchAgentFunc: func(_ context.Context, req *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
			launchedProfiles = append(launchedProfiles, req.AgentProfileID)
			if req.AgentProfileID == "candidate-1" {
				return nil, errors.New("The agent could not start.")
			}
			return &executor.LaunchAgentResponse{AgentExecutionID: executionID}, nil
		},
		startAgentProcessFunc: func(context.Context, string) error { return nil },
	}
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, agentManager)
	resolver := newWorkflowDynamicProfileResolverWithCandidates(t, dynamicID, []workflowDynamicCandidate{
		{executionProfileID: "candidate-1", enabled: true, rulesJSON: `{"on_provider_error":"try_next"}`},
		{executionProfileID: "candidate-2", enabled: true},
	}, dynamicruntime.WithPersistence(repo))
	svc.SetProfileExecutionResolver(resolver)
	projectStartupAdvanceSession(t, ctx, repo, resolver, dynamicID, sessionID, executionID)

	task := &v1.Task{ID: taskID, WorkspaceID: "ws1", Description: "startup fallback"}
	if _, err := svc.launchPreparedSessionWithDynamicFallbackWithContinuation(
		ctx, task, sessionID, executor.LaunchOptions{AgentProfileID: dynamicID, StartAgent: true}, nil,
	); err != nil {
		t.Fatalf("dynamic fallback launch: %v", err)
	}
	if len(launchedProfiles) != 2 || launchedProfiles[0] != "candidate-1" || launchedProfiles[1] != "candidate-2" {
		t.Fatalf("launched profiles = %#v, want [candidate-1 candidate-2]", launchedProfiles)
	}
}

// A process-start failure that surfaces after LaunchPreparedSession returned
// routes through the same immediate next-candidate policy.
func TestHandleAgentProcessStartFailedAdvancesToNextCandidate(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-startup-advance-async"
		sessionID   = "session-startup-advance-async"
		executionID = "execution-startup-advance-async"
		dynamicID   = "dynamic-startup-advance-async"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateStarting)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, &mockAgentManager{})
	resolver := newWorkflowDynamicProfileResolverWithCandidates(t, dynamicID, []workflowDynamicCandidate{
		{executionProfileID: "candidate-1", enabled: true, rulesJSON: `{"on_provider_error":"try_next"}`},
		{executionProfileID: "candidate-2", enabled: true},
	}, dynamicruntime.WithPersistence(repo))
	svc.SetProfileExecutionResolver(resolver)
	projectStartupAdvanceSession(t, ctx, repo, resolver, dynamicID, sessionID, executionID)

	svc.beginDynamicAttempt(sessionID)
	svc.bindDynamicAttemptExecution(sessionID, executionID)

	svc.handleAgentProcessStartFailed(ctx, taskID, sessionID, executionID, errors.New("provider process failed"))

	state, err := repo.LoadRouteState(ctx, sessionID)
	if err != nil {
		t.Fatalf("LoadRouteState: %v", err)
	}
	if state == nil || state.Generation != 2 || state.ExecutionProfileID != "candidate-2" {
		t.Fatalf("route state after process failure = %#v, want generation 2 on candidate-2", state)
	}
}

// A never-started stall produces no output, so it advances the route
// immediately rather than recording FAILED and tearing the session down.
func TestHandleAgentStalledNeverStartedAdvancesToNextCandidate(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-startup-advance-stall"
		sessionID   = "session-startup-advance-stall"
		executionID = "execution-startup-advance-stall"
		dynamicID   = "dynamic-startup-advance-stall"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateRunning)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)
	agentManager := &mockAgentManager{
		repoForExecutionLookup:   repo,
		currentPromptExecutionID: executionID,
	}
	agentManager.currentPromptGeneration.Store(1)
	agentManager.currentPromptActivityEpoch.Store(1)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, agentManager)
	svc.turnService = &repoTurnService{repo: repo}
	if _, err := svc.turnService.StartTurn(ctx, sessionID); err != nil {
		t.Fatalf("start active turn: %v", err)
	}
	svc.messageCreator = &mockMessageCreator{}
	resolver := newWorkflowDynamicProfileResolverWithCandidates(t, dynamicID, []workflowDynamicCandidate{
		{executionProfileID: "candidate-1", enabled: true, rulesJSON: `{"on_provider_error":"try_next"}`},
		{executionProfileID: "candidate-2", enabled: true},
	}, dynamicruntime.WithPersistence(repo))
	svc.SetProfileExecutionResolver(resolver)
	projectStartupAdvanceSession(t, ctx, repo, resolver, dynamicID, sessionID, executionID)
	svc.beginDynamicAttempt(sessionID)
	svc.bindDynamicAttemptExecution(sessionID, executionID)
	svc.lastTurnPrompt.Store(sessionID, capturedPrompt{text: "retry the task"})

	svc.handleAgentStalled(ctx, lifecycle.AgentStalledPayload{
		AgentExecutionID: executionID,
		TaskID:           taskID,
		SessionID:        sessionID,
		PromptGeneration: 1,
		ActivityEpoch:    1,
		NeverStarted:     true,
	})

	state, err := repo.LoadRouteState(ctx, sessionID)
	if err != nil {
		t.Fatalf("LoadRouteState: %v", err)
	}
	if state == nil || state.Generation != 2 || state.ExecutionProfileID != "candidate-2" {
		t.Fatalf("route state after never-started stall = %#v, want generation 2 on candidate-2", state)
	}
	session, err := repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	if session.State == models.TaskSessionStateFailed {
		t.Fatal("never-started stall recorded FAILED instead of advancing the route")
	}
}
