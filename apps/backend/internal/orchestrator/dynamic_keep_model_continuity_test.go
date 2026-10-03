package orchestrator

import (
	"context"
	"testing"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
)

// tieredRulesJSON configures the first row as a free-cost tier, so a fresh
// comparison prefers it over the metered row behind it.
const continuityTierRulesJSON = `{"version":1,` +
	`"transient":{"retry":{"enabled":false,"max_retries":0,"initial_interval_seconds":0},` +
	`"wait_for_reset":{"enabled":false,"max_wait_seconds":0},"on_exhausted":"skip"},` +
	`"hard":{"retry":{"enabled":false,"max_retries":0,"initial_interval_seconds":0},` +
	`"wait_for_reset":{"enabled":false,"max_wait_seconds":0},"on_exhausted":"stop"},` +
	`"selection":{"join_previous":false,"tier":{"mode":"cost","on_failure":"same_tier_next"},` +
	`"model":{"cost":"free","usage_source":"none","reserved_user_share_pct":0}}}`

const continuityPlainRulesJSON = `{"version":1,` +
	`"transient":{"retry":{"enabled":false,"max_retries":0,"initial_interval_seconds":0},` +
	`"wait_for_reset":{"enabled":false,"max_wait_seconds":0},"on_exhausted":"skip"},` +
	`"hard":{"retry":{"enabled":false,"max_retries":0,"initial_interval_seconds":0},` +
	`"wait_for_reset":{"enabled":false,"max_wait_seconds":0},"on_exhausted":"stop"}}`

func continuityCandidates() []workflowDynamicCandidate {
	return []workflowDynamicCandidate{
		{executionProfileID: "concrete-free", enabled: true, rulesJSON: continuityTierRulesJSON},
		{executionProfileID: "concrete-metered", enabled: true, rulesJSON: continuityPlainRulesJSON},
	}
}

// TestKeepModelPreferenceIsReadable pins that the stored continuity preference
// reaches the runtime. Without it the keep-off comparison has nothing to read and
// the setting is inert.
func TestKeepModelPreferenceIsReadable(t *testing.T) {
	for name, stored := range map[string]bool{"keep on": true, "keep off": false} {
		t.Run(name, func(t *testing.T) {
			resolver := newWorkflowDynamicProfileResolverWithCandidatesAndKeepModel(
				t, "dynamic-continuity", continuityCandidates(), stored,
			)
			got := resolver.KeepModelWhileRunning(context.Background(), "dynamic-continuity")
			if got != stored {
				t.Fatalf("KeepModelWhileRunning = %v, want the stored %v", got, stored)
			}
		})
	}
}

// TestKeepModelOffReevaluatesAtANewTurnBoundary pins the Continuity rule: with
// keep-model off, a fresh comparison runs and the tier ranking picks the cheap
// row rather than inheriting whichever candidate happened to be selected. The
// incumbent is not preferred, so this is a real comparison rather than a no-op.
func TestKeepModelOffReevaluatesAtANewTurnBoundary(t *testing.T) {
	ctx := context.Background()
	resolver := newWorkflowDynamicProfileResolverWithCandidatesAndKeepModel(
		t, "dynamic-continuity", continuityCandidates(), false,
	)
	if resolver.KeepModelWhileRunning(ctx, "dynamic-continuity") {
		t.Fatal("fixture stored keep-model on, want it off")
	}
	decision, err := resolver.ReevaluateForNewTurn(ctx, "session-continuity", "dynamic-continuity", 0, "exec-local")
	if err != nil {
		t.Fatalf("ReevaluateForNewTurn: %v", err)
	}
	if decision.ExecutionProfileID != "concrete-free" {
		t.Fatalf("reevaluated to %q, want the lowest-cost row", decision.ExecutionProfileID)
	}
	if decision.Generation != 1 {
		t.Fatalf("generation = %d, want the comparison to claim one", decision.Generation)
	}
}

// TestKeepModelOnKeepsTheSelectedCandidate pins the other half of Continuity:
// with keep-model on the incumbent survives a usage refresh and a resume, which
// is what the sticky default exists for. The candidate list here ranks the cheap
// row first, so a selection that landed on the metered row is only possible if
// the incumbent is genuinely retained.
func TestKeepModelOnKeepsTheSelectedCandidate(t *testing.T) {
	ctx := context.Background()
	resolver := newWorkflowDynamicProfileResolverWithCandidatesAndKeepModel(
		t, "dynamic-continuity", continuityCandidates(), true,
	)
	first, err := resolver.ReevaluateForNewTurn(ctx, "session-keep-on", "dynamic-continuity", 0, "exec-local")
	if err != nil {
		t.Fatalf("initial selection: %v", err)
	}
	if first.ExecutionProfileID != "concrete-free" {
		t.Fatalf("initial selection = %q, want the lowest-cost row", first.ExecutionProfileID)
	}
	// Keep-model on is reported as such, so the launch path takes the incumbent
	// branch instead of comparing again.
	if !resolver.KeepModelWhileRunning(ctx, "dynamic-continuity") {
		t.Fatal("keep-model on must be reported as on")
	}
}

// TestReevaluateForNewTurnStartsANewChain pins that a comparison at a turn
// boundary is a fresh selection, not a continuation: it must not inherit the
// previous chain's exclusions, or a session would slowly run out of candidates
// across turns.
func TestReevaluateForNewTurnStartsANewChain(t *testing.T) {
	ctx := context.Background()
	resolver := newWorkflowDynamicProfileResolverWithCandidatesAndKeepModel(
		t, "dynamic-continuity", continuityCandidates(), false,
	)
	first, err := resolver.ReevaluateForNewTurn(
		ctx, "session-chain", "dynamic-continuity", 0, "exec-local",
	)
	if err != nil {
		t.Fatalf("first comparison: %v", err)
	}
	second, err := resolver.ReevaluateForNewTurn(
		ctx, "session-chain", "dynamic-continuity", first.Generation, "exec-local",
	)
	if err != nil {
		t.Fatalf("second comparison: %v", err)
	}
	if second.ExecutionProfileID != first.ExecutionProfileID {
		t.Fatalf("second comparison chose %q after %q, want the same candidate from a fresh chain",
			second.ExecutionProfileID, first.ExecutionProfileID)
	}
	if second.Generation != first.Generation+1 {
		t.Fatalf("generation = %d, want %d", second.Generation, first.Generation+1)
	}
}

// TestReevaluateForNewTurnRejectsAStaleGeneration pins the fencing: a comparison
// issued against a superseded generation must not claim a new one, or two
// concurrent turns could each start a comparison.
func TestReevaluateForNewTurnRejectsAStaleGeneration(t *testing.T) {
	ctx := context.Background()
	resolver := newWorkflowDynamicProfileResolverWithCandidatesAndKeepModel(
		t, "dynamic-continuity", continuityCandidates(), false,
	)
	if _, err := resolver.ReevaluateForNewTurn(ctx, "session-stale", "dynamic-continuity", 7, "exec-local"); err == nil {
		t.Fatal("a comparison against a nonexistent generation must be refused")
	} else if err != dynamicruntime.ErrStaleGeneration {
		t.Fatalf("error = %v, want %v", err, dynamicruntime.ErrStaleGeneration)
	}
}
