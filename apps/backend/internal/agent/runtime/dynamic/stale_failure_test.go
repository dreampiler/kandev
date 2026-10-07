package dynamic

import (
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/agent/runtime/routingpolicy"
)

// After a failure moved the route from claude to sol, a later failure that
// still names the old generation or candidate belongs to the attempt the route
// already left. It must not suspend sol, the candidate the route now holds.
func TestStaleFailureDoesNotSuspendTheNewerCandidate(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	engine := NewEngine(WithClock(func() time.Time { return now }))
	profile := Profile{
		ID: "dynamic", Version: 1,
		Candidates: []Candidate{
			{ID: "claude", Enabled: true, BindingKey: "claude-account", Policies: routingpolicy.DefaultDocument()},
			{ID: "sol", Enabled: true, BindingKey: "sol-account", Policies: routingpolicy.DefaultDocument()},
			{ID: "nemotron", Enabled: true, BindingKey: "nemotron-account", Policies: routingpolicy.DefaultDocument()},
		},
	}
	rateLimited := &routingerr.Error{
		Code: routingerr.CodeRateLimited, Class: routingerr.ClassTransient,
		Confidence: routingerr.ConfHigh, FallbackAllowed: true, AutoRetryable: true,
	}
	initial, err := engine.Select("session", profile, 0, "")
	if err != nil || initial.ExecutionProfileID != "claude" {
		t.Fatalf("Select = %#v, %v; want claude", initial, err)
	}
	moved, err := engine.ApplyFailure("session", profile, initial.Generation, "claude", rateLimited)
	if err != nil {
		t.Fatalf("ApplyFailure: %v", err)
	}
	if moved.ExecutionProfileID == "claude" || moved.Generation != initial.Generation+1 {
		t.Fatalf("route after claude failure = %#v, want a successor generation", moved)
	}
	successor := moved.ExecutionProfileID

	stale := []struct {
		name       string
		generation int64
		candidate  string
	}{
		{name: "old generation and candidate", generation: initial.Generation, candidate: "claude"},
		{name: "current generation with the old candidate", generation: moved.Generation, candidate: "claude"},
		{name: "old generation with the current candidate", generation: initial.Generation, candidate: successor},
	}
	for _, tc := range stale {
		if _, err := engine.ApplyFailure("session", profile, tc.generation, tc.candidate, rateLimited); !errors.Is(err, ErrStaleGeneration) {
			t.Fatalf("%s: ApplyFailure error = %v, want %v", tc.name, err, ErrStaleGeneration)
		}
	}
	for _, candidate := range profile.Candidates[1:] {
		if got := engine.SuspensionFor(candidate, now); got.State != ResourceAvailable {
			t.Fatalf("stale failure suspended %s: %#v", candidate.ID, got)
		}
	}
	state, exists, err := engine.stateForFailure(t.Context(), "session")
	if err != nil || !exists || state.Generation != moved.Generation || state.ExecutionProfileID != successor {
		t.Fatalf("route state = %#v, %v, %v; want generation %d on %s", state, exists, err, moved.Generation, successor)
	}
}
