package orchestrator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
)

func seedResourceWaitRouteState(t *testing.T, svcRepo interface {
	SaveRouteState(context.Context, dynamicruntime.RouteState) error
}, sessionID string, deadline time.Time) {
	t.Helper()
	raw, err := json.Marshal(dynamicruntime.PolicyState{Deadline: &deadline, ResourceWait: true})
	if err != nil {
		t.Fatalf("marshal policy state: %v", err)
	}
	if err := svcRepo.SaveRouteState(context.Background(), dynamicruntime.RouteState{
		SessionID: sessionID, LogicalProfileID: "dynamic-policy", Generation: 4, ProfileVersion: 1,
		Status: dynamicRouteStatusWaiting, PolicyStateJSON: string(raw), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed resource wait: %v", err)
	}
}

func TestResourceWaitRecoveryRetriesThroughRouteAction(t *testing.T) {
	repo := setupTestRepo(t)
	seedSession(t, repo, "task-resource-wait", "session-resource-wait", "")
	seedResourceWaitRouteState(t, repo, "session-resource-wait", time.Now().Add(-time.Second))

	engine := dynamicruntime.NewEngine(dynamicruntime.WithPersistence(repo), dynamicruntime.WithStateLoader(repo))
	resolver := agentruntime.NewProfileExecutionResolver(&fakeSettingsRepo{}, engine, true)
	var requests []RouteActionRequest
	svc := &Service{logger: testLogger(), repo: repo, profileExecutionResolver: resolver}
	svc.SetRouteActionHandler(func(_ context.Context, request RouteActionRequest) (*RouteActionResult, error) {
		requests = append(requests, request)
		return &RouteActionResult{SessionID: request.SessionID, State: "starting"}, nil
	})
	// Stands in for the recovery context of an earlier start whose timer fires.
	recoveryCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc.dynamicRecoveryCtx = recoveryCtx
	svc.dynamicRecoveryTimers = make(map[string]*time.Timer)
	svc.runDynamicPolicyRecovery(recoveryCtx, "session-resource-wait", 4)

	if len(requests) != 1 || requests[0].Action != RouteActionRetry || requests[0].ExpectedGeneration != 4 {
		t.Fatalf("route action requests = %#v, want one retry at generation 4", requests)
	}
}

func TestResourceWaitRecoveryIgnoresManualWait(t *testing.T) {
	repo := setupTestRepo(t)
	seedSession(t, repo, "task-manual-wait", "session-manual-wait", "")
	if err := repo.SaveRouteState(context.Background(), dynamicruntime.RouteState{
		SessionID: "session-manual-wait", LogicalProfileID: "dynamic-policy", Generation: 2,
		Status: dynamicRouteStatusWaiting, UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed manual wait: %v", err)
	}
	engine := dynamicruntime.NewEngine(dynamicruntime.WithPersistence(repo), dynamicruntime.WithStateLoader(repo))
	resolver := agentruntime.NewProfileExecutionResolver(&fakeSettingsRepo{}, engine, true)
	called := false
	svc := &Service{logger: testLogger(), repo: repo, profileExecutionResolver: resolver}
	svc.SetRouteActionHandler(func(context.Context, RouteActionRequest) (*RouteActionResult, error) {
		called = true
		return nil, nil
	})
	recoveryCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc.dynamicRecoveryCtx = recoveryCtx
	svc.dynamicRecoveryTimers = make(map[string]*time.Timer)
	svc.runDynamicPolicyRecovery(recoveryCtx, "session-manual-wait", 2)
	if called {
		t.Fatal("a wait without a resource deadline must stay a manual decision")
	}
}

func TestStartDynamicPolicyRecoveryArmsResourceWait(t *testing.T) {
	repo := setupTestRepo(t)
	seedSession(t, repo, "task-resource-arm", "session-resource-arm", "")
	seedResourceWaitRouteState(t, repo, "session-resource-arm", time.Now().Add(time.Hour))
	engine := dynamicruntime.NewEngine(dynamicruntime.WithPersistence(repo), dynamicruntime.WithStateLoader(repo))
	resolver := agentruntime.NewProfileExecutionResolver(&fakeSettingsRepo{}, engine, true)
	svc := &Service{logger: testLogger(), repo: repo, profileExecutionResolver: resolver}

	svc.startDynamicPolicyRecovery(context.Background())
	t.Cleanup(svc.stopDynamicPolicyRecovery)

	svc.dynamicRecoveryMu.Lock()
	_, armed := svc.dynamicRecoveryTimers["session-resource-arm"]
	svc.dynamicRecoveryMu.Unlock()
	if !armed {
		t.Fatal("a persisted resource wait must re-arm its timer after restart")
	}
}
