package dynamic

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/agent/runtime/routingpolicy"
)

func engineBackoffFixture(clock *backoffClock, escalation bool) (*Engine, *CircuitRegistry, Profile) {
	registry := newBackoffRegistry(clock, escalation, nil)
	engine := NewEngine(WithCircuitRegistry(registry), WithClock(clock.Now))
	profile := Profile{
		ID: "dynamic", Version: 1,
		Candidates: []Candidate{
			{ID: "candidate-a", Enabled: true, BindingKey: "binding-a", Policies: routingpolicy.DefaultDocument()},
			{ID: "candidate-b", Enabled: true, BindingKey: "binding-b", Policies: routingpolicy.DefaultDocument()},
		},
	}
	return engine, registry, profile
}

// midTurnCycle selects candidate-a for a new session (a probe once its
// circuit expired), reports a successful launch, then records a mid-turn
// provider failure. It returns the backoff that failure opened.
func midTurnCycle(t *testing.T, engine *Engine, registry *CircuitRegistry, profile Profile, clock *backoffClock, sessionID string) time.Duration {
	t.Helper()
	decision, err := engine.Select(sessionID, profile, 0, "")
	if err != nil || decision.ExecutionProfileID != "candidate-a" {
		t.Fatalf("select for %s = %#v, %v; want candidate-a", sessionID, decision, err)
	}
	engine.ReleaseProbe(decision, true)
	if _, err := engine.RecordFailureCircuit(context.Background(), sessionID, profile, decision.Generation, "candidate-a", &routingerr.Error{
		Code: routingerr.CodeProviderUnavailable, Class: routingerr.ClassTransient, FallbackAllowed: true,
	}); err != nil {
		t.Fatalf("RecordFailureCircuit: %v", err)
	}
	return circuitUntil(t, registry, "binding-a").Sub(clock.now)
}

// A candidate whose launch succeeds and whose turn then fails mid-turn
// escalates across cycles while the flag is enabled; a clean turn completion
// resets the ladder. Disabled, every cycle uses the fixed minute.
//
// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.20
func TestEngineMidTurnFailuresEscalateAcrossSuccessfulLaunches(t *testing.T) {
	for _, tc := range []struct {
		escalation bool
		want       []time.Duration
	}{
		{escalation: false, want: []time.Duration{time.Minute, time.Minute, time.Minute}},
		{escalation: true, want: []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}},
	} {
		clock := &backoffClock{now: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}
		engine, registry, profile := engineBackoffFixture(clock, tc.escalation)
		sessions := []string{"session-a", "session-b", "session-c"}
		for index, want := range tc.want {
			if got := midTurnCycle(t, engine, registry, profile, clock, sessions[index]); got != want {
				t.Fatalf("escalation=%v cycle %d backoff = %v, want %v", tc.escalation, index+1, got, want)
			}
			clock.now = clock.now.Add(want + time.Second)
		}

		// A later turn on the candidate completes without a provider error.
		clean, err := engine.Select("session-clean", profile, 0, "")
		if err != nil || clean.ExecutionProfileID != "candidate-a" {
			t.Fatalf("clean select = %#v, %v", clean, err)
		}
		engine.ReleaseProbe(clean, true)
		if _, err := engine.ResetCircuitFailures(context.Background(), "session-clean", profile, clean.Generation+1, "candidate-a"); err == nil {
			t.Fatalf("escalation=%v a stale completion was accepted", tc.escalation)
		}
		if tc.escalation && registry.Failures("binding-a") != 3 {
			t.Fatalf("a stale completion changed the failure count to %d", registry.Failures("binding-a"))
		}
		if _, err := engine.ResetCircuitFailures(context.Background(), "session-clean", profile, clean.Generation, "candidate-a"); err != nil {
			t.Fatalf("ResetCircuitFailures: %v", err)
		}
		if got := registry.Failures("binding-a"); got != 0 {
			t.Fatalf("escalation=%v failures after a clean turn = %d, want 0", tc.escalation, got)
		}
		if got := midTurnCycle(t, engine, registry, profile, clock, "session-after-clean"); got != time.Minute {
			t.Fatalf("escalation=%v backoff after a clean turn = %v, want 1m", tc.escalation, got)
		}
	}
}

// A failed probe launch advances the ladder through the engine only while
// escalation is enabled.
//
// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.20
func TestEngineFailedProbeBackoffFollowsTheFlag(t *testing.T) {
	for _, tc := range []struct {
		escalation bool
		probe      time.Duration
	}{
		{escalation: false, probe: time.Minute},
		{escalation: true, probe: 5 * time.Minute},
	} {
		clock := &backoffClock{now: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}
		engine, registry, profile := engineBackoffFixture(clock, tc.escalation)
		midTurnCycle(t, engine, registry, profile, clock, "session-1")
		clock.now = clock.now.Add(time.Minute + time.Second)
		probe, err := engine.Select("session-2", profile, 0, "")
		if err != nil || probe.ExecutionProfileID != "candidate-a" {
			t.Fatalf("escalation=%v probe select = %#v, %v; want the probe on candidate-a", tc.escalation, probe, err)
		}
		engine.ReleaseProbe(probe, false)
		if got := circuitUntil(t, registry, "binding-a"); !got.Equal(clock.now.Add(tc.probe)) {
			t.Fatalf("escalation=%v failed probe backoff = %v, want %v", tc.escalation, got.Sub(clock.now), tc.probe)
		}
	}
}
