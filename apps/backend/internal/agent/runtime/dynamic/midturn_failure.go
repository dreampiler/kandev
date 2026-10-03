package dynamic

import (
	"context"
	"strings"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/agent/runtime/routingpolicy"
)

// InterruptedFailureAllowed is the task interruption exception, separate from
// pre-result retry policy and from account errors requiring user action.
func InterruptedFailureAllowed(failure *routingerr.Error) bool {
	if failure == nil || !failure.FallbackAllowed || failure.UserAction {
		return false
	}
	switch failure.Code {
	case routingerr.CodeQuotaLimited, routingerr.CodeRateLimited, routingerr.CodeProviderUnavailable,
		routingerr.CodeProviderOverloaded, routingerr.CodeModelCapacity:
		return true
	default:
		return false
	}
}

// ApplyInterruptedFailureContext advances an owned route without replaying its
// model. The status claim fences resource penalties as well as successor claims.
func (e *Engine) ApplyInterruptedFailureContext(ctx context.Context, sessionID string, profile Profile, generation int64, candidateID string, failure *routingerr.Error) (RouteDecision, error) {
	if !InterruptedFailureAllowed(failure) {
		return RouteDecision{}, ErrNoEligibleCandidate
	}
	inputs := e.rankInputs(ctx, profile)
	e.mu.Lock()
	defer e.mu.Unlock()
	state, err := e.currentUnclassifiedRouteState(ctx, sessionID, profile, generation, candidateID)
	if err != nil {
		return RouteDecision{}, err
	}
	if state.Status != routeStatusActive && state.Status != routeStatusStarting {
		return RouteDecision{}, ErrStaleGeneration
	}
	chain, err := decodeSelectionChain(state.PolicyStateJSON)
	if err != nil {
		return RouteDecision{}, err
	}
	chain = excludeInterruptedModel(chain.forProfile(profile.ID, generation), profile, candidateID)
	chain.Interrupted = true
	oldStatus, oldPolicy := state.Status, state.PolicyStateJSON
	state.Status = routeStatusRetrying
	state.PolicyStateJSON, err = carrySelectionChain("", chain)
	if err != nil {
		return RouteDecision{}, err
	}
	if err := e.persistSnapshot(ctx, oldStatus, oldPolicy, state); err != nil {
		return RouteDecision{}, err
	}
	e.states[sessionID] = state
	e.RecordResourceFailure(ctx, profile, candidateID, failure)
	e.observeLimit(ctx, candidateID, failure)
	key := probeKey(sessionID, generation, candidateID)
	leases := e.probes[key]
	delete(e.probes, key)
	for _, lease := range leases {
		e.circuits.ReleaseProbe(lease, false, circuitBackoff)
	}
	return e.selectInterruptedSuccessor(ctx, state, profile, candidateID, failure, chain, inputs)
}

func (e *Engine) selectInterruptedSuccessor(ctx context.Context, state RouteState, profile Profile, candidateID string, failure *routingerr.Error, chain SelectionChain, inputs RankOptions) (RouteDecision, error) {
	now := e.now()
	generation := state.Generation + 1
	ineligible := make(map[string]string)
	for _, candidate := range profile.Candidates {
		if !e.candidateSelectable(candidate, state.SessionID, generation, "", "", now) {
			ineligible[candidate.ID] = IneligibleCircuit
		}
	}
	plan := ResolveFallbackSelection(profile, candidateID, "", chain, ineligible)
	winner := plan.firstSelectable(inputs.at(now), func(candidate Candidate) bool {
		return e.claimProbe(candidate, state.SessionID, generation, now)
	})
	policy := PolicyState{SelectionChain: &chain, FailureCode: failure.Code,
		FailureClass: routingerr.ClassForCode(failure.Code), CatalogueVersion: routingerr.CatalogueVersion,
		PendingOutcome: routingpolicy.OutcomeSkip}
	if !winner.ok {
		return e.persistInterruptedExhaustion(ctx, state, policy, failure)
	}
	chain = chain.withTried(winner.candidate.ID)
	chain.LastTierHeadID = winner.tier.HeadID
	policy.SelectionChain = &chain
	decision := RouteDecision{SessionID: state.SessionID, LogicalProfileID: profile.ID,
		ExecutionProfileID: winner.candidate.ID, Generation: generation, ProfileVersion: profile.Version,
		Status: routeStatusStarting, Reason: selectionReason(winner.candidate, winner.tier, plan, "interrupted_try_next"),
		ErrorCode: failure.Code, ErrorClass: policy.FailureClass, CatalogueVersion: routingerr.CatalogueVersion,
		PendingOutcome: routingpolicy.OutcomeSkip}
	next := RouteState{SessionID: state.SessionID, LogicalProfileID: profile.ID,
		ExecutionProfileID: winner.candidate.ID, Generation: generation, ProfileVersion: profile.Version,
		Status: routeStatusStarting, PolicyStateJSON: string(mustJSON(policy)), UpdatedAt: now}
	if err := e.claimAndPersist(ctx, state.Generation, decision, next); err != nil {
		delete(e.states, state.SessionID)
		return RouteDecision{}, err
	}
	e.states[state.SessionID] = next
	e.notePickLocked(profile.ID, winner.candidate.ID, now)
	return decision, nil
}

func (e *Engine) persistInterruptedExhaustion(ctx context.Context, state RouteState, policy PolicyState, failure *routingerr.Error) (RouteDecision, error) {
	oldStatus, oldPolicy := state.Status, state.PolicyStateJSON
	policy.PendingOutcome = routingpolicy.OutcomeStop
	state.Status = routeStatusActionRequired
	state.PolicyStateJSON = string(mustJSON(policy))
	state.UpdatedAt = e.now()
	if err := e.persistSnapshot(ctx, oldStatus, oldPolicy, state); err != nil {
		return RouteDecision{}, err
	}
	e.states[state.SessionID] = state
	return RouteDecision{SessionID: state.SessionID, LogicalProfileID: state.LogicalProfileID,
		ExecutionProfileID: state.ExecutionProfileID, Generation: state.Generation, ProfileVersion: state.ProfileVersion,
		Status: state.Status, Reason: "interrupted_candidates_exhausted", ErrorCode: failure.Code,
		ErrorClass: policy.FailureClass, CatalogueVersion: routingerr.CatalogueVersion,
		PendingOutcome: routingpolicy.OutcomeStop}, ErrRecoveryPending
}

func excludeInterruptedModel(chain SelectionChain, profile Profile, candidateID string) SelectionChain {
	chain = chain.withTried(candidateID)
	failed, ok := candidateByID(profile, candidateID)
	if !ok {
		return chain
	}
	model := interruptionModelIdentity(failed.ModelID)
	if model == "" {
		return chain
	}
	for _, candidate := range profile.Candidates {
		if interruptionModelIdentity(candidate.ModelID) == model {
			chain = chain.withTried(candidate.ID)
		}
	}
	return chain
}

func interruptionModelIdentity(model string) string {
	model = strings.TrimSpace(model)
	if unknownInterruptionModel(model) {
		return ""
	}
	if suffix, ok := strings.CutPrefix(model, "opencode-go/"); ok {
		if unknownInterruptionModel(suffix) {
			return ""
		}
		return "opencode/" + suffix
	}
	if suffix, ok := strings.CutPrefix(model, "opencode/"); ok && unknownInterruptionModel(suffix) {
		return ""
	}
	return model
}

func unknownInterruptionModel(model string) bool {
	return model == "" || model == "default" || model == "unknown"
}
