package dynamic

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

func TestOpenRouterTryLaterStartsShortAndEscalatesWithinDay(t *testing.T) {
	engine, clock := newFailoverEngine(time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC))
	candidate := failoverCandidate("or-free", "openrouter/vendor/model:free")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{candidate}}
	failure := failureOf(routingerr.CodeRateLimited, routingerr.RuleOpenCodeRateTryLater)
	for i, want := range []time.Duration{5 * time.Minute, 15 * time.Minute, 30 * time.Minute, 30 * time.Minute, 30 * time.Minute, 30 * time.Minute, 30 * time.Minute, 30 * time.Minute} {
		if got := recordAfterExpiry(t, engine, clock, profile, candidate, candidate.pathModelKey(), failure); got != want {
			t.Fatalf("strike %d = %v, want %v", i+1, got, want)
		}
	}
	got := recordAfterExpiry(t, engine, clock, profile, candidate, candidate.pathModelKey(), failure)
	if got != nextUTCDay(clock.now).Sub(clock.now) {
		t.Fatalf("ninth strike = %v, want next UTC reset", got)
	}
	clock.now = nextUTCDay(clock.now).Add(time.Minute)
	if got := recordAfterExpiry(t, engine, clock, profile, candidate, candidate.pathModelKey(), failure); got != 5*time.Minute {
		t.Fatalf("new UTC day = %v, want 5m", got)
	}
}

func TestRestoreCapsLegacyGoQuotaFromUpdatedAt(t *testing.T) {
	updated := time.Date(2026, 10, 7, 4, 47, 0, 0, time.UTC)
	key := "model:credential:profile:ocg|opencode-go/glm-5.3-flash"
	persist := newStubCircuitPersistence()
	snapshot := CircuitSnapshot{Key: key, State: CircuitOpen, Code: routingerr.CodeQuotaLimited, Until: updated.Add(23 * 24 * time.Hour), Strikes: 2, UpdatedAt: updated}
	persist.rows[key] = snapshot
	registry := NewCircuitRegistry(WithCircuitPersistence(persist))
	if err := registry.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := registry.Inspect(key, updated).Until; !got.Equal(updated.Add(7 * 24 * time.Hour)) {
		t.Fatalf("legacy expiry = %v, want updated_at + 7d", got)
	}
	row, _ := persist.row(key)
	if !row.Until.Equal(updated.Add(7 * 24 * time.Hour)) {
		t.Fatalf("durable expiry = %v, want capped", row.Until)
	}
}

func TestOwnerReleaseCircuitIsIsolatedAndDurable(t *testing.T) {
	persist := newStubCircuitPersistence()
	registry := NewCircuitRegistry(WithCircuitPersistence(persist))
	registry.Open("model:one", time.Now().Add(time.Hour), routingerr.CodeRateLimited)
	registry.Hold("account_block:sibling", routingerr.CodeAccountBlocked)
	releaser := registry
	if released, err := releaser.Release("model:one"); !released || err != nil {
		t.Fatalf("release = %v, %v", released, err)
	}
	if registry.Inspect("model:one", time.Now()).State != ResourceAvailable || registry.Inspect("account_block:sibling", time.Now()).State != ResourceWaiting {
		t.Fatal("release changed the wrong circuit")
	}
	if released, err := releaser.Release("model:one"); released || err != nil {
		t.Fatalf("repeated release = %v, %v", released, err)
	}
}

func TestOwnerReleaseKeepsCircuitWhenPersistenceFails(t *testing.T) {
	persist := newStubCircuitPersistence()
	registry := NewCircuitRegistry(WithCircuitPersistence(persist))
	until := time.Now().Add(time.Hour)
	registry.Open("model:one", until, routingerr.CodeRateLimited)
	persist.setFailing(true)
	if released, err := registry.Release("model:one"); released || err == nil {
		t.Fatalf("failed durable release = %v, %v", released, err)
	}
	if got := registry.Inspect("model:one", time.Now()).Until; !got.Equal(until) {
		t.Fatalf("failed release changed expiry to %v", got)
	}
}

func TestLegacyGoCapLeavesOtherPoliciesAndShorterBlocksAlone(t *testing.T) {
	updated := time.Date(2026, 10, 7, 4, 47, 0, 0, time.UTC)
	for _, snapshot := range []CircuitSnapshot{
		{Key: "model:credential:profile:go|opencode-go/glm", State: CircuitOpen, Code: routingerr.CodeQuotaLimited, UpdatedAt: updated, Until: updated.Add(time.Hour)},
		{Key: "model:credential:profile:paid|llmgateway/glm", State: CircuitOpen, Code: routingerr.CodeQuotaLimited, UpdatedAt: updated, Until: updated.Add(30 * 24 * time.Hour)},
		{Key: "account_block:go", State: CircuitOpen, Code: routingerr.CodeAccountBlocked, UpdatedAt: updated},
	} {
		if got := cappedCircuitUntil(snapshot); !got.Equal(snapshot.Until) {
			t.Fatalf("cap changed unrelated or shorter circuit %s to %v", snapshot.Key, got)
		}
	}
}

func TestOpenRouterDailyStrikeRestoresWithoutCarryingYesterday(t *testing.T) {
	now := time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC)
	persist := newStubCircuitPersistence()
	registry := NewCircuitRegistry(WithCircuitClock(func() time.Time { return now }), WithCircuitPersistence(persist))
	registry.OpenDaily("model:one", now.Add(5*time.Minute), routingerr.CodeRateLimited)
	restored := NewCircuitRegistry(WithCircuitClock(func() time.Time { return now }), WithCircuitPersistence(persist))
	if err := restored.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Minute)
	if got := restored.NextDailyStrike("model:one"); got != 2 {
		t.Fatalf("same-day restored strike = %d, want 2", got)
	}
	now = nextUTCDay(now).Add(time.Minute)
	if got := restored.NextDailyStrike("model:one"); got != 1 {
		t.Fatalf("next-day restored strike = %d, want 1", got)
	}
}

func TestOwnerReleaseInvalidatesAnOutstandingProbe(t *testing.T) {
	now := time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC)
	registry := NewCircuitRegistry(WithCircuitClock(func() time.Time { return now }))
	registry.Open("model:one", now.Add(-time.Minute), routingerr.CodeRateLimited)
	lease, acquired := registry.AcquireProbe("model:one", time.Minute)
	if !acquired {
		t.Fatal("probe was not acquired")
	}
	if released, err := registry.Release("model:one"); !released || err != nil {
		t.Fatalf("release = %v, %v", released, err)
	}
	registry.ReleaseProbe(lease, false, time.Hour)
	if registry.Inspect("model:one", now).State != ResourceAvailable {
		t.Fatal("stale probe reopened the released circuit")
	}
}
