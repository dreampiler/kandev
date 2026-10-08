package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// A focus-driven idle-suspension resume is passive inspection, not an explicit
// execution request, so it must be admitted against the session ceiling. With
// the worker lane saturated, focusing a parked session must not launch a second
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

	launchCalls := 0
	agentMgr := &mockAgentManager{
		repoForExecutionLookup: repo,
		isAgentReadyFn:         func(context.Context, string) bool { return true },
		launchAgentFunc: func(_ context.Context, req *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
			launchCalls++
			return &executor.LaunchAgentResponse{AgentExecutionID: "execution-focus-resumed"}, nil
		},
	}
	taskRepo := newMockTaskRepo()
	taskRepo.tasks["task-focus-ceiling"] = &v1.Task{ID: "task-focus-ceiling", State: v1.TaskStateInProgress}
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), taskRepo, agentMgr)
	svc.executor = executor.NewExecutor(agentMgr, repo, testLogger(), executor.ExecutorConfig{})
	svc.turnService = &inactiveTurnService{}
	svc.SetLSPLeaseLifecycle(&activeLSPLeaseForTest{sessionID: "session-focus-ceiling"})

	// Saturate the single worker slot with an unrelated in-flight launch.
	svc.sessionCeiling = newSessionCeilingController(1, nil, nil)
	if decision := svc.sessionCeiling.admit(ctx, admissionRequest{
		taskID: "task-occupier", sessionID: "session-occupier",
		origin: launchOriginAutomatic, seam: "test", agentProfileID: claudeACPProviderID,
	}); !decision.admitted {
		t.Fatalf("occupier admission = %+v, want admitted", decision)
	}

	execution, _, resumed, err := svc.focusTaskSession(ctx, "task-focus-ceiling", "session-focus-ceiling")
	if err != nil {
		t.Fatalf("focusTaskSession: %v", err)
	}
	if resumed || execution != nil {
		t.Fatalf("saturated focus resumed: execution=%v resumed=%v, want none", execution != nil, resumed)
	}
	if launchCalls != 0 {
		t.Fatalf("saturated focus launched %d agents, want 0", launchCalls)
	}
}

// durableActivityRepo satisfies idleDurableActivityReader over the shared repo
// interface so the anchor helper can be exercised without a full repository.
type durableActivityRepo struct {
	repoStore
	times map[string]time.Time
	err   error
}

func (r durableActivityRepo) GetLastMessageTimeBySessionIDs(context.Context, []string) (map[string]time.Time, error) {
	return r.times, r.err
}

func TestDurableIdleActivityAnchorPrefersLastMessage(t *testing.T) {
	fallback := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	messageAt := fallback.Add(-45 * time.Minute)

	svc := &Service{repo: durableActivityRepo{times: map[string]time.Time{"session-1": messageAt}}}
	if got := svc.durableIdleActivityAnchor(context.Background(), "session-1", fallback); !got.Equal(messageAt) {
		t.Fatalf("anchor = %v, want durable last-message time %v", got, messageAt)
	}
}

func TestDurableIdleActivityAnchorFallsBackWithoutActivity(t *testing.T) {
	fallback := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

	cases := map[string]durableActivityRepo{
		"no message for session": {times: map[string]time.Time{}},
		"zero message time":      {times: map[string]time.Time{"session-1": {}}},
		"read error":             {err: errors.New("read failed")},
	}
	for name, repo := range cases {
		t.Run(name, func(t *testing.T) {
			svc := &Service{repo: repo}
			if got := svc.durableIdleActivityAnchor(context.Background(), "session-1", fallback); !got.Equal(fallback) {
				t.Fatalf("anchor = %v, want fallback %v", got, fallback)
			}
		})
	}
}
