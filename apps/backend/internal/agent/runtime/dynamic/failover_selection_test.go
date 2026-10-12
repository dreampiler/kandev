package dynamic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

func tryNextRules() map[string]Action {
	return map[string]Action{
		string(routingerr.CodeProviderUnavailable): ActionTryNext,
		string(routingerr.CodeRateLimited):         ActionTryNext,
	}
}

// startRoute claims the first generation on the profile's first candidate and
// marks it active, the state a running task turn is in.
func startRoute(t *testing.T, engine *Engine, sessionID string, profile Profile) RouteDecision {
	t.Helper()
	ctx := context.Background()
	decision, err := engine.SelectContext(ctx, sessionID, profile, 0, "")
	if err != nil {
		t.Fatalf("initial selection: %v", err)
	}
	if err := engine.MarkActive(ctx, sessionID, decision.Generation); err != nil {
		t.Fatalf("mark active: %v", err)
	}
	return decision
}

// A cause that can reach the model on every path keeps the same model's other
// paths out of try_next; a cause bound to one path does not.
func TestTryNextExcludesTheSameModelOnlyForModelWideCauses(t *testing.T) {
	cases := []struct {
		name string
		rule string
		want string
	}{
		{name: "unrecognized unavailable", rule: "provider.unavailable.v1", want: "other-model"},
		{name: "model host", rule: routingerr.RuleProviderHeaderTimeout, want: "other-model"},
		{name: "intermittent endpoint", rule: routingerr.RuleOpenCodeEndpointUnavailable, want: "ocg-bunny"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine, _ := newFailoverEngine(time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC))
			ocz := failoverCandidate("ocz-bunny", "opencode/space-bunny-free")
			ocg := failoverCandidate("ocg-bunny", "opencode-go/space-bunny-free")
			other := failoverCandidate("other-model", "opencode/big-pickle-free")
			for _, candidate := range []*Candidate{&ocz, &ocg, &other} {
				candidate.Rules = tryNextRules()
			}
			profile := Profile{ID: "dynamic", Candidates: []Candidate{ocz, ocg, other}}
			first := startRoute(t, engine, "session-"+tc.name, profile)
			if first.ExecutionProfileID != ocz.ID {
				t.Fatalf("initial candidate = %s, want %s", first.ExecutionProfileID, ocz.ID)
			}
			next, err := engine.ApplyFailureContext(context.Background(), "session-"+tc.name, profile,
				first.Generation, ocz.ID, failureOf(routingerr.CodeProviderUnavailable, tc.rule))
			if err != nil {
				t.Fatalf("ApplyFailureContext: %v", err)
			}
			if next.ExecutionProfileID != tc.want {
				t.Fatalf("successor = %s, want %s", next.ExecutionProfileID, tc.want)
			}
		})
	}
}

// The interrupted-turn successor search reads circuit health without claiming
// probes. Before, it claimed the probe of every expired candidate, so the
// winner's own claim then failed on the lease the search had just taken and
// the search ran out of candidates, while the other expired candidates stayed
// leased for thirty seconds.
func TestInterruptedSuccessorClaimsOnlyTheWinnersProbe(t *testing.T) {
	start := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	engine, clock := newFailoverEngine(start)
	current := failoverCandidate("current", "opencode/big-pickle-free")
	expiredB := failoverCandidate("expired-b", "opencode/step-5-preview-free")
	expiredC := failoverCandidate("expired-c", "llmgateway/glm-5.3-flash")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{current, expiredB, expiredC}}
	first := startRoute(t, engine, "session-interrupted", profile)

	engine.Circuits().Open(expiredB.pathModelKey(), clock.now.Add(-time.Second), routingerr.CodeProviderUnavailable)
	engine.Circuits().Open(expiredC.pathModelKey(), clock.now.Add(-time.Second), routingerr.CodeProviderUnavailable)
	failure := failureOf(routingerr.CodeRateLimited, "opencode.stderr.rate.v1")
	decision, err := engine.ApplyInterruptedFailureContext(context.Background(), "session-interrupted",
		profile, first.Generation, current.ID, failure)
	if err != nil {
		t.Fatalf("ApplyInterruptedFailureContext: %v", err)
	}
	if decision.ExecutionProfileID != expiredB.ID {
		t.Fatalf("successor = %s, want the expired candidate %s", decision.ExecutionProfileID, expiredB.ID)
	}
	if got := engine.Circuits().Inspect(expiredB.pathModelKey(), clock.now).State; got != ResourceProbing {
		t.Fatalf("winner circuit = %s, want its probe claimed", got)
	}
	if got := engine.Circuits().Inspect(expiredC.pathModelKey(), clock.now).State; got != ResourceExpired {
		t.Fatalf("passed-over circuit = %s, want it left expired for other sessions", got)
	}
}

// An interrupted turn on a path-bound cause may continue on the same model
// through another path.
func TestInterruptedSuccessorKeepsTheSameModelForAPathBoundCause(t *testing.T) {
	engine, _ := newFailoverEngine(time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC))
	ocg := failoverCandidate("ocg-longcat", "opencode-go/longcat-2.5-preview-free")
	ocz := failoverCandidate("ocz-longcat", "opencode/longcat-2.5-preview-free")
	other := failoverCandidate("dp-glm", "llmgateway/glm-5.3-flash")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{ocg, ocz, other}}
	first := startRoute(t, engine, "session-path-bound", profile)
	decision, err := engine.ApplyInterruptedFailureContext(context.Background(), "session-path-bound",
		profile, first.Generation, ocg.ID, failureOf(routingerr.CodeProviderUnavailable, routingerr.RuleOpenCodeEndpointUnavailable))
	if err != nil {
		t.Fatalf("ApplyInterruptedFailureContext: %v", err)
	}
	if decision.ExecutionProfileID != ocz.ID {
		t.Fatalf("successor = %s, want the same model on Zen", decision.ExecutionProfileID)
	}
}

type launchFailureDownstream struct {
	failure  error
	launches int
}

func (d *launchFailureDownstream) Launch(context.Context, DownstreamLaunch) (DownstreamExecution, error) {
	d.launches++
	if d.launches == 1 {
		return DownstreamExecution{}, d.failure
	}
	return DownstreamExecution{ID: "execution-next"}, nil
}

func (*launchFailureDownstream) Resume(context.Context, string, string) error { return nil }
func (*launchFailureDownstream) Stop(context.Context, string, string) error   { return nil }

// A probe launch that fails must move its circuit up the ladder. Before, the
// probe was released with a one-minute block first, and the failure recorded
// inside that block read as the same incident.
func TestLaunchFailureOfAProbeRaisesItsStrike(t *testing.T) {
	start := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	for _, routed := range []bool{true, false} {
		engine, clock := newFailoverEngine(start)
		probed := failoverCandidate("probed", "opencode-go/longcat-2.5-preview-free")
		next := failoverCandidate("next", "llmgateway/glm-5.3-flash")
		if routed {
			probed.Rules = tryNextRules()
		}
		profile := Profile{ID: "dynamic", Candidates: []Candidate{probed, next}}
		key := probed.pathModelKey()
		engine.Circuits().Open(key, clock.now.Add(-time.Second), routingerr.CodeProviderUnavailable)
		failure := &routingerr.Error{
			Code: routingerr.CodeProviderUnavailable, ClassifierRule: routingerr.RuleOpenCodeEndpointUnavailable,
			Phase: routingerr.PhaseProcessStart, FallbackAllowed: true,
		}
		downstream := &launchFailureDownstream{failure: failure}
		conductor := NewConductor(engine, conductorTestProfileLoader{profile: profile}, downstream)
		_, err := conductor.Launch(context.Background(), ConductorLaunch{SessionID: "session-probe", LogicalProfileID: profile.ID})
		if routed && err != nil {
			t.Fatalf("routed launch: %v", err)
		}
		if !routed && !errors.Is(err, failure) {
			t.Fatalf("unrouted launch error = %v, want the launch failure", err)
		}
		status := engine.Circuits().Inspect(key, clock.now)
		if status.Strikes != 2 || status.State != ResourceWaiting || status.Until.Sub(clock.now) != 2*time.Minute {
			t.Fatalf("routed=%v probed circuit = %#v, want strike 2 and the 2m step", routed, status)
		}
	}
}
