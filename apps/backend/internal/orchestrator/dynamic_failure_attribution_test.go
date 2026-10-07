package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/orchestrator/watcher"
	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// A dynamic route moved from claude to sol, but the sol launch was deferred by
// the session ceiling, so the claude execution kept serving the session. A
// later failure of that claude execution must not suspend sol or advance the
// route past it: sol never ran.
func TestDynamicFailureOfSupersededCandidateDoesNotSuspendCurrentCandidate(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-dynamic-superseded-failure"
		sessionID   = "session-dynamic-superseded-failure"
		executionID = "execution-claude-still-serving"
		dynamicID   = "dynamic-superseded-failure"
		claude      = "candidate-claude"
		sol         = "candidate-sol"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateRunning)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, &mockAgentManager{})
	resolver := newWorkflowDynamicProfileResolverWithCandidates(t, dynamicID, []workflowDynamicCandidate{
		{executionProfileID: claude, enabled: true, rulesJSON: `{"on_provider_error":"try_next"}`},
		{executionProfileID: sol, enabled: true, rulesJSON: `{"on_provider_error":"try_next"}`},
		{executionProfileID: "candidate-nemotron", enabled: true},
	}, dynamicruntime.WithPersistence(repo))
	svc.SetProfileExecutionResolver(resolver)

	initial, err := resolver.Resolve(ctx, sessionID, dynamicID, 0, "")
	if err != nil || initial.ExecutionProfileID != claude {
		t.Fatalf("initial resolve = %#v, %v; want %s", initial, err, claude)
	}
	rateLimited := &routingerr.Error{
		Code: routingerr.CodeRateLimited, Class: routingerr.ClassTransient,
		Confidence: routingerr.ConfHigh, FallbackAllowed: true, AutoRetryable: true,
	}
	moved, err := resolver.ResolveExecutionAfterFailure(ctx, sessionID, dynamicID, claude, initial.Generation, rateLimited)
	if err != nil || moved.ExecutionProfileID != sol {
		t.Fatalf("route after the first claude failure = %#v, %v; want %s", moved, err, sol)
	}
	session, err := repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	session.AgentProfileID = dynamicID
	session.ExecutionProfileID = moved.ExecutionProfileID
	session.RouteGeneration = moved.Generation
	session.RouteState = dynamicRouteStatusActive
	session.AgentExecutionID = executionID
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatalf("project route: %v", err)
	}
	// The next queued prompt still reaches the claude execution.
	svc.beginDynamicAttempt(sessionID)
	svc.bindDynamicAttemptExecution(sessionID, executionID)

	svc.routeDynamicAgentFailure(ctx, watcher.AgentEventData{
		TaskID: taskID, SessionID: sessionID, AgentExecutionID: executionID,
		AgentProfileID: dynamicID, ExecutionProfileID: claude, PromptGeneration: 1,
	}, rateLimited)

	now := time.Now()
	if got, ok := resolver.CandidateSuspension(ctx, sol, now); !ok || got.Blocked() {
		t.Fatalf("sol suspension after a claude failure = %#v (resolved=%v), want available", got, ok)
	}
	if got, ok := resolver.CandidateSuspension(ctx, claude, now); !ok || !got.Blocked() {
		t.Fatalf("claude suspension = %#v (resolved=%v), want the failure kept on claude", got, ok)
	}
	state, err := repo.LoadRouteState(ctx, sessionID)
	if err != nil {
		t.Fatalf("LoadRouteState: %v", err)
	}
	if state == nil || state.Generation != moved.Generation || state.ExecutionProfileID != sol {
		t.Fatalf("route state = %#v, want generation %d still on %s", state, moved.Generation, sol)
	}
}

// Launch failures raised by Kandev itself (workspace preparation, ceiling
// admission, command validation, runtime detection, the local agentctl control
// plane) say nothing about the candidate's provider. They must reach the
// conductor unclassified, so no provider fallback suspends the candidate.
func TestDynamicLaunchFailuresFromKandevAreNotProviderFailures(t *testing.T) {
	for _, launchErr := range []error{
		errors.New("workspace is preparing: retry after the initial workspace launch completes"),
		errors.New("workspace reuse is unsafe: existing task environment is not attachable"),
		errors.New("workspace reuse is unsafe: load task environment while waiting: context deadline exceeded"),
		errors.New("validate agent command: agent command cannot be empty"),
		errors.New("detect native OpenCode runtime: read native OpenCode version: exit status 1"),
		errors.New("detect native OpenCode runtime: read native OpenCode version: exit status 1 (timed out after 10s)"),
		errors.New(`detect native OpenCode runtime: read native OpenCode version: exit status 1; ` +
			`output: "Error: connect ECONNREFUSED 127.0.0.1:4096 rate limit exceeded"`),
		errors.New(`detect native OpenCode runtime: read native OpenCode version: unsupported output; output: "Too Many Requests"`),
		errors.New("session ceiling refused seam 3 admission (ceiling)"),
		errors.New(`failed to create execution: failed to create standalone instance: failed to create instance: ` +
			`Post "http://127.0.0.1:41044/api/v1/instances": dial tcp 127.0.0.1:41044: i/o timeout`),
		errors.New(`failed to create instance: Post "http://127.0.0.1:41044/api/v1/instances": ` +
			`dial tcp 127.0.0.1:41044: connect: connection refused`),
	} {
		t.Run(launchErr.Error(), func(t *testing.T) {
			err := launchDynamicDownstreamWithError(t, launchErr)
			if err == nil {
				t.Fatal("launch error was swallowed")
			}
			var classified *routingerr.Error
			if errors.As(err, &classified) {
				t.Fatalf("Kandev launch failure classified as provider %s (%s): %v", classified.Code, classified.ClassifierRule, err)
			}
		})
	}
}

// The agent process itself can still report a provider failure while it
// starts; that evidence keeps routing the launch.
func TestDynamicLaunchAgentStartupProviderFailureStillRoutes(t *testing.T) {
	startup := routingerr.NewAgentStartupFailure(routingerr.PhaseProcessStart, "claude-acp",
		errors.New("request to api.anthropic.com failed: ECONNREFUSED"))
	err := launchDynamicDownstreamWithError(t, fmt.Errorf("launch agent: %w", startup))
	var classified *routingerr.Error
	if !errors.As(err, &classified) || classified.Code != routingerr.CodeNetworkUnavailable {
		t.Fatalf("startup failure = %v, want a classified network_unavailable failure", err)
	}
}

func launchDynamicDownstreamWithError(t *testing.T, launchErr error) error {
	t.Helper()
	ctx := context.Background()
	const (
		taskID    = "task-dynamic-launch-kandev-failure"
		sessionID = "session-dynamic-launch-kandev-failure"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateStarting)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)
	agentManager := &mockAgentManager{
		launchAgentFunc: func(context.Context, *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
			return nil, launchErr
		},
	}
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, agentManager)
	engine := dynamicruntime.NewEngine(dynamicruntime.WithPersistence(repo))
	svc.SetProfileExecutionResolver(agentruntime.NewProfileExecutionResolver(nil, engine, true))
	decision := seedClaimedDynamicRoute(t, ctx, repo, engine, sessionID, "")

	downstream := &dynamicTaskDownstream{
		service:   svc,
		task:      &v1.Task{ID: taskID, WorkspaceID: "ws1", Description: "test"},
		sessionID: sessionID,
		options:   executor.LaunchOptions{AgentProfileID: "candidate-1", StartAgent: true},
	}
	_, err := downstream.Launch(ctx, dynamicruntime.DownstreamLaunch{
		ExecutionProfileID: "candidate-1",
		Decision:           decision,
	})
	return err
}
