package dynamic

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

// IneligibleProbing marks a candidate whose expired suspension is being probed
// by another selection right now.
const IneligibleProbing = "circuit_probing"

// RecordResourceFailure suspends the failed candidate's resource without
// changing the route. It covers a failure that is not routed to a successor,
// such as a usage limit reported after the attempt already produced output, so
// the next selection still avoids the exhausted resource.
func (e *Engine) RecordResourceFailure(ctx context.Context, profile Profile, candidateID string, failure *routingerr.Error) {
	if e == nil {
		return
	}
	e.openCircuitForFailure(ctx, profile, candidateID, failure)
}

// RecordResourceSuccess clears the suspension history of a candidate whose
// attempt produced real output.
func (e *Engine) RecordResourceSuccess(candidate Candidate) {
	if e == nil || e.circuits == nil {
		return
	}
	for _, key := range candidate.resourceKeys() {
		e.circuits.RecordSuccess(key)
	}
}

// CandidateSuspension is the read-only resource health of one candidate.
type CandidateSuspension struct {
	State  ResourceState
	Until  time.Time
	Scope  SuspensionScope
	Source SuspensionSource
	Code   routingerr.Code
}

// Blocked reports whether the candidate cannot be selected now.
func (s CandidateSuspension) Blocked() bool {
	return s.State == ResourceWaiting || s.State == ResourceProbing
}

// SuspensionFor reports a candidate's resource health without claiming
// anything. A running block outranks a held probe, which outranks an expired
// block; among equal states the later end wins.
func (e *Engine) SuspensionFor(candidate Candidate, now time.Time) CandidateSuspension {
	result := CandidateSuspension{State: ResourceAvailable}
	if candidate.SuspendedUntil.After(now) {
		result = CandidateSuspension{
			State: ResourceWaiting, Until: candidate.SuspendedUntil,
			Scope: SuspensionScopeProvider, Source: SuspensionSourceManual,
		}
	}
	if e == nil || e.circuits == nil {
		return result
	}
	for _, key := range candidate.resourceKeys() {
		status := e.circuits.Inspect(key, now)
		if status.State == ResourceAvailable {
			continue
		}
		scope := SuspensionScopeCredential
		if key == candidate.ModelKey {
			scope = SuspensionScopeModel
		}
		next := CandidateSuspension{
			State: status.State, Until: status.Until, Scope: scope,
			Source: SuspensionSourceFailure, Code: status.Code,
		}
		if suspensionRank(next.State) > suspensionRank(result.State) ||
			(next.State == result.State && next.Until.After(result.Until)) {
			result = next
		}
	}
	return result
}

func suspensionRank(state ResourceState) int {
	switch state {
	case ResourceWaiting:
		return 3
	case ResourceProbing:
		return 2
	case ResourceExpired:
		return 1
	default:
		return 0
	}
}

// SetResourceWaitObserver registers the callback told about a selection that
// waits for suspended resources.
func (e *Engine) SetResourceWaitObserver(observer func(sessionID string, generation int64, deadline time.Time)) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.resourceWait = observer
	e.mu.Unlock()
}

// resourceWaitDeadline returns the earliest instant a suspended candidate can
// be tried again, only when every enabled candidate is suspended. A candidate
// excluded for another reason, such as an unclassified failure earlier in the
// chain, keeps the wait manual because time alone does not make it usable.
func (e *Engine) resourceWaitDeadline(profile Profile, now time.Time) (time.Time, bool) {
	var deadline time.Time
	enabled := 0
	for _, candidate := range profile.Candidates {
		if !candidate.Enabled {
			continue
		}
		enabled++
		suspension := e.SuspensionFor(candidate, now)
		if !suspension.Blocked() || suspension.Until.IsZero() {
			return time.Time{}, false
		}
		if deadline.IsZero() || suspension.Until.Before(deadline) {
			deadline = suspension.Until
		}
	}
	return deadline, enabled > 0
}

// withResourceWait marks an exhausted waiting state with the instant its
// suspended candidates can be tried again.
func withResourceWait(policyStateJSON string, deadline time.Time) (string, error) {
	state := PolicyState{}
	if policyStateJSON != "" {
		if err := json.Unmarshal([]byte(policyStateJSON), &state); err != nil {
			return "", fmt.Errorf("decode dynamic policy state: %w", err)
		}
	}
	deadline = deadline.UTC()
	state.Deadline = &deadline
	state.ResourceWait = true
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("encode dynamic policy state: %w", err)
	}
	return string(encoded), nil
}
