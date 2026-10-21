package dynamic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

type stubLimits struct {
	blocked map[string]time.Time
}

func (s stubLimits) CheckLimit(_ context.Context, candidate Candidate, _ string, _ time.Time) (LimitVerdict, error) {
	if at, ok := s.blocked[candidate.ID]; ok {
		return LimitVerdict{Reason: "limit_daily", RetryAt: at}, nil
	}
	return LimitVerdict{}, nil
}

func TestConfiguredLimitSkipsTheExhaustedCandidateInOrder(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	engine := NewEngine(WithClock(func() time.Time { return now }))
	engine.SetLimitChecker(stubLimits{blocked: map[string]time.Time{"a": now.Add(time.Hour)}})
	profile := Profile{ID: "p", Version: 1, Candidates: []Candidate{
		{ID: "a", Enabled: true, BindingKey: ResourceKey(ScopeProfile, "a"), ModelID: "opencode/a"},
		{ID: "b", Enabled: true, BindingKey: ResourceKey(ScopeProfile, "b"), ModelID: "opencode/b"},
	}}
	decision, err := engine.SelectContext(context.Background(), "s", profile, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if decision.ExecutionProfileID != "b" {
		t.Fatalf("selected %q, want the next eligible candidate b", decision.ExecutionProfileID)
	}
}

func TestConfiguredLimitDefersWhenEveryCandidateIsExhausted(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	engine := NewEngine(WithClock(func() time.Time { return now }))
	engine.SetLimitChecker(stubLimits{blocked: map[string]time.Time{
		"a": now.Add(time.Hour), "b": now.Add(2 * time.Hour),
	}})
	profile := Profile{ID: "p", Version: 1, Candidates: []Candidate{
		{ID: "a", Enabled: true, BindingKey: ResourceKey(ScopeProfile, "a")},
		{ID: "b", Enabled: true, BindingKey: ResourceKey(ScopeProfile, "b")},
	}}
	_, err := engine.SelectContext(context.Background(), "s", profile, 0, "")
	var noCandidate *NoEligibleCandidateError
	if !errors.As(err, &noCandidate) {
		t.Fatalf("error = %v, want NoEligibleCandidateError", err)
	}
}

func TestSuspendedCandidateIsNotSelectable(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	engine := NewEngine(WithClock(func() time.Time { return now }))
	profile := Profile{ID: "p", Version: 1, Candidates: []Candidate{
		{ID: "a", Enabled: true, BindingKey: ResourceKey(ScopeProfile, "a"), SuspendedUntil: now.Add(time.Hour)},
		{ID: "b", Enabled: true, BindingKey: ResourceKey(ScopeProfile, "b")},
	}}
	decision, err := engine.SelectContext(context.Background(), "s", profile, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if decision.ExecutionProfileID != "b" {
		t.Fatalf("selected %q, want b", decision.ExecutionProfileID)
	}
}

func TestLimitObserverSeesClassifiedFailure(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	engine := NewEngine(WithClock(func() time.Time { return now }))
	recorder := &stubObserver{}
	engine.SetLimitObserver(recorder)
	profile := Profile{ID: "p", Version: 1, Candidates: []Candidate{
		{ID: "a", Enabled: true, BindingKey: ResourceKey(ScopeProfile, "a")},
	}}
	if _, err := engine.SelectContext(context.Background(), "s", profile, 0, ""); err != nil {
		t.Fatal(err)
	}
	_, err := engine.ApplyFailureContext(context.Background(), "s", profile, 1, "a", &routingerr.Error{Code: routingerr.CodeQuotaLimited})
	if err == nil {
		t.Fatal("expected the exhausted route to fail")
	}
	if recorder.candidateID != "a" {
		t.Fatalf("observed candidate = %q, want a", recorder.candidateID)
	}
}

type stubObserver struct{ candidateID string }

func (s *stubObserver) ObserveLimit(_ context.Context, candidate Candidate, _ *routingerr.Error, _ time.Time) {
	s.candidateID = candidate.ID
}

func TestRateLimitedFailureSuspendsTheCredentialBinding(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	engine := NewEngine(WithClock(func() time.Time { return now }))
	profile := Profile{ID: "p", Version: 1, Candidates: []Candidate{
		{ID: "a", Enabled: true, BindingKey: ResourceKey(ScopeProfile, "a"), ModelID: "opencode/a"},
	}}
	if _, err := engine.SelectContext(context.Background(), "s", profile, 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ApplyFailureContext(context.Background(), "s", profile, 1, "a",
		&routingerr.Error{Code: routingerr.CodeRateLimited}); err == nil {
		t.Fatal("expected the only candidate to be exhausted")
	}
	if !engine.Circuits().IsOpen(ResourceKey(ScopeProfile, "a"), now) {
		t.Fatal("a rate-limited failure did not suspend the credential binding")
	}
	modelKey := ResourceKey(ScopeModel, ResourceKey(ScopeProfile, "a")+"|opencode/a")
	if engine.Circuits().IsOpen(modelKey, now) {
		t.Fatal("a rate-limited failure created an unused model-key circuit")
	}
}
