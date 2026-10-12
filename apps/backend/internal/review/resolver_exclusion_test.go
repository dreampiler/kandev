package review

import (
	"context"
	"errors"
	"testing"
)

// fakeProfilesByID resolves a distinct ReviewerProfile per profile ID, so tests
// can model an origin's concrete execution profile separately from a candidate.
type fakeProfilesByID map[string]ReviewerProfile

func (f fakeProfilesByID) ReviewerProfile(_ context.Context, id string) (ReviewerProfile, bool, error) {
	profile, ok := f[id]
	return profile, ok, nil
}

func TestResolveExcluding_DistinctExplicitProfileWins(t *testing.T) {
	profiles := fakeProfilesByID{
		"p-origin": {AgentID: "claude-acp", Model: "opus", Mode: "acceptEdits"},
		"p-review": {AgentID: "claude-acp", Model: "sonnet", Mode: "acceptEdits"},
	}
	r := NewResolver(profiles, nil, nil)
	origin := &OriginIdentity{ProfileID: "p-origin", ExecutionProfileID: "p-origin"}

	got, err := r.ResolveExcluding(context.Background(), "p-review", origin)
	if err != nil {
		t.Fatalf("ResolveExcluding returned error: %v", err)
	}
	if got.ProfileID != "p-review" || got.Model != "sonnet" {
		t.Fatalf("got %+v, want the distinct explicit profile p-review/sonnet", got)
	}
}

func TestResolveExcluding_ExplicitOriginFallsToUtility(t *testing.T) {
	profiles := fakeProfilesByID{
		"p-origin": {AgentID: "claude-acp", Model: "opus"},
	}
	r := NewResolver(profiles,
		&fakeUtility{found: true, enabled: true, agentID: "codex", model: "gpt"}, nil)
	origin := &OriginIdentity{ProfileID: "p-origin", ExecutionProfileID: "p-origin"}

	got, err := r.ResolveExcluding(context.Background(), "p-origin", origin)
	if err != nil {
		t.Fatalf("ResolveExcluding returned error: %v", err)
	}
	if got.AgentID != "codex" || got.Model != "gpt" {
		t.Fatalf("got %+v, want the distinct utility reviewer codex/gpt", got)
	}
}

func TestResolveExcluding_UtilityOriginFallsToDefaults(t *testing.T) {
	profiles := fakeProfilesByID{
		"p-origin": {AgentID: "claude-acp", Model: "opus"},
	}
	r := NewResolver(profiles,
		&fakeUtility{found: true, enabled: true, agentID: "claude-acp", model: "opus"},
		&fakeDefaults{agentID: "codex", model: "gpt"})
	origin := &OriginIdentity{ExecutionProfileID: "p-origin"}

	got, err := r.ResolveExcluding(context.Background(), "", origin)
	if err != nil {
		t.Fatalf("ResolveExcluding returned error: %v", err)
	}
	if got.AgentID != "codex" || got.Model != "gpt" {
		t.Fatalf("got %+v, want the default reviewer codex/gpt after excluding the utility origin", got)
	}
}

func TestResolveExcluding_AllCandidatesAreOriginFailsClosed(t *testing.T) {
	profiles := fakeProfilesByID{
		"p-origin": {AgentID: "claude-acp", Model: "opus"},
	}
	r := NewResolver(profiles,
		&fakeUtility{found: true, enabled: true, agentID: "claude-acp", model: "opus"},
		&fakeDefaults{agentID: "claude-acp", model: "opus"})
	origin := &OriginIdentity{ProfileID: "p-origin", ExecutionProfileID: "p-origin"}

	_, err := r.ResolveExcluding(context.Background(), "p-origin", origin)
	if !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("got error %v, want ErrAgentUnavailable when no distinct reviewer exists", err)
	}
}

func TestResolveExcluding_TripleMatchWithoutProfileID(t *testing.T) {
	profiles := fakeProfilesByID{
		"p-origin": {AgentID: "claude-acp", Model: "opus"},
		"p-other":  {AgentID: "claude-acp", Model: "opus", Mode: "plan"},
	}
	r := NewResolver(profiles, nil, &fakeDefaults{agentID: "codex", model: "gpt"})
	origin := &OriginIdentity{ExecutionProfileID: "p-origin"}

	got, err := r.ResolveExcluding(context.Background(), "p-other", origin)
	if err != nil {
		t.Fatalf("ResolveExcluding returned error: %v", err)
	}
	if got.AgentID != "codex" || got.Model != "gpt" {
		t.Fatalf("got %+v, want the default reviewer after excluding the same-model profile p-other", got)
	}
}

func TestResolveRequiredOrigin_UnknownOriginFailsClosed(t *testing.T) {
	r := NewResolver(fakeProfilesByID{"p-review": {AgentID: "claude-acp", Model: "sonnet"}},
		&fakeUtility{found: true, enabled: true, agentID: "codex", model: "gpt"},
		&fakeDefaults{agentID: "codex", model: "gpt"})

	_, err := r.ResolveRequiredOrigin(context.Background(), "p-review", nil)
	if !errors.Is(err, ErrAgentUnavailable) || !errors.Is(err, ErrOriginUnreadable) {
		t.Fatalf("got %v, want ErrAgentUnavailable wrapping ErrOriginUnreadable", err)
	}
}

func TestResolveRequiredOrigin_KnownOriginKeepsPrecedence(t *testing.T) {
	r := NewResolver(fakeProfilesByID{
		"p-origin": {AgentID: "claude-acp", Model: "opus"},
		"p-review": {AgentID: "claude-acp", Model: "sonnet"},
	}, nil, nil)

	got, err := r.ResolveRequiredOrigin(context.Background(), "p-review",
		&OriginIdentity{ProfileID: "p-origin"})
	if err != nil {
		t.Fatalf("ResolveRequiredOrigin returned error: %v", err)
	}
	if got.ProfileID != "p-review" {
		t.Fatalf("got %+v, want the distinct explicit profile p-review", got)
	}
}

func TestResolveExcluding_NilOriginKeepsPrecedence(t *testing.T) {
	profiles := fakeProfilesByID{
		"p-review": {AgentID: "claude-acp", Model: "sonnet"},
	}
	r := NewResolver(profiles, &fakeUtility{found: true, enabled: true, agentID: "codex", model: "gpt"}, nil)

	got, err := r.ResolveExcluding(context.Background(), "p-review", nil)
	if err != nil {
		t.Fatalf("ResolveExcluding returned error: %v", err)
	}
	if got.ProfileID != "p-review" {
		t.Fatalf("got %+v, want the explicit profile when no origin is supplied", got)
	}
}
