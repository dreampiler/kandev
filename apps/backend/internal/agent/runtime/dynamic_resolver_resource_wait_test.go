package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/agents"
	"github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	agentsettingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	"github.com/kandev/kandev/internal/agent/settings/store"
)

const (
	resourceWaitProfile = "dynamic-wait"
	// resourceWaitStillBlocked is the candidate that frees up last and is also
	// the one a session chose before it ran out of candidates.
	resourceWaitStillBlocked = "concrete-still-blocked"
	// resourceWaitFreed is the candidate whose suspension expires first.
	resourceWaitFreed   = "concrete-freed"
	resourceWaitSession = "resource-wait-session"
	// Both candidates are OpenCode Go models, whose limit is metered per model
	// and whose unknown-reset block follows the 2h ladder.
	resourceWaitModelStillBlocked = "opencode-go/glm-5"
	resourceWaitModelFreed        = "opencode-go/kimi-k2"
)

func resourceWaitQuota() *routingerr.Error {
	return &routingerr.Error{Code: routingerr.CodeQuotaLimited}
}

// resourceWaitResolver builds a dynamic profile with two enabled candidates and
// returns the engine alongside a settable clock, so a test can expire one
// suspension without waiting in real time.
func resourceWaitResolver(t *testing.T) (*ProfileExecutionResolver, *dynamic.Engine, *time.Time) {
	t.Helper()
	clock := time.Unix(1_700_000_000, 0)
	engine := dynamic.NewEngine(dynamic.WithClock(func() time.Time { return clock }))
	store := &resourceWaitProfiles{rows: map[string]*agentsettingsmodels.AgentProfile{
		resourceWaitProfile: {
			ID: resourceWaitProfile, AgentID: agents.DynamicAgentID, Enabled: true,
		},
		resourceWaitStillBlocked: {
			ID: resourceWaitStillBlocked, AgentID: "concrete", Enabled: true,
			Model: resourceWaitModelStillBlocked,
		},
		resourceWaitFreed: {
			ID: resourceWaitFreed, AgentID: "concrete", Enabled: true,
			Model: resourceWaitModelFreed,
		},
	}}
	store.dynamic = &agentsettingsmodels.DynamicAgentProfile{
		ProfileID: resourceWaitProfile, Version: 1,
	}
	store.routes = []agentsettingsmodels.DynamicAgentRoute{
		{DynamicProfileID: resourceWaitProfile, ExecutionProfileID: resourceWaitStillBlocked, Enabled: true},
		{DynamicProfileID: resourceWaitProfile, ExecutionProfileID: resourceWaitFreed, Enabled: true},
	}
	return NewProfileExecutionResolver(store, engine, true), engine, &clock
}

// resourceWaitProfiles answers a lookup for any of the fixture's profile rows.
// The dynamic family agent is the only one that resolves to the dynamic agent,
// because a candidate keeps its own concrete agent identity.
type resourceWaitProfiles struct {
	store.Repository
	store.DynamicProfileRepository
	rows    map[string]*agentsettingsmodels.AgentProfile
	dynamic *agentsettingsmodels.DynamicAgentProfile
	routes  []agentsettingsmodels.DynamicAgentRoute
}

func (p *resourceWaitProfiles) GetAgentProfile(_ context.Context, id string) (*agentsettingsmodels.AgentProfile, error) {
	if row, ok := p.rows[id]; ok {
		return row, nil
	}
	return nil, errors.New("profile not found")
}

func (p *resourceWaitProfiles) GetAgent(_ context.Context, id string) (*agentsettingsmodels.Agent, error) {
	if id == agents.DynamicAgentID {
		return &agentsettingsmodels.Agent{ID: id, Name: agents.DynamicAgentID}, nil
	}
	return &agentsettingsmodels.Agent{ID: id, Name: "concrete"}, nil
}

func (p *resourceWaitProfiles) GetDynamicAgentProfile(
	_ context.Context, profileID string,
) (*agentsettingsmodels.DynamicAgentProfile, []agentsettingsmodels.DynamicAgentRoute, error) {
	if profileID != p.dynamic.ProfileID {
		return nil, nil, errors.New("dynamic profile not found")
	}
	return p.dynamic, p.routes, nil
}

// resourceWaitProfileConfig mirrors the candidate keys the resolver's loader
// builds for these rows: a profile-scoped binding, and a model key because the
// provider meters each model separately. A suspension recorded here is therefore
// the same one a selection reads back.
func resourceWaitProfileConfig() dynamic.Profile {
	candidate := func(id, model string) dynamic.Candidate {
		binding := dynamic.ResourceKey(dynamic.ScopeProfile, id)
		return dynamic.Candidate{
			ID: id, Enabled: true, ModelID: model, BindingKey: binding,
			ModelKey: dynamic.ResourceKey(dynamic.ScopeModel, binding+"|"+model),
		}
	}
	return dynamic.Profile{
		ID: resourceWaitProfile, Version: 1,
		Candidates: []dynamic.Candidate{
			candidate(resourceWaitStillBlocked, resourceWaitModelStillBlocked),
			candidate(resourceWaitFreed, resourceWaitModelFreed),
		},
	}
}

// exhaustedResourceWait suspends both candidates with different end instants,
// then drives a selection that finds nothing eligible so the route persists the
// waiting state the recovery timer later retries. It returns that generation.
func exhaustedResourceWait(
	t *testing.T, resolver *ProfileExecutionResolver, engine *dynamic.Engine, clock *time.Time,
) int64 {
	t.Helper()
	ctx := context.Background()
	profile := resourceWaitProfileConfig()
	quota := resourceWaitQuota()
	engine.RecordResourceFailure(ctx, profile, resourceWaitFreed, quota)
	// A later strike on the other candidate pushes its end past the first one's,
	// so exactly one candidate frees up while the other stays blocked. That is
	// the shape a resource wait produces once work has already moved between two
	// models that both hit their own limit.
	*clock = clock.Add(time.Hour)
	engine.RecordResourceFailure(ctx, profile, resourceWaitStillBlocked, quota)

	var noCandidate *dynamic.NoEligibleCandidateError
	_, err := resolver.ResolveRouteAction(
		ctx, resourceWaitSession, resourceWaitProfile, resourceWaitStillBlocked, 0, "retry",
	)
	if !errors.As(err, &noCandidate) {
		t.Fatalf("selection with every candidate suspended = %v, want NoEligibleCandidateError", err)
	}
	state, ok := engine.State(resourceWaitSession)
	if !ok || state.Status != "waiting" {
		t.Fatalf("route state = %#v (ok=%v), want waiting", state, ok)
	}
	return state.Generation
}

// A route waiting on suspended resources must retry as a fresh selection. A
// retry that carried the last chosen candidate as a preference would make the
// engine refuse every other candidate while that one stays suspended, so the
// session would sit in waiting forever even though a usable candidate had
// already come back.
func TestRetryResourceWaitSelectsCandidateThatAlreadyFreedUp(t *testing.T) {
	resolver, engine, clock := resourceWaitResolver(t)
	generation := exhaustedResourceWait(t, resolver, engine, clock)

	// Advance past the candidate that frees first, but not past the one the
	// session last chose.
	*clock = clock.Add(90 * time.Minute)
	profile := resourceWaitProfileConfig()
	if engine.SuspensionFor(profile.Candidates[1], *clock).Blocked() {
		t.Fatalf("fixture candidate %s is still blocked after the clock advanced", resourceWaitFreed)
	}
	if !engine.SuspensionFor(profile.Candidates[0], *clock).Blocked() {
		t.Fatalf("fixture candidate %s is no longer blocked after the clock advanced", resourceWaitStillBlocked)
	}

	execution, err := resolver.ResolveRouteAction(
		context.Background(), resourceWaitSession, resourceWaitProfile,
		resourceWaitStillBlocked, generation, "retry",
	)
	if err != nil {
		t.Fatalf("retry after resource wait: %v", err)
	}
	if execution.ExecutionProfileID != resourceWaitFreed {
		t.Fatalf("execution profile = %q, want %q", execution.ExecutionProfileID, resourceWaitFreed)
	}
}

// A wait that still finds every candidate suspended must schedule the next
// attempt at the earliest suspension end instead of leaving the wait manual.
func TestRetryResourceWaitReschedulesAtEarliestSuspensionEnd(t *testing.T) {
	resolver, engine, clock := resourceWaitResolver(t)
	generation := exhaustedResourceWait(t, resolver, engine, clock)

	var deadlines []time.Time
	engine.SetResourceWaitObserver(func(_ string, _ int64, deadline time.Time) {
		deadlines = append(deadlines, deadline)
	})
	profile := resourceWaitProfileConfig()
	earliest := engine.SuspensionFor(profile.Candidates[1], *clock).Until
	if latest := engine.SuspensionFor(profile.Candidates[0], *clock).Until; !latest.After(earliest) {
		t.Fatalf("fixture suspensions %s and %s are not distinct", earliest, latest)
	}

	var noCandidate *dynamic.NoEligibleCandidateError
	_, err := resolver.ResolveRouteAction(
		context.Background(), resourceWaitSession, resourceWaitProfile,
		resourceWaitStillBlocked, generation, "retry",
	)
	if !errors.As(err, &noCandidate) {
		t.Fatalf("retry with every candidate suspended = %v, want NoEligibleCandidateError", err)
	}
	if len(deadlines) != 1 || !deadlines[0].Equal(earliest) {
		t.Fatalf("rescheduled deadlines = %v, want exactly one at %s", deadlines, earliest)
	}
}
