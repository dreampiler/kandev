package orchestrator

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	commonlogger "github.com/kandev/kandev/internal/common/logger"
)

// A session recovered after a backend restart has no in-memory prompt activity.
// The reaper must still anchor its idle interval to the session's durable
// semantic activity instead of skipping the candidate, so a pre-restart idle
// window is not lost.
func TestIdleParkingRecoveredSessionUsesDurableActivity(t *testing.T) {
	svc, agent, _ := newIdleParkingFixture(t, true, 1, "opencode")
	agent.currentPromptLastActivityAt = time.Time{}
	agent.currentPromptActivityEpoch.Store(0)

	svc.suspendWorkspaceIdleSessionsOnce(context.Background())

	if got := len(agent.suspensionCalls()); got != 1 {
		t.Fatalf("suspension calls = %d, want 1 for a recovered idle session", got)
	}
}

// A recovered session whose durable session activity is within the configured
// timeout is not due yet, so it must not be suspended. The anchor is the
// durable activity, not a fresh restart time.
func TestIdleParkingRecoveredSessionNotDueWithinTimeout(t *testing.T) {
	svc, agent, _ := newIdleParkingFixture(t, true, 100000, "opencode")
	agent.currentPromptLastActivityAt = time.Time{}
	agent.currentPromptActivityEpoch.Store(0)

	svc.suspendWorkspaceIdleSessionsOnce(context.Background())

	if got := len(agent.suspensionCalls()); got != 0 {
		t.Fatalf("suspension calls = %d, want 0 before the idle timeout elapses", got)
	}
}

// A future activity timestamp is invalid and must fail closed rather than
// making a candidate immediately eligible.
func TestIdleParkingFutureActivityFailsClosed(t *testing.T) {
	svc, agent, _ := newIdleParkingFixture(t, true, 1, "opencode")
	agent.currentPromptLastActivityAt = time.Now().UTC().Add(time.Hour)

	svc.suspendWorkspaceIdleSessionsOnce(context.Background())

	if got := len(agent.suspensionCalls()); got != 0 {
		t.Fatalf("suspension calls = %d, want 0 for an invalid future activity time", got)
	}
}

// The scan summary must identify the bounded skip reasons at Info so an
// operator can see why a settled live session was not parked, without a
// per-session log line.
func TestIdleParkingScanLogsSkipReasonsAtInfo(t *testing.T) {
	svc, _, _ := newIdleParkingFixture(t, true, 1, "opencode")
	core, observed := observer.New(zap.InfoLevel)
	log, err := commonlogger.NewFromZap(zap.New(core))
	if err != nil {
		t.Fatalf("create observer logger: %v", err)
	}
	svc.logger = log
	svc.registerBackgroundTask("session-idle-parking", "background-work")

	svc.suspendWorkspaceIdleSessionsOnce(context.Background())

	entries := observed.FilterMessage("workspace ACP idle suspension scan skipped candidates").All()
	if len(entries) != 1 {
		t.Fatalf("skip summary entries = %d, want 1", len(entries))
	}
	if entries[0].Level != zap.InfoLevel {
		t.Fatalf("skip summary level = %s, want info", entries[0].Level)
	}
	if got := entries[0].ContextMap()["skip_known_work"]; got != int64(1) {
		t.Fatalf("skip_known_work = %v, want 1; fields=%v", got, entries[0].ContextMap())
	}
}
