package service

import "testing"

// A completion may only consume the parked failure whose run it names. Events
// without a run identity (the legacy task+agent fallback) and events naming a
// different run must leave the parked failure in place.
func TestDropPendingAgentFailureRequiresMatchingRunIdentity(t *testing.T) {
	svc := &Service{}
	svc.agentErrorVerdict.pending.Store("session-1", &pendingAgentFailure{runID: "run-new"})

	svc.dropPendingAgentFailure("session-1", "")
	if _, ok := svc.agentErrorVerdict.pending.Load("session-1"); !ok {
		t.Fatal("a completion without run identity must not drop a parked failure")
	}

	svc.dropPendingAgentFailure("session-1", "run-old")
	if _, ok := svc.agentErrorVerdict.pending.Load("session-1"); !ok {
		t.Fatal("a completion for another run must not drop a parked failure")
	}

	svc.dropPendingAgentFailure("session-1", "run-new")
	if _, ok := svc.agentErrorVerdict.pending.Load("session-1"); ok {
		t.Fatal("the completing run must consume its parked failure")
	}
}
