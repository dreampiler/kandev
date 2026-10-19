package dynamic

import (
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

// A fixed-profile dispatch acquires no probe lease, so its suspension query has
// to stop blocking at the reset instant even though IsOpen keeps treating the
// expired circuit as open for dynamic probing.
func TestCircuitActiveSuspensionExpiry(t *testing.T) {
	now := time.Now()
	registry := NewCircuitRegistry(WithCircuitClock(func() time.Time { return now }))
	key := "credential:acme"

	if _, ok := registry.ActiveSuspension(key, now); ok {
		t.Fatal("an absent circuit blocked")
	}

	registry.Open(key, now.Add(time.Minute), routingerr.CodeQuotaLimited)
	snapshot, ok := registry.ActiveSuspension(key, now)
	if !ok || snapshot.Code != routingerr.CodeQuotaLimited || !snapshot.Until.Equal(now.Add(time.Minute)) {
		t.Fatalf("active circuit = %#v, %v; want a blocking quota_limited circuit", snapshot, ok)
	}

	if _, ok := registry.ActiveSuspension(key, now.Add(2*time.Minute)); ok {
		t.Fatal("an expired circuit still blocked a fixed-profile query")
	}
	if !registry.IsOpen(key, now.Add(2*time.Minute)) {
		t.Fatal("IsOpen must keep the expired circuit open for dynamic probing")
	}

	halfOpen := NewCircuitRegistry(WithCircuitClock(func() time.Time { return now }))
	halfOpen.Open(key, now.Add(-time.Second), routingerr.CodeQuotaLimited)
	if _, acquired := halfOpen.AcquireProbe(key, time.Minute); !acquired {
		t.Fatal("probe was not acquired")
	}
	if _, ok := halfOpen.ActiveSuspension(key, now); !ok {
		t.Fatal("a half-open circuit did not block")
	}

	held := NewCircuitRegistry()
	if _, err := held.Hold(key, routingerr.CodeAccountBlocked); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	snapshot, ok = held.ActiveSuspension(key, now.Add(365*24*time.Hour))
	if !ok || !snapshot.Until.IsZero() {
		t.Fatalf("held circuit = %#v, %v; want a blocking circuit with no reset instant", snapshot, ok)
	}
}

func TestEngineRecordResourceFailureOpensCircuit(t *testing.T) {
	now := time.Now()
	candidate := Candidate{ID: "fixed-profile", Enabled: true, BindingKey: ResourceKey(ScopeCredential, "acme")}
	profile := Profile{ID: "fixed-profile", Candidates: []Candidate{candidate}}

	engine := NewEngine(WithClock(func() time.Time { return now }))
	engine.RecordResourceFailure(profile, candidate.ID, &routingerr.Error{
		Code: routingerr.CodeQuotaLimited, Class: routingerr.ClassForCode(routingerr.CodeQuotaLimited),
		Confidence: routingerr.ConfHigh, FallbackAllowed: true,
	})
	snapshot, ok := engine.Circuits().ActiveSuspension(candidate.BindingKey, now)
	if !ok || snapshot.Code != routingerr.CodeQuotaLimited || !snapshot.Until.Equal(now.Add(circuitBackoff)) {
		t.Fatalf("quota failure circuit = %#v, %v; want the fixed backoff", snapshot, ok)
	}

	withHint := NewEngine(WithClock(func() time.Time { return now }))
	hint := now.Add(2 * time.Hour)
	withHint.RecordResourceFailure(profile, candidate.ID, &routingerr.Error{
		Code: routingerr.CodeRateLimited, Class: routingerr.ClassForCode(routingerr.CodeRateLimited),
		Confidence: routingerr.ConfHigh, ResetHint: &hint,
	})
	snapshot, ok = withHint.Circuits().ActiveSuspension(candidate.BindingKey, now)
	if !ok || !snapshot.Until.Equal(hint) {
		t.Fatalf("reset-hint circuit = %#v, %v; want until %v", snapshot, ok, hint)
	}

	blocked := NewEngine(WithClock(func() time.Time { return now }))
	blocked.RecordResourceFailure(profile, candidate.ID, &routingerr.Error{
		Code: routingerr.CodeAccountBlocked, Class: routingerr.ClassForCode(routingerr.CodeAccountBlocked),
		Confidence: routingerr.ConfHigh,
	})
	if !blocked.Circuits().IsHeld(accountBlockKey(candidate.ID)) {
		t.Fatal("an account restriction did not hold the profile")
	}

	ignored := NewEngine(WithClock(func() time.Time { return now }))
	ignored.RecordResourceFailure(profile, candidate.ID, &routingerr.Error{
		Code: routingerr.CodeTask, Class: routingerr.ClassForCode(routingerr.CodeTask), Confidence: routingerr.ConfHigh,
	})
	if snapshot, ok := ignored.Circuits().ActiveSuspension(candidate.BindingKey, now); ok {
		t.Fatalf("a code outside the circuit policy opened one: %#v", snapshot)
	}
}

func TestEngineCandidateSuspensionScopes(t *testing.T) {
	now := time.Now()
	engine := NewEngine(WithClock(func() time.Time { return now }))
	candidate := Candidate{ID: "fixed-profile", Enabled: true, BindingKey: ResourceKey(ScopeCredential, "acme")}

	engine.Circuits().Open(candidate.BindingKey, now.Add(time.Minute), routingerr.CodeQuotaLimited)
	suspension, blocked := engine.CandidateSuspension(candidate, now)
	if !blocked || suspension.Scope != ScopeCredential || suspension.Code != routingerr.CodeQuotaLimited || !suspension.Blocked() {
		t.Fatalf("credential suspension = %#v, %v", suspension, blocked)
	}

	profileCandidate := Candidate{ID: "p2", Enabled: true, BindingKey: ResourceKey(ScopeProfile, "p2")}
	engine.Circuits().Open(profileCandidate.BindingKey, now.Add(time.Minute), routingerr.CodeProviderUnavailable)
	got, blocked := engine.CandidateSuspension(profileCandidate, now)
	if !blocked || got.Scope != ScopeProfile {
		t.Fatalf("profile suspension = %#v, %v; want the profile scope", got, blocked)
	}

	if _, err := engine.Circuits().Hold(accountBlockKey(candidate.ID), routingerr.CodeAccountBlocked); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	got, blocked = engine.CandidateSuspension(candidate, now)
	if !blocked || got.Scope != ScopeAccountBlock || !got.Until.IsZero() {
		t.Fatalf("held suspension = %#v, %v; want an owner-release hold", got, blocked)
	}

	empty := Candidate{ID: "unknown", Enabled: true}
	if suspension, blocked := engine.CandidateSuspension(empty, now); blocked || suspension.Blocked() {
		t.Fatalf("a candidate without a circuit reported %#v, %v", suspension, blocked)
	}
}
