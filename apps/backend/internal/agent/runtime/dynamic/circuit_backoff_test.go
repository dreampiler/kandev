package dynamic

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

type backoffClock struct{ now time.Time }

func (c *backoffClock) Now() time.Time { return c.now }

func newBackoffRegistry(clock *backoffClock, escalation bool, persistence CircuitPersistence) *CircuitRegistry {
	options := []CircuitRegistryOption{WithCircuitClock(clock.Now), WithCircuitBackoffEscalation(escalation)}
	if persistence != nil {
		options = append(options, WithCircuitPersistence(persistence))
	}
	return NewCircuitRegistry(options...)
}

func circuitUntil(t *testing.T, registry *CircuitRegistry, key string) time.Time {
	t.Helper()
	registry.mu.Lock()
	defer registry.mu.Unlock()
	entry, ok := registry.circuits[key]
	if !ok {
		t.Fatalf("circuit %s missing", key)
	}
	return entry.until
}

func rateLimitedOpen(now time.Time) FailureOpen {
	return FailureOpen{Code: routingerr.CodeRateLimited, Now: now}
}

// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.20
func TestCircuitBackoffWithoutEscalationKeepsTheFixedMinute(t *testing.T) {
	clock := &backoffClock{now: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}
	persistence := newStubCircuitPersistence()
	registry := newBackoffRegistry(clock, false, persistence)
	for attempt := 1; attempt <= 4; attempt++ {
		registry.OpenFailure("binding", rateLimitedOpen(clock.now))
		if got, want := circuitUntil(t, registry, "binding"), clock.now.Add(time.Minute); !got.Equal(want) {
			t.Fatalf("open %d until = %v, want %v", attempt, got, want)
		}
		clock.now = clock.now.Add(time.Hour)
	}
	lease, ok := registry.AcquireProbe("binding", time.Second)
	if !ok {
		t.Fatal("AcquireProbe failed after expiry")
	}
	registry.ReleaseProbe(lease, false, circuitBackoff)
	if got, want := circuitUntil(t, registry, "binding"), clock.now.Add(time.Minute); !got.Equal(want) {
		t.Fatalf("failed probe until = %v, want %v", got, want)
	}
	if got := registry.Failures("binding"); got != 0 {
		t.Fatalf("failures = %d, want 0 while escalation is disabled", got)
	}
	if row := persistence.rows["binding"]; row.Failures != 0 {
		t.Fatalf("persisted failures = %d, want 0", row.Failures)
	}
}

// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.20
func TestCircuitBackoffWithoutEscalationIgnoresPersistedCount(t *testing.T) {
	clock := &backoffClock{now: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}
	persistence := newStubCircuitPersistence()
	persistence.rows["binding"] = CircuitSnapshot{
		Key: "binding", State: CircuitOpen, Until: clock.now.Add(-time.Minute),
		Code: routingerr.CodeRateLimited, Failures: 5,
	}
	registry := newBackoffRegistry(clock, false, persistence)
	if err := registry.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := registry.Failures("binding"); got != 0 {
		t.Fatalf("restored failures = %d, want 0 while escalation is disabled", got)
	}
	registry.OpenFailure("binding", rateLimitedOpen(clock.now))
	if got, want := circuitUntil(t, registry, "binding"), clock.now.Add(time.Minute); !got.Equal(want) {
		t.Fatalf("until = %v, want %v", got, want)
	}
}

// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.20
func TestCircuitBackoffEscalatesAcrossRepeatedOpens(t *testing.T) {
	clock := &backoffClock{now: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}
	registry := newBackoffRegistry(clock, true, nil)
	want := []time.Duration{
		time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute,
		time.Hour, 2 * time.Hour, 4 * time.Hour, 4 * time.Hour,
	}
	for index, backoff := range want {
		registry.OpenFailure("binding", rateLimitedOpen(clock.now))
		if got := circuitUntil(t, registry, "binding"); !got.Equal(clock.now.Add(backoff)) {
			t.Fatalf("open %d until = %v, want %v after the failure", index+1, got.Sub(clock.now), backoff)
		}
		if got := registry.Failures("binding"); got != index+1 {
			t.Fatalf("open %d failures = %d, want %d", index+1, got, index+1)
		}
		clock.now = clock.now.Add(backoff + time.Second)
	}
}

// Concurrent failures inside one open window are one incident.
//
// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.20
func TestCircuitBackoffCountsOneIncidentOnce(t *testing.T) {
	clock := &backoffClock{now: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}
	registry := newBackoffRegistry(clock, true, nil)
	registry.OpenFailure("binding", rateLimitedOpen(clock.now))
	first := circuitUntil(t, registry, "binding")
	clock.now = clock.now.Add(10 * time.Second)
	registry.OpenFailure("binding", rateLimitedOpen(clock.now))
	if got := registry.Failures("binding"); got != 1 {
		t.Fatalf("failures = %d, want 1 for a failure inside the open window", got)
	}
	if got := circuitUntil(t, registry, "binding"); got.Before(first) {
		t.Fatalf("until moved earlier: %v before %v", got, first)
	}
}

// A failed probe advances the count. A successful probe closes the circuit
// but keeps the count, so the next failure continues the ladder; only a clean
// turn completion resets it.
//
// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.20
func TestCircuitBackoffProbeOutcomesAndCleanTurnReset(t *testing.T) {
	clock := &backoffClock{now: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}
	registry := newBackoffRegistry(clock, true, nil)
	registry.OpenFailure("binding", rateLimitedOpen(clock.now))
	clock.now = clock.now.Add(2 * time.Minute)

	lease, ok := registry.AcquireProbe("binding", time.Second)
	if !ok {
		t.Fatal("AcquireProbe failed after expiry")
	}
	registry.ReleaseProbe(lease, false, circuitBackoff)
	if got := registry.Failures("binding"); got != 2 {
		t.Fatalf("failures after a failed probe = %d, want 2", got)
	}
	if got, want := circuitUntil(t, registry, "binding"), clock.now.Add(5*time.Minute); !got.Equal(want) {
		t.Fatalf("failed probe until = %v, want %v", got, want)
	}

	clock.now = clock.now.Add(6 * time.Minute)
	lease, ok = registry.AcquireProbe("binding", time.Second)
	if !ok {
		t.Fatal("AcquireProbe failed after the second expiry")
	}
	registry.ReleaseProbe(lease, true, circuitBackoff)
	if registry.IsOpen("binding", clock.now) {
		t.Fatal("a successful probe left the circuit open")
	}
	if got := registry.Failures("binding"); got != 2 {
		t.Fatalf("failures after a successful probe = %d, want 2 (kept)", got)
	}
	registry.OpenFailure("binding", rateLimitedOpen(clock.now))
	if got, want := circuitUntil(t, registry, "binding"), clock.now.Add(15*time.Minute); !got.Equal(want) {
		t.Fatalf("failure after a successful probe until = %v, want %v", got, want)
	}

	if !registry.ResetFailures("binding") || registry.Failures("binding") != 0 {
		t.Fatal("a clean turn did not reset the failure count")
	}
	clock.now = clock.now.Add(time.Hour)
	registry.OpenFailure("binding", rateLimitedOpen(clock.now))
	if got, want := circuitUntil(t, registry, "binding"), clock.now.Add(time.Minute); !got.Equal(want) {
		t.Fatalf("first failure after a clean turn until = %v, want %v", got, want)
	}
}

// Disabled, a clean turn has nothing to reset and a successful probe keeps the
// fixed behavior.
//
// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.20
func TestCircuitBackoffResetFailuresWithoutEscalationIsANoOp(t *testing.T) {
	clock := &backoffClock{now: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}
	persistence := newStubCircuitPersistence()
	registry := newBackoffRegistry(clock, false, persistence)
	registry.OpenFailure("binding", rateLimitedOpen(clock.now))
	saves := persistence.saves
	if registry.ResetFailures("binding") || persistence.saves != saves {
		t.Fatal("ResetFailures acted while escalation is disabled")
	}
	if registry.EscalationEnabled() {
		t.Fatal("EscalationEnabled = true, want false")
	}
}

// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.20
func TestCircuitBackoffResetHints(t *testing.T) {
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		hint     time.Duration
		inferred bool
		want     time.Duration
	}{
		{name: "earlier hint keeps the ladder", hint: 30 * time.Second, want: time.Minute},
		{name: "later explicit hint wins", hint: 3 * time.Hour, want: 3 * time.Hour},
		{name: "explicit hint beyond a day wins", hint: 72 * time.Hour, want: 72 * time.Hour},
		{name: "inferred hint beyond a day is capped", hint: 30 * time.Hour, inferred: true, want: 24 * time.Hour},
		{name: "inferred hint within a day wins", hint: 6 * time.Hour, inferred: true, want: 6 * time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := &backoffClock{now: start}
			registry := newBackoffRegistry(clock, true, nil)
			hint := start.Add(tc.hint)
			registry.OpenFailure("binding", FailureOpen{
				Code: routingerr.CodeQuotaLimited, Now: start, ResetHint: &hint, ResetHintInferred: tc.inferred,
			})
			if got := circuitUntil(t, registry, "binding"); !got.Equal(start.Add(tc.want)) {
				t.Fatalf("until = %v after the failure, want %v", got.Sub(start), tc.want)
			}
		})
	}
}

// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.21
func TestCircuitBackoffCountPersistsAndRestores(t *testing.T) {
	clock := &backoffClock{now: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}
	persistence := newStubCircuitPersistence()
	registry := newBackoffRegistry(clock, true, persistence)
	registry.OpenFailure("binding", rateLimitedOpen(clock.now))
	clock.now = clock.now.Add(time.Hour)
	registry.OpenFailure("binding", rateLimitedOpen(clock.now))
	row := persistence.rows["binding"]
	if row.Failures != 2 || row.Until.Location() != time.UTC {
		t.Fatalf("persisted row = %#v, want 2 failures with a UTC deadline", row)
	}

	restored := newBackoffRegistry(clock, true, persistence)
	if err := restored.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := restored.Failures("binding"); got != 2 {
		t.Fatalf("restored failures = %d, want 2", got)
	}
	clock.now = clock.now.Add(time.Hour)
	restored.OpenFailure("binding", rateLimitedOpen(clock.now))
	if got, want := circuitUntil(t, restored, "binding"), clock.now.Add(15*time.Minute); !got.Equal(want) {
		t.Fatalf("until after restore = %v, want %v", got, want)
	}
}

// An owner-release hold is never replaced by a timed open.
//
// @covers AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.20
func TestCircuitBackoffNeverReplacesAHold(t *testing.T) {
	clock := &backoffClock{now: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}
	registry := newBackoffRegistry(clock, true, nil)
	if _, err := registry.Hold("binding", routingerr.CodeAccountBlocked); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	registry.OpenFailure("binding", rateLimitedOpen(clock.now))
	if !registry.IsHeld("binding") || registry.Failures("binding") != 0 {
		t.Fatal("a timed open replaced the owner-release hold")
	}
}
