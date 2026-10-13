package orchestrator

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/agent/runtime/lifecycle"
	"github.com/kandev/kandev/internal/agentctl/types/streams"
	"github.com/kandev/kandev/internal/orchestrator/watcher"
)

// The automatic successor of a session's own failed dynamic attempt replaces
// that attempt's process. By the time it is admitted the failed attempt has
// usually left the counted population, so an ordinary admission at a full
// ceiling used to refuse it and strand the route in "starting".
func TestReplacementOfAFailedAttemptIsHandedOffAtAFullCeiling(t *testing.T) {
	lister := &fakeAdmittedLister{}
	controller := newTestController(t, 2, lister)
	lister.set("other-a", "other-b")
	ctx := context.Background()

	ordinary := controller.handOffOrAdmit(ctx, admissionRequest{
		taskID: "task-ordinary", sessionID: "session-ordinary", origin: launchOriginAutomatic,
		seam: "relaunchDynamicTaskAfterFailure",
	})
	if ordinary.admitted {
		t.Fatal("an ordinary automatic relaunch of an uncounted session was admitted at a full ceiling")
	}

	replacement := controller.handOffOrAdmit(ctx, admissionRequest{
		taskID: "task-replacement", sessionID: "session-replacement", origin: launchOriginAutomatic,
		seam: "relaunchDynamicTaskAfterFailure", replacesFailedAttempt: true,
	})
	if !replacement.admitted || !replacement.handedOff || replacement.reasonCode != ceilingReasonReplacementHandOff {
		t.Fatalf("replacement decision = %+v, want a hand-off", replacement)
	}
	if replacement.reservationKey != "session-replacement" {
		t.Fatalf("reservation key = %q, want the session itself", replacement.reservationKey)
	}
	again := controller.handOffOrAdmit(ctx, admissionRequest{
		taskID: "task-replacement", sessionID: "session-replacement", origin: launchOriginAutomatic,
		seam: "relaunchDynamicTaskAfterFailure", replacesFailedAttempt: true,
	})
	if !again.admitted || mustPopulation(t, controller) != 3 {
		t.Fatalf("a repeated replacement admission = %+v population %d, want one unit for the session", again, mustPopulation(t, controller))
	}
}

// A manual-origin or a route-action relaunch is not marked as a replacement,
// so the existing deferral contract (AC-38a/AC-42f) still applies to it.
func TestReplacementMarkIsOnlyHonoredForAutomaticRelaunches(t *testing.T) {
	if replacesFailedDynamicAttempt(context.Background()) {
		t.Fatal("an unmarked context reads as a replacement")
	}
	if !replacesFailedDynamicAttempt(withReplacesFailedDynamicAttempt(context.Background())) {
		t.Fatal("the replacement mark did not survive the context")
	}
}

// The successor of a failed attempt goes through relaunchDynamicTaskAfterFailure
// with the replacement mark and is admitted where the same call without it is
// deferred.
func TestRelaunchDynamicTaskAfterFailure_ReplacementIsAdmittedOverCeiling(t *testing.T) {
	svcA, _ := newSeam5TestService(t, "seam5-replace-task-a", "seam5-replace-session-a", "seam5-replace-exec-a")
	svcA.sessionCeiling = newSessionCeilingController(1, nil, nil)
	ctx := context.Background()
	if !svcA.relaunchDynamicTaskAfterFailure(ctx, seam5EventData("seam5-replace-task-a", "seam5-replace-session-a", "seam5-replace-exec-a"), "profile-1", launchOriginAutomatic) {
		t.Fatal("first automatic relaunch was not admitted")
	}
	svcB, _ := newSeam5TestService(t, "seam5-replace-task-b", "seam5-replace-session-b", "seam5-replace-exec-b")
	svcB.sessionCeiling = svcA.sessionCeiling
	outcome := svcB.relaunchDynamicTaskAfterFailureOutcome(withReplacesFailedDynamicAttempt(ctx),
		seam5EventData("seam5-replace-task-b", "seam5-replace-session-b", "seam5-replace-exec-b"), "profile-1", launchOriginAutomatic)
	if outcome != dynamicRelaunchSucceeded {
		t.Fatalf("replacement relaunch outcome = %v, want succeeded", outcome)
	}
	if record := deferredLaunchOf(t, svcB, "seam5-replace-task-b"); record != nil {
		t.Fatalf("a replacement must not write a ceiling deferral: %+v", record)
	}
}

func TestCompletedTurnWithoutProviderError(t *testing.T) {
	frame := func(data map[string]any, errText string, providerError *streams.ProviderError) *lifecycle.AgentStreamEventPayload {
		return &lifecycle.AgentStreamEventPayload{Data: &lifecycle.AgentStreamEventData{
			Type: agentEventComplete, Data: data, Error: errText, ProviderError: providerError,
		}}
	}
	cases := []struct {
		name    string
		payload *lifecycle.AgentStreamEventPayload
		want    bool
	}{
		{"end turn", frame(map[string]any{"stop_reason": "end_turn"}, "", nil), true},
		{"no stop reason", frame(nil, "", nil), true},
		{"flagged error", frame(map[string]any{"is_error": true}, "", nil), false},
		{"error text", frame(nil, "AI_APICallError: Upstream request failed: Endpoint is unavailable", nil), false},
		{"provider diagnostic", frame(nil, "", &streams.ProviderError{Message: "rate limited"}), false},
		{"cancelled", frame(map[string]any{"stop_reason": stopReasonCancelled}, "", nil), false},
	}
	for _, tc := range cases {
		if got := completedTurnWithoutProviderError(tc.payload); got != tc.want {
			t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A dynamic attempt is never served by the same-model continuation, so its
// recovery card names that instead of reporting missing evidence.
func TestContinuationRefusalNamesADynamicAttempt(t *testing.T) {
	svc := &Service{}
	reason := svc.continuationRefusalReason(context.Background(), watcher.AgentEventData{
		SessionID: "session-dynamic", DynamicRouteAttempt: true,
	})
	if reason != continuationRefusalDynamicRoute {
		t.Fatalf("refusal reason = %q, want %q", reason, continuationRefusalDynamicRoute)
	}
}
