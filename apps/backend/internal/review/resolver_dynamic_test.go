package review

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRoute models a dynamic profile's route: an ordered candidate list plus
// the rows its health marks blocked. It picks the first row that is neither
// blocked nor skipped, which is what the real route does for an ordered
// profile; the real ranking and health are exercised by the backendapp adapter.
type fakeRoute struct {
	candidates map[string][]string
	blocked    map[string]bool
	err        error
	skipped    []string
}

func (f *fakeRoute) SelectCandidate(_ context.Context, profileID string, skip func(string) bool) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	chosen := ""
	for _, id := range f.candidates[profileID] {
		if skip != nil && skip(id) {
			f.skipped = append(f.skipped, id)
			continue
		}
		if f.blocked[id] || chosen != "" {
			continue
		}
		chosen = id
	}
	return chosen, nil
}

type fakeUtilityProfile struct {
	profileID string
}

func (f fakeUtilityProfile) CodeReviewAgent(context.Context) (string, string, bool, bool, error) {
	return "", "", false, false, nil
}

func (f fakeUtilityProfile) CodeReviewAgentProfile(context.Context) (string, bool, bool, error) {
	return f.profileID, true, true, nil
}

// reviewProfiles mirrors the production shape: "검토 (동적)" is a dynamic
// profile (agent "dynamic", no model) over concrete free/paid candidates, and
// the writer runs "구현 상급 (동적)" whose current candidate is 6 Sol.
func reviewProfiles() fakeProfilesByID {
	return fakeProfilesByID{
		"review-dynamic": {AgentID: "dynamic", Name: "검토 (동적)", Dynamic: true},
		"writer-dynamic": {AgentID: "dynamic", Name: "구현 상급 (동적)", Dynamic: true},
		"big-pickle":     {AgentID: "opencode", Model: "opencode/big-pickle", Name: "OCZ-BigPickle Free"},
		"space-bunny":    {AgentID: "opencode", Model: "opencode/space-bunny-free", Name: "OCZ-SpaceBunny Free"},
		"sol":            {AgentID: "codex", Model: "gpt-6.1-sol", Name: "6 Sol"},
		"sol-twin":       {AgentID: "codex", Model: "gpt-6.1-sol", Name: "6 Sol (second account)"},
		"opus":           {AgentID: "claude", Model: "opus", Name: "Opus"},
		"passthrough":    {AgentID: "claude", Model: "opus", CLIPassthrough: true},
		"no-model":       {AgentID: "gemini"},
	}
}

func writerOrigin() *OriginIdentity {
	return &OriginIdentity{ProfileID: "writer-dynamic", ExecutionProfileID: "sol"}
}

// TestResolveDynamicReviewer_ManualRunUsesTheRoutesFirstCandidate covers the
// task.review.run failure on f954e49a: a dynamic reviewer profile used to fail
// with `agent profile "검토 (동적)" has no model configured`.
func TestResolveDynamicReviewer_ManualRunUsesTheRoutesFirstCandidate(t *testing.T) {
	r := NewResolver(reviewProfiles(), nil, nil)
	r.Routes = &fakeRoute{candidates: map[string][]string{"review-dynamic": {"big-pickle", "space-bunny"}}}

	got, err := r.ResolveExcluding(context.Background(), "review-dynamic", nil)
	if err != nil {
		t.Fatalf("ResolveExcluding returned error: %v", err)
	}
	if got.ProfileID != "big-pickle" || got.AgentID != "opencode" || got.Model != "opencode/big-pickle" {
		t.Fatalf("got %+v, want the route's first candidate as a concrete reviewer", got)
	}
	if got.Source != SourceAgentProfile {
		t.Fatalf("source = %q, want %q", got.Source, SourceAgentProfile)
	}
}

// TestResolveDynamicReviewer_SkipsTheWritersModelAndBlockedRows covers the
// workflow-step path with a dynamic writer: the writer's concrete candidate and
// any other profile on the same agent+model are skipped in place, a blocked row
// is skipped by the route, and the next candidate in route order reviews.
func TestResolveDynamicReviewer_SkipsTheWritersModelAndBlockedRows(t *testing.T) {
	route := &fakeRoute{
		candidates: map[string][]string{"review-dynamic": {"sol", "sol-twin", "big-pickle", "space-bunny", "opus"}},
		blocked:    map[string]bool{"big-pickle": true},
	}
	r := NewResolver(reviewProfiles(), nil, nil)
	r.Routes = route

	got, err := r.ResolveRequiredOrigin(context.Background(), "review-dynamic", writerOrigin())
	if err != nil {
		t.Fatalf("ResolveRequiredOrigin returned error: %v", err)
	}
	if got.ProfileID != "space-bunny" || got.Model != "opencode/space-bunny-free" {
		t.Fatalf("got %+v, want space-bunny (sol and its twin are the writer, big-pickle is blocked)", got)
	}
	if strings.Join(route.skipped, ",") != "sol,sol-twin" {
		t.Fatalf("skipped = %v, want exactly the writer's model rows", route.skipped)
	}
}

// TestResolveDynamicReviewer_SkipsCandidatesThatCannotReview keeps a row that
// could never run a structured review from being chosen.
func TestResolveDynamicReviewer_SkipsCandidatesThatCannotReview(t *testing.T) {
	r := NewResolver(reviewProfiles(), nil, nil)
	r.Routes = &fakeRoute{candidates: map[string][]string{
		"review-dynamic": {"missing", "passthrough", "no-model", "writer-dynamic", "opus"},
	}}

	got, err := r.ResolveExcluding(context.Background(), "review-dynamic", nil)
	if err != nil {
		t.Fatalf("ResolveExcluding returned error: %v", err)
	}
	if got.ProfileID != "opus" {
		t.Fatalf("got %+v, want opus after the unusable rows", got)
	}
}

// TestResolveDynamicReviewer_NoDistinctCandidateFallsThrough treats a dynamic
// profile with nothing distinct and available like an explicit profile that is
// the origin: the utility/default candidates are tried, and if they are the
// origin too the review fails closed.
func TestResolveDynamicReviewer_NoDistinctCandidateFallsThrough(t *testing.T) {
	route := &fakeRoute{
		candidates: map[string][]string{"review-dynamic": {"sol", "big-pickle"}},
		blocked:    map[string]bool{"big-pickle": true},
	}

	t.Run("to a distinct utility reviewer", func(t *testing.T) {
		r := NewResolver(reviewProfiles(), &fakeUtility{found: true, enabled: true, agentID: "claude", model: "sonnet"}, nil)
		r.Routes = route
		got, err := r.ResolveRequiredOrigin(context.Background(), "review-dynamic", writerOrigin())
		if err != nil {
			t.Fatalf("ResolveRequiredOrigin returned error: %v", err)
		}
		if got.AgentID != "claude" || got.Model != "sonnet" || got.Source != SourceUtilityAgent {
			t.Fatalf("got %+v, want the utility reviewer claude/sonnet", got)
		}
	})
	t.Run("and fails closed when every candidate is the writer", func(t *testing.T) {
		r := NewResolver(reviewProfiles(),
			&fakeUtility{found: true, enabled: true, agentID: "codex", model: "gpt-6.1-sol"},
			&fakeDefaults{agentID: "codex", model: "gpt-6.1-sol"})
		r.Routes = route
		_, err := r.ResolveRequiredOrigin(context.Background(), "review-dynamic", writerOrigin())
		if !errors.Is(err, ErrAgentUnavailable) || !errors.Is(err, ErrNoDistinctReviewer) {
			t.Fatalf("err = %v, want ErrAgentUnavailable wrapping ErrNoDistinctReviewer", err)
		}
	})
}

// TestResolveDynamicReviewer_DynamicUtilityProfileIsResolvedToo applies the same
// resolution to a dynamic code-review utility profile, so the writer's model is
// excluded there as well instead of being left to an unconstrained launch.
func TestResolveDynamicReviewer_DynamicUtilityProfileIsResolvedToo(t *testing.T) {
	r := NewResolver(reviewProfiles(), fakeUtilityProfile{profileID: "review-dynamic"}, nil)
	r.Routes = &fakeRoute{candidates: map[string][]string{"review-dynamic": {"sol", "opus"}}}

	got, err := r.ResolveRequiredOrigin(context.Background(), "", writerOrigin())
	if err != nil {
		t.Fatalf("ResolveRequiredOrigin returned error: %v", err)
	}
	if got.ProfileID != "opus" || got.Model != "opus" || got.Source != SourceUtilityAgent {
		t.Fatalf("got %+v, want the utility profile's concrete candidate opus", got)
	}
}

// TestResolveDynamicReviewer_UnresolvableRouteFailsClosed never reviews with a
// dynamic profile it could not resolve to a concrete, comparable model.
func TestResolveDynamicReviewer_UnresolvableRouteFailsClosed(t *testing.T) {
	for name, route := range map[string]DynamicRouteSelector{
		"no route wired": nil,
		"route error":    &fakeRoute{err: errors.New("dynamic agent routing is disabled")},
	} {
		t.Run(name, func(t *testing.T) {
			r := NewResolver(reviewProfiles(), nil, nil)
			r.Routes = route
			_, err := r.ResolveExcluding(context.Background(), "review-dynamic", nil)
			if !errors.Is(err, ErrAgentUnavailable) {
				t.Fatalf("err = %v, want ErrAgentUnavailable", err)
			}
		})
	}
}
