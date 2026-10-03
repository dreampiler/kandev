package dynamic

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/agent/runtime/routingpolicy"
)

// unclassifiedTierDocument is the smallest unclassified policy this path admits:
// the threshold must be at least two, so reaching the automatic successor takes
// two counted failures with a manual retry between them.
func unclassifiedTierDocument() routingpolicy.Document {
	document := routingpolicy.DefaultDocument()
	document.Unclassified = &routingpolicy.UnclassifiedPolicy{Enabled: true, ConsecutiveFailureThreshold: 2}
	return document
}

// tieredUnclassifiedProfile builds three rows where candidate-a heads a tier
// joined by candidate-b, and candidate-c is a second tier. The direction decides
// whether the successor is the joined peer or the following tier.
func tieredUnclassifiedProfile(direction FailureDirection) Profile {
	document := unclassifiedTierDocument()
	tier := &TierPolicy{Mode: TierModeOrder, OnFailure: direction}
	noUsage := ModelOptions{UsageSource: UsageNone}
	return Profile{
		ID: "dynamic-unclassified", Version: 4,
		Candidates: []Candidate{
			{ID: "candidate-a", Enabled: true, Policies: document,
				Selection: Selection{Tier: tier, Model: noUsage}},
			{ID: "candidate-b", Enabled: true, Policies: document,
				Selection: Selection{JoinPrevious: true, Model: noUsage}},
			{ID: "candidate-c", Enabled: true, Policies: document,
				Selection: Selection{Tier: tier, Model: noUsage}},
		},
	}
}

// reachUnclassifiedSuccessor drives candidate-a to the threshold with the manual
// retry the existing policy requires, and returns the automatic successor.
func reachUnclassifiedSuccessor(
	t *testing.T,
	engine *Engine,
	profile Profile,
	candidateID string,
) RouteDecision {
	t.Helper()
	decision, err := engine.Select("session-unclassified", profile, 0, "")
	if err != nil {
		t.Fatalf("initial select: %v", err)
	}
	if err := engine.MarkActive(context.Background(), "session-unclassified", decision.Generation); err != nil {
		t.Fatalf("mark active: %v", err)
	}
	current := candidateID
	for attempt := 1; attempt <= 2; attempt++ {
		failure := &routingerr.Error{
			Code: routingerr.CodeUnknownProvider, Class: routingerr.ClassUnclassified,
			Phase: routingerr.PhasePromptSend,
		}
		evidence := safePromptEvidence(decision.Generation, attemptLabel(attempt), "provider returned an unsupported response")
		evidence.ExecutionProfileID = current
		decision, err = engine.ApplyUnclassifiedFailureContext(
			context.Background(), "session-unclassified", profile,
			decision.Generation, current, failure, evidence,
		)
		if attempt < 2 {
			if err == nil {
				t.Fatalf("attempt %d succeeded below the threshold", attempt)
			}
			decision, err = engine.SelectContextWithPreference(
				context.Background(), "session-unclassified", profile,
				decision.Generation, "", current,
			)
			if err != nil {
				t.Fatalf("manual retry after attempt %d: %v", attempt, err)
			}
			if err := engine.MarkActive(context.Background(), "session-unclassified", decision.Generation); err != nil {
				t.Fatalf("mark active after attempt %d: %v", attempt, err)
			}
		}
	}
	if err != nil {
		t.Fatalf("unclassified fallback at the threshold: %v", err)
	}
	return decision
}

func attemptLabel(attempt int) string {
	if attempt == 1 {
		return "attempt-1"
	}
	return "attempt-2"
}

// TestUnclassifiedFallbackHonorsTheTierDirection pins that the unclassified
// automatic successor consumes the same tier selector as every other fallback.
// A next_tier tier must skip its own joined peer rather than taking it, and the
// persisted reason must name the direction that produced the transition.
func TestUnclassifiedFallbackHonorsTheTierDirection(t *testing.T) {
	for name, direction := range map[string]FailureDirection{
		"next tier": FailureNextTier, "same tier next": FailureSameTierNext,
	} {
		t.Run(name, func(t *testing.T) {
			profile := tieredUnclassifiedProfile(direction)
			engine := NewEngine()
			next := reachUnclassifiedSuccessor(t, engine, profile, "candidate-a")

			want := "candidate-b"
			wantReason := ReasonTierOrder + ReasonSuffixSameTier
			if direction == FailureNextTier {
				want = "candidate-c"
				wantReason = ReasonTierOrder + ReasonSuffixNextTier
			}
			if next.ExecutionProfileID != want {
				t.Fatalf("successor = %q, want %q for %s", next.ExecutionProfileID, want, direction)
			}
			if next.Reason != wantReason {
				t.Fatalf("reason = %q, want %q", next.Reason, wantReason)
			}

			// The successor joins the durable chain, so a later transition in this
			// chain cannot come back to it. The rows the user already replaced
			// through manual recovery are not in it: that retry started a new
			// chain by design.
			state, ok := engine.State("session-unclassified")
			if !ok {
				t.Fatal("missing route state")
			}
			var policyState PolicyState
			if err := json.Unmarshal([]byte(state.PolicyStateJSON), &policyState); err != nil {
				t.Fatalf("decode policy state: %v", err)
			}
			if policyState.SelectionChain == nil {
				t.Fatal("the unclassified successor dropped the transition chain")
			}
			tried := map[string]bool{}
			for _, id := range policyState.SelectionChain.TriedCandidateIDs {
				tried[id] = true
			}
			if !tried[want] {
				t.Fatalf("chain = %v, want the successor recorded as tried",
					policyState.SelectionChain.TriedCandidateIDs)
			}
		})
	}
}

// TestUnclassifiedFallbackExcludesAlreadyTriedCandidates pins that the chain
// survives the unclassified path: an exhausted chain holds for manual recovery
// instead of walking back into a row already tried in this chain.
func TestUnclassifiedFallbackExcludesAlreadyTriedCandidates(t *testing.T) {
	document := unclassifiedTierDocument()
	profile := Profile{
		ID: "dynamic-unclassified", Version: 4,
		Candidates: []Candidate{
			{ID: "candidate-a", Enabled: true, Policies: document},
			{ID: "candidate-b", Enabled: true, Policies: document},
		},
	}
	engine := NewEngine()
	next := reachUnclassifiedSuccessor(t, engine, profile, "candidate-a")
	if next.ExecutionProfileID != "candidate-b" {
		t.Fatalf("successor = %q, want candidate-b", next.ExecutionProfileID)
	}
	if err := engine.MarkActive(context.Background(), "session-unclassified", next.Generation); err != nil {
		t.Fatalf("mark successor active: %v", err)
	}

	// Both rows are now in the chain, so a further counted failure has nowhere
	// left to go and must hold for manual recovery.
	retry := func(attempt int) (RouteDecision, error) {
		failure := &routingerr.Error{
			Code: routingerr.CodeUnknownProvider, Class: routingerr.ClassUnclassified,
			Phase: routingerr.PhasePromptSend,
		}
		evidence := safePromptEvidence(next.Generation, attemptLabel(attempt), "provider returned an unsupported response")
		evidence.ExecutionProfileID = "candidate-b"
		return engine.ApplyUnclassifiedFailureContext(
			context.Background(), "session-unclassified", profile,
			next.Generation, "candidate-b", failure, evidence,
		)
	}
	if _, err := retry(1); err == nil {
		t.Fatal("the first failure below the threshold must hold for manual recovery")
	}
	decision, err := engine.SelectContextWithPreference(
		context.Background(), "session-unclassified", profile, next.Generation, "", "candidate-b",
	)
	if err != nil {
		t.Fatalf("manual retry: %v", err)
	}
	if err := engine.MarkActive(context.Background(), "session-unclassified", decision.Generation); err != nil {
		t.Fatalf("mark active after retry: %v", err)
	}
	next = decision
	failure := &routingerr.Error{
		Code: routingerr.CodeUnknownProvider, Class: routingerr.ClassUnclassified,
		Phase: routingerr.PhasePromptSend,
	}
	evidence := safePromptEvidence(next.Generation, "attempt-2", "provider returned an unsupported response")
	evidence.ExecutionProfileID = "candidate-b"
	if _, err := engine.ApplyUnclassifiedFailureContext(
		context.Background(), "session-unclassified", profile,
		next.Generation, "candidate-b", failure, evidence,
	); err == nil {
		t.Fatal("an exhausted chain must not select another candidate")
	}
	state, ok := engine.State("session-unclassified")
	if !ok {
		t.Fatal("missing route state")
	}
	if state.ExecutionProfileID != "candidate-b" {
		t.Fatalf("an exhausted chain moved to %q instead of holding", state.ExecutionProfileID)
	}
}
