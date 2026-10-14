package dynamic

import "context"

// WithTurnAdmission serializes the final prompt claim with route selection.
// The callback commits RUNNING before another account claim can inspect it.
func (e *Engine) WithTurnAdmission(ctx context.Context, profile Profile, current RouteDecision, claim func() error) error {
	checker, configured := e.usage.(AdmissionChecker)
	if !configured {
		return claim()
	}
	candidate, found := candidateByID(profile, current.ExecutionProfileID)
	if !found {
		return ErrNoEligibleCandidate
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	state, exists, err := e.loadStateLocked(ctx, current.SessionID)
	if err != nil {
		return err
	}
	if !exists || state.Generation != current.Generation {
		return ErrStaleGeneration
	}
	score, err := checker.CheckAdmission(ctx, candidate, current.SessionID, e.now())
	if err != nil || score.AdmissionReason == "" {
		return claim()
	}
	return e.deferTurnAdmission(ctx, profile, state, score)
}

func (e *Engine) deferTurnAdmission(ctx context.Context, profile Profile, state RouteState, score PaceScore) error {
	oldStatus := state.Status
	policy, err := withResourceWait(state.PolicyStateJSON, score.AdmissionRetryAt)
	if err != nil {
		return err
	}
	state.Status = string(ResourceWaiting)
	state.PolicyStateJSON = policy
	state.UpdatedAt = e.now()
	if err := e.persistExpectedStatus(ctx, state.Generation, oldStatus, state); err != nil {
		return err
	}
	e.states[state.SessionID] = state
	if e.resourceWait != nil {
		e.resourceWait(state.SessionID, state.Generation, score.AdmissionRetryAt)
	}
	return &NoEligibleCandidateError{SessionID: state.SessionID, LogicalProfile: profile.ID, Generation: state.Generation, ResourceWait: true, RetryAt: score.AdmissionRetryAt}
}
