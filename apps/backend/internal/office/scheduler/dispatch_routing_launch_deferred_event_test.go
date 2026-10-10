package scheduler_test

// RR19-F3 follow-up: REQ-OFFICE-BACKPRESSURE-003 and
// REQ-OFFICE-LAUNCH-SAFETY-003 both require a durable operator-visible
// record whenever a gate blocks a launch. handleLaunchDeferred in
// service/scheduler_integration.go (the legacy direct-launch fallback)
// already wrote one; its sibling in this file (the routed-dispatch path)
// did not, so a run deferred by the orchestrator's session ceiling was
// silently unrecorded whenever routing was enabled. This test pins that
// both park call sites now write the same durable record.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/office/routing"
	"github.com/kandev/kandev/internal/office/scheduler"
	"github.com/kandev/kandev/internal/office/service"
)

func TestDispatchWithRouting_LaunchDeferredByCapacity_RecordsDurableRunEvent(t *testing.T) {
	repo := newTestRepoSched(t)
	seedRoutingConfig(t, repo, []routing.ProviderID{"claude-acp"})
	starter := newFakeTaskStarter()
	starter.failFor["claude-acp"] = service.ErrLaunchDeferredByCapacity
	ss := buildScheduler(t, repo, starter)

	run := seedRun(t, repo, `{"task_id":"t-deferred-1"}`)

	launched, parked, err := ss.DispatchWithRouting(context.Background(), run, makeAgent(), scheduler.LaunchContext{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if launched || !parked {
		t.Fatalf("launched=%v parked=%v, want launched=false parked=true", launched, parked)
	}

	events, err := repo.ListRunEvents(context.Background(), run.ID, -1, 0)
	if err != nil {
		t.Fatalf("list run events: %v", err)
	}
	var found bool
	for _, e := range events {
		if string(e.EventType) != "adapter.invoke" {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(e.Payload), &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload["phase"] == "deferred" && payload["reason"] == "session_ceiling" {
			found = true
		}
	}
	if !found {
		t.Fatalf("events = %+v, want a durable adapter.invoke event with phase=deferred reason=session_ceiling", events)
	}
}

// TestDispatch_LaunchDeferredByCapacity_ParksWaitingForSessionCapacity pins
// the park status of a session-ceiling deferral: waiting_for_session_capacity
// with an automatic retry deadline, never blocked_provider_action_required
// (which carries no deadline and is only cleared by an operator).
func TestDispatch_LaunchDeferredByCapacity_ParksWaitingForSessionCapacity(t *testing.T) {
	repo := newTestRepoSched(t)
	seedRoutingConfig(t, repo, []routing.ProviderID{"claude-acp"})
	starter := newFakeTaskStarter()
	starter.failFor["claude-acp"] = service.ErrLaunchDeferredByCapacity
	ss := buildScheduler(t, repo, starter)

	run := seedRun(t, repo, `{"task_id":"t-deferred-status"}`)

	launched, parked, err := ss.DispatchWithRouting(context.Background(), run, makeAgent(), scheduler.LaunchContext{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if launched || !parked {
		t.Fatalf("launched=%v parked=%v, want launched=false parked=true", launched, parked)
	}

	got, err := repo.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.RoutingBlockedStatus == nil || string(*got.RoutingBlockedStatus) != routing.StatusWaitingForSessionCapacity {
		t.Fatalf("routing block = %v, want %s", got.RoutingBlockedStatus, routing.StatusWaitingForSessionCapacity)
	}
	if got.EarliestRetryAt == nil {
		t.Fatalf("earliest_retry_at = nil, want an automatic retry deadline")
	}
	retryIn := time.Until(*got.EarliestRetryAt)
	if retryIn < routing.SessionCeilingRetryDelay-5*time.Second || retryIn > routing.SessionCeilingRetryDelay+5*time.Second {
		t.Fatalf("earliest_retry_at is %v away, want ~%v", retryIn, routing.SessionCeilingRetryDelay)
	}
}

// TestDispatch_CeilingDeferral_LiftsAndRelaunches pins the automatic recovery
// loop: once a session-ceiling deferral's retry deadline passes, the scheduler
// wake-up lifts it and the next dispatch launches it without operator action.
func TestDispatch_CeilingDeferral_LiftsAndRelaunches(t *testing.T) {
	repo := newTestRepoSched(t)
	seedRoutingConfig(t, repo, []routing.ProviderID{"claude-acp"})
	starter := newFakeTaskStarter()
	starter.failFor["claude-acp"] = service.ErrLaunchDeferredByCapacity
	ss := buildScheduler(t, repo, starter)
	run := seedRun(t, repo, `{"task_id":"t-deferred-lift"}`)

	launched, parked, err := ss.DispatchWithRouting(context.Background(), run, makeAgent(), scheduler.LaunchContext{})
	if err != nil || launched || !parked {
		t.Fatalf("first dispatch launched=%v parked=%v err=%v, want parked", launched, parked, err)
	}

	lifted, err := ss.LiftParkedRuns(context.Background(), time.Now().UTC().Add(time.Minute))
	if err != nil || lifted != 1 {
		t.Fatalf("lifted=%d err=%v, want one lifted run", lifted, err)
	}
	fresh, err := repo.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get lifted run: %v", err)
	}
	if fresh.RoutingBlockedStatus != nil {
		t.Fatalf("lifted run still parked: %v", fresh.RoutingBlockedStatus)
	}

	// A freed slot: a scheduler whose starter admits the launch.
	relaunchStarter := newFakeTaskStarter()
	ss2 := buildScheduler(t, repo, relaunchStarter)
	launched, parked, err = ss2.DispatchWithRouting(context.Background(), fresh, makeAgent(), scheduler.LaunchContext{})
	if err != nil {
		t.Fatalf("relaunch dispatch: %v", err)
	}
	if !launched || parked {
		t.Fatalf("after lift launched=%v parked=%v, want launched=true parked=false", launched, parked)
	}
	if relaunchStarter.callCount() != 1 {
		t.Fatalf("relaunch starter calls = %d, want 1", relaunchStarter.callCount())
	}
}
