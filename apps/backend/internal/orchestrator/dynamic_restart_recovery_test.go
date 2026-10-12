package orchestrator

import (
	"context"
	"errors"
	"testing"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// A resumed or prepared runtime settles to WAITING_FOR_INPUT before the
// process-start callback runs. The process did start for this execution, so
// the re-claimed route must become active instead of staying "starting".
//
// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.12
func TestHandleAgentProcessStartedActivatesRouteWaitingForInput(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-dynamic-resumed-waiting"
		sessionID   = "session-dynamic-resumed-waiting"
		executionID = "execution-dynamic-resumed-waiting"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateWaitingForInput)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, taskID, v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, &mockAgentManager{})
	engine := dynamicruntime.NewEngine(dynamicruntime.WithPersistence(repo))
	svc.SetProfileExecutionResolver(agentruntime.NewProfileExecutionResolver(nil, engine, true))
	decision := seedClaimedDynamicRoute(t, ctx, repo, engine, sessionID, executionID)

	svc.handleAgentProcessStarted(ctx, taskID, sessionID, executionID)

	state, err := repo.LoadRouteState(ctx, sessionID)
	if err != nil {
		t.Fatalf("LoadRouteState: %v", err)
	}
	if state == nil || state.Generation != decision.Generation || state.Status != dynamicRouteStatusActive {
		t.Fatalf("route state after resumed process start = %#v, want active generation %d", state, decision.Generation)
	}
}

// The resume-ready path completes the same transition when the runtime is
// already prepared and no process-start callback follows.
//
// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.12
func TestActivateResumedDynamicRouteMarksClaimedRouteActive(t *testing.T) {
	ctx := context.Background()
	const (
		taskID      = "task-dynamic-resume-ready"
		sessionID   = "session-dynamic-resume-ready"
		executionID = "execution-dynamic-resume-ready"
	)
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, taskID, sessionID, models.TaskSessionStateWaitingForInput)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{})
	engine := dynamicruntime.NewEngine(dynamicruntime.WithPersistence(repo))
	svc.SetProfileExecutionResolver(agentruntime.NewProfileExecutionResolver(nil, engine, true))
	decision := seedClaimedDynamicRoute(t, ctx, repo, engine, sessionID, executionID)
	session, err := repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}

	svc.activateResumedDynamicRoute(ctx, session)

	state, err := repo.LoadRouteState(ctx, sessionID)
	if err != nil {
		t.Fatalf("LoadRouteState: %v", err)
	}
	if state == nil || state.Generation != decision.Generation || state.Status != dynamicRouteStatusActive {
		t.Fatalf("route state after resume ready = %#v, want active generation %d", state, decision.Generation)
	}
}

// An explicit recovery may resume a dynamic session whose route claim was
// left "starting" with no live runtime behind it. A live launch owner, a
// passive resume, and a route in any other state keep the guard.
//
// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.13
func TestExplicitRecoveryReclaimsStrandedStartingRoute(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		routeState string
		recovery   string
		running    bool
		wantGuard  bool
	}{
		{name: "explicit recovery without a live runtime", routeState: "starting", recovery: recoveryActionResume},
		{name: "passive resume", routeState: "starting", wantGuard: true},
		{name: "live launch owner", routeState: "starting", recovery: recoveryActionResume, running: true, wantGuard: true},
		{name: "manual recovery state", routeState: dynamicRouteStatusActionRequired, recovery: recoveryActionResume, wantGuard: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := setupTestRepo(t)
			seedTaskAndSession(t, repo, "task1", "session1", models.TaskSessionStateWaitingForInput)
			session, err := repo.GetTaskSession(ctx, "session1")
			if err != nil {
				t.Fatalf("GetTaskSession: %v", err)
			}
			session.AgentProfileID = "dynamic-logical"
			session.ExecutionProfileID = "candidate-1"
			session.RouteGeneration = 1
			session.RouteState = tt.routeState
			if err := repo.UpdateTaskSession(ctx, session); err != nil {
				t.Fatalf("UpdateTaskSession: %v", err)
			}
			running := tt.running
			agentMgr := &mockAgentManager{isAgentRunningFn: func(context.Context, string) bool { return running }}
			svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), agentMgr)

			_, err = svc.loadResumeSessionCandidate(ctx, "task1", "session1",
				executor.ResumeOptions{RecoveryAction: tt.recovery}, nil)

			var blocked *sessionOpenRecoveryBlockedError
			if guarded := errors.As(err, &blocked) && blocked.reason == autoResumeBlockedDynamicRoute; guarded != tt.wantGuard {
				t.Fatalf("loadResumeSessionCandidate error = %v, want dynamic-route guard %v", err, tt.wantGuard)
			}
		})
	}
}

// An explicit recovery action that cannot resume must fail. Reporting success
// with no runtime lets the caller acknowledge delivery or commit a
// continuation against an execution that does not exist. A passive session
// open keeps its waiting response.
//
// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.13
func TestLaunchResumeRecoveryActionReportsBlockedRouteAsError(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "task1", "session1", models.TaskSessionStateWaitingForInput)
	session, err := repo.GetTaskSession(ctx, "session1")
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	session.AgentProfileID = "dynamic-logical"
	session.RouteState = dynamicRouteStatusActionRequired
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatalf("UpdateTaskSession: %v", err)
	}
	agentMgr := &mockAgentManager{isAgentRunningFn: func(context.Context, string) bool { return false }}
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), agentMgr)
	svc.executor = executor.NewExecutor(agentMgr, repo, testLogger(), executor.ExecutorConfig{})

	response, err := svc.launchResume(ctx, &LaunchSessionRequest{
		TaskID: "task1", SessionID: "session1", RecoveryAction: recoveryActionResume,
	})
	var blocked *sessionOpenRecoveryBlockedError
	if !errors.As(err, &blocked) || response != nil {
		t.Fatalf("explicit recovery = %#v, %v; want the dynamic-route block as an error", response, err)
	}

	passive, err := svc.launchResume(ctx, &LaunchSessionRequest{TaskID: "task1", SessionID: "session1"})
	if err != nil || passive == nil || !passive.Success || passive.ActivationReason != autoResumeBlockedDynamicRoute {
		t.Fatalf("passive resume = %#v, %v; want the existing waiting response", passive, err)
	}
}
