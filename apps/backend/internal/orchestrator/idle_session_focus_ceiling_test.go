package orchestrator

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// A focus-driven idle-suspension resume is passive inspection, not an explicit
// execution request, so it must be admitted against the session ceiling. With
// the ceiling saturated, focusing a parked session must not launch a second
// agent; a manual-origin resume would have bypassed the ceiling and resurrected
// every parked session after a restart.
func TestFocusTaskSessionIdleSuspensionResumeRespectsCeiling(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "task-focus-ceiling", "session-focus-ceiling", models.TaskSessionStateWaitingForInput)
	session, err := repo.GetTaskSession(ctx, "session-focus-ceiling")
	if err != nil {
		t.Fatal(err)
	}
	session.AgentProfileID = claudeACPProviderID
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := repo.UpsertExecutorRunning(ctx, &models.ExecutorRunning{
		ID: "session-focus-ceiling", SessionID: "session-focus-ceiling", TaskID: "task-focus-ceiling",
		AgentExecutionID: "execution-focus-ceiling", Status: models.ExecutorRunningStatusStopped,
		IdleSuspensionState: models.ExecutorIdleSuspensionSuspended,
		ExecutorID:          "executor-local", Resumable: true, ResumeToken: "same-conversation-token",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert idle-suspended runtime: %v", err)
	}

	var launchCalls atomic.Int32
	agentMgr := &mockAgentManager{
		repoForExecutionLookup: repo,
		isAgentReadyFn:         func(context.Context, string) bool { return true },
		launchAgentFunc: func(_ context.Context, req *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
			// Fail the launch so a resume that bypasses the ceiling returns
			// promptly instead of waiting for an agent that never becomes ready.
			launchCalls.Add(1)
			return nil, errors.New("launch must not run while the ceiling is saturated")
		},
	}
	taskRepo := newMockTaskRepo()
	taskRepo.tasks["task-focus-ceiling"] = &v1.Task{ID: "task-focus-ceiling", State: v1.TaskStateInProgress}
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), taskRepo, agentMgr)
	svc.executor = executor.NewExecutor(agentMgr, repo, testLogger(), executor.ExecutorConfig{})
	svc.turnService = &inactiveTurnService{}
	svc.SetLSPLeaseLifecycle(&activeLSPLeaseForTest{sessionID: "session-focus-ceiling"})

	// Fill the only session slot with an unrelated in-flight launch.
	svc.sessionCeiling = newSessionCeilingController(1, nil, nil)
	if decision := svc.sessionCeiling.admit(ctx, admissionRequest{
		taskID: "task-occupier", sessionID: "session-occupier",
		origin: launchOriginAutomatic, seam: "test",
	}); !decision.admitted {
		t.Fatalf("occupier admission = %+v, want admitted", decision)
	}

	execution, _, resumed, err := svc.focusTaskSession(ctx, "task-focus-ceiling", "session-focus-ceiling")
	if n := launchCalls.Load(); n != 0 {
		t.Fatalf("saturated focus launched %d agents, want 0", n)
	}
	if err != nil {
		t.Fatalf("focusTaskSession: %v", err)
	}
	if resumed || execution != nil {
		t.Fatalf("saturated focus resumed: execution=%v resumed=%v, want none", execution != nil, resumed)
	}
}
