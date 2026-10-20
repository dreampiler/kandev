package dynamic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

type quotaStopConductorDownstream struct{ err error }

func (d quotaStopConductorDownstream) Launch(context.Context, DownstreamLaunch) (DownstreamExecution, error) {
	return DownstreamExecution{}, d.err
}

func (quotaStopConductorDownstream) Resume(context.Context, string, string) error { return nil }

func (quotaStopConductorDownstream) Stop(context.Context, string, string) error { return nil }

func quotaStopProfile() Profile {
	return Profile{
		ID: "dynamic-quota-stop", Version: 1,
		Candidates: []Candidate{
			{ID: "quota", Enabled: true, BindingKey: "credential:quota", Rules: map[string]Action{
				string(routingerr.CodeQuotaLimited): ActionStop,
			}},
			{ID: "healthy", Enabled: true, BindingKey: "credential:healthy"},
		},
	}
}

func quotaStopFailure(reset time.Time) *routingerr.Error {
	return &routingerr.Error{
		Code: routingerr.CodeQuotaLimited, Class: routingerr.ClassHard,
		Phase: routingerr.PhaseProcessStart, FallbackAllowed: true, ResetHint: &reset,
	}
}

// A quota failure the policy answers with stop must still open the account
// circuit until the provider reset; otherwise the next selection retries the
// exhausted account.
func TestConductorOpensCircuitForUnroutedQuotaFailure(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	profile := quotaStopProfile()
	engine := NewEngine(WithClock(func() time.Time { return now }))
	failure := quotaStopFailure(now.Add(3 * time.Hour))
	conductor := NewConductor(engine, conductorTestProfileLoader{profile: profile}, quotaStopConductorDownstream{err: failure})

	_, err := conductor.Launch(context.Background(), ConductorLaunch{
		SessionID: "session-quota-stop", LogicalProfileID: profile.ID,
	})
	if !errors.Is(err, failure) {
		t.Fatalf("Launch error = %v, want %v", err, failure)
	}
	if !engine.Circuits().IsOpen("credential:quota", now) {
		t.Fatal("the stopped quota failure did not open the candidate circuit")
	}
	next, err := engine.Select("session-after-quota", profile, 0, "")
	if err != nil || next.ExecutionProfileID != "healthy" {
		t.Fatalf("next selection = %#v, %v; want the healthy candidate", next, err)
	}
}

// The same recording must not charge a failure the route already left: the
// generation fence keeps a stale attempt from opening a current candidate.
func TestConductorUnroutedFailureRespectsTheGenerationFence(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	profile := quotaStopProfile()
	engine := NewEngine(WithClock(func() time.Time { return now }))
	initial, err := engine.Select("session-stale", profile, 0, "")
	if err != nil || initial.ExecutionProfileID != "quota" {
		t.Fatalf("initial selection = %#v, %v; want quota", initial, err)
	}
	moved, err := engine.Select("session-stale", profile, initial.Generation, "quota")
	if err != nil || moved.ExecutionProfileID != "healthy" {
		t.Fatalf("advanced selection = %#v, %v; want healthy", moved, err)
	}

	failure := quotaStopFailure(now.Add(3 * time.Hour))
	conductor := NewConductor(engine, conductorTestProfileLoader{profile: profile}, quotaStopConductorDownstream{err: failure})
	_, _ = conductor.LaunchSelected(context.Background(), ConductorSelectedLaunch{
		SessionID: "session-stale", LogicalProfileID: profile.ID, Decision: initial,
	})
	if engine.Circuits().IsOpen("credential:quota", now) {
		t.Fatal("a stale attempt opened the candidate circuit")
	}
}
