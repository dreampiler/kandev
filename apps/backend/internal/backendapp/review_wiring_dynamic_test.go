package backendapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/agents"
	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	agentsettingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	"github.com/kandev/kandev/internal/review"
)

// fakeDynamicProfileLoader returns a fixed dynamic profile.
type fakeDynamicProfileLoader struct {
	profile dynamicruntime.Profile
	err     error
}

func (f fakeDynamicProfileLoader) LoadDynamicProfile(context.Context, string) (dynamicruntime.Profile, error) {
	return f.profile, f.err
}

// reviewRouteFixture is a dynamic reviewer route over the real read-only
// preview: ordered candidates, one disabled row and one row whose route health
// is a running suspension (a blocked candidate).
func reviewRouteFixture(now time.Time) reviewDynamicRoute {
	candidate := func(id string, enabled bool) dynamicruntime.Candidate {
		return dynamicruntime.Candidate{ID: id, Enabled: enabled}
	}
	profile := dynamicruntime.Profile{ID: "review-dynamic", Version: 1, Candidates: []dynamicruntime.Candidate{
		candidate("sol", true),
		candidate("disabled", false),
		candidate("blocked", true),
		candidate("big-pickle", true),
		candidate("opus", true),
	}}
	snapshot := newDynamicUsageSnapshot(nil, nil, func() time.Time { return now }).
		WithPreviewHealth(stubSuspensionHealth{suspensions: map[string]dynamicruntime.CandidateSuspension{
			"blocked": {State: dynamicruntime.ResourceWaiting, Until: now.Add(time.Hour), Scope: dynamicruntime.SuspensionScopeProfile},
		}})
	return reviewDynamicRoute{
		profiles: fakeDynamicProfileLoader{profile: profile},
		preview:  snapshot,
		now:      func() time.Time { return now },
	}
}

// TestReviewDynamicRoute_TakesTheRoutesChoiceAmongAllowedRows proves the
// adapter adds no selection rule: disabled and blocked rows are out exactly as
// in a live selection, a row the resolver rejects is skipped in place, and the
// first remaining row in the saved order wins.
func TestReviewDynamicRoute_TakesTheRoutesChoiceAmongAllowedRows(t *testing.T) {
	now := time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC)
	route := reviewRouteFixture(now)
	ctx := context.Background()

	cases := []struct {
		name     string
		rejected map[string]bool
		want     string
	}{
		{name: "nothing rejected", rejected: nil, want: "sol"},
		{name: "writer rejected", rejected: map[string]bool{"sol": true}, want: "big-pickle"},
		{name: "writer and next rejected", rejected: map[string]bool{"sol": true, "big-pickle": true}, want: "opus"},
		{name: "every usable row rejected", rejected: map[string]bool{"sol": true, "big-pickle": true, "opus": true}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := route.SelectCandidate(ctx, "review-dynamic", func(id string) bool { return tc.rejected[id] })
			if err != nil {
				t.Fatalf("SelectCandidate: %v", err)
			}
			if got != tc.want {
				t.Fatalf("SelectCandidate = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReviewDynamicRoute_LoadFailureIsAnError(t *testing.T) {
	route := reviewDynamicRoute{
		profiles: fakeDynamicProfileLoader{err: errors.New("dynamic agent routing is disabled")},
		preview:  newDynamicUsageSnapshot(nil, nil, nil),
	}
	if _, err := route.SelectCandidate(context.Background(), "review-dynamic", nil); err == nil {
		t.Fatal("expected the load failure to surface")
	}
}

// fakeAgentProfiles serves agent-settings rows by ID.
type fakeAgentProfiles map[string]*agentsettingsmodels.AgentProfile

func (f fakeAgentProfiles) GetAgentProfile(_ context.Context, id string) (*agentsettingsmodels.AgentProfile, error) {
	return f[id], nil
}

// TestReviewResolver_DynamicReviewerOverTheRealRoute composes the production
// pieces: the agent-settings lookup flags the dynamic "검토 (동적)" profile, the
// resolver excludes the dynamic writer's concrete candidate (6 Sol), the route
// skips the blocked row, and the review runs on the next concrete candidate.
func TestReviewResolver_DynamicReviewerOverTheRealRoute(t *testing.T) {
	now := time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC)
	profiles := fakeAgentProfiles{
		"review-dynamic": {ID: "review-dynamic", AgentID: agents.DynamicAgentID, Name: "검토 (동적)"},
		"writer-dynamic": {ID: "writer-dynamic", AgentID: agents.DynamicAgentID, Name: "구현 상급 (동적)"},
		"sol":            {ID: "sol", AgentID: "codex-agent", Model: "gpt-6.1-sol", Name: "6 Sol"},
		"blocked":        {ID: "blocked", AgentID: "opencode-agent", Model: "opencode/blocked", Name: "Blocked"},
		"big-pickle":     {ID: "big-pickle", AgentID: "opencode-agent", Model: "opencode/big-pickle", Name: "OCZ-BigPickle Free"},
		"opus":           {ID: "opus", AgentID: "claude-agent", Model: "opus", Name: "Opus"},
	}
	resolver := review.NewResolver(reviewProfileLookup{profiles: profiles}, nil, nil)
	resolver.Routes = reviewRouteFixture(now)
	origin := &review.OriginIdentity{ProfileID: "writer-dynamic", ExecutionProfileID: "sol"}

	got, err := resolver.ResolveRequiredOrigin(context.Background(), "review-dynamic", origin)
	if err != nil {
		t.Fatalf("ResolveRequiredOrigin: %v", err)
	}
	if got.ProfileID != "big-pickle" || got.AgentID != "opencode-agent" || got.Model != "opencode/big-pickle" {
		t.Fatalf("reviewer = %+v, want big-pickle (sol is the writer, blocked is suspended)", got)
	}

	manual, err := resolver.ResolveExcluding(context.Background(), "review-dynamic", nil)
	if err != nil {
		t.Fatalf("manual ResolveExcluding: %v", err)
	}
	if manual.ProfileID != "sol" || manual.Model != "gpt-6.1-sol" {
		t.Fatalf("manual reviewer = %+v, want the route's first available candidate", manual)
	}
}
