package dynamic

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// SelectionChain is the durable record of one transition chain: the admitted
// attempt plus its automatic retries and candidate changes. It is bounded by
// the candidate list, identifies candidates by concrete profile ID rather than
// by a derived badge number, and survives retry, skip and restart so no
// automatic transition returns to an earlier tier or a candidate already tried
// in the same chain.
type SelectionChain struct {
	Version           int      `json:"version"`
	LogicalProfileID  string   `json:"logical_profile_id"`
	StartGeneration   int64    `json:"start_generation"`
	TriedCandidateIDs []string `json:"tried_candidate_ids"`
	LastTierHeadID    string   `json:"last_tier_head_id"`
}

const selectionChainVersion = 1

func (c SelectionChain) valid() bool {
	return c.Version == selectionChainVersion && c.LogicalProfileID != "" && c.StartGeneration > 0
}

func (c SelectionChain) tried() map[string]bool {
	tried := make(map[string]bool, len(c.TriedCandidateIDs))
	for _, id := range c.TriedCandidateIDs {
		tried[id] = true
	}
	return tried
}

// withTried appends one candidate, keeping the list duplicate-free so a
// same-candidate retry cannot grow it.
func (c SelectionChain) withTried(candidateID string) SelectionChain {
	if candidateID == "" || c.tried()[candidateID] {
		return c
	}
	c.TriedCandidateIDs = append(append([]string{}, c.TriedCandidateIDs...), candidateID)
	return c
}

func decodeSelectionChain(policyStateJSON string) (SelectionChain, error) {
	chain := SelectionChain{}
	if policyStateJSON == "" {
		return chain, nil
	}
	var state PolicyState
	if err := json.Unmarshal([]byte(policyStateJSON), &state); err != nil {
		return chain, fmt.Errorf("decode dynamic policy state: %w", err)
	}
	if state.SelectionChain == nil {
		return chain, nil
	}
	return *state.SelectionChain, nil
}

// carrySelectionChain writes a chain into a policy-state document without
// disturbing the failure-policy fields already in it.
func carrySelectionChain(policyStateJSON string, chain SelectionChain) (string, error) {
	state := PolicyState{}
	if policyStateJSON != "" {
		if err := json.Unmarshal([]byte(policyStateJSON), &state); err != nil {
			return "", fmt.Errorf("decode dynamic policy state: %w", err)
		}
	}
	if !chain.valid() {
		state.SelectionChain = nil
	} else {
		state.SelectionChain = &chain
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("encode dynamic policy state: %w", err)
	}
	return string(encoded), nil
}

// UsageSnapshotProvider supplies a bounded per-candidate usage observation for
// one decision. It is called outside the engine lock and returns a failure as
// unknown usage rather than as zero usage or unlimited capacity.
type UsageSnapshotProvider interface {
	UsageSnapshot(ctx context.Context, profile Profile) (map[string]PaceScore, error)
}

// WithUsageSnapshotProvider injects the usage observation used by pace and cost
// ranking and by the settings preview.
func WithUsageSnapshotProvider(provider UsageSnapshotProvider) EngineOption {
	return func(engine *Engine) { engine.usage = provider }
}

// usageSnapshot fetches outside the engine lock. A failed refresh degrades to
// unknown usage, which ranks after known pace instead of winning it.
func (e *Engine) usageSnapshot(ctx context.Context, profile Profile) map[string]PaceScore {
	if e.usage == nil {
		return nil
	}
	scores, err := e.usage.UsageSnapshot(ctx, profile)
	if err != nil {
		return nil
	}
	return scores
}

// TierSearchOrder returns the tiers to try, in order, for a decision. A fresh
// selection walks every tier in list order. A fallback starts at the tier that
// owns the current candidate and never returns to an earlier tier.
//
// next_tier_on_failure skips the remaining peers of the current tier entirely;
// same_tier_next keeps the tier and moves on only once it is exhausted.
func TierSearchOrder(tiers []Tier, currentCandidateID string, direction FailureDirection) []Tier {
	if currentCandidateID == "" {
		return tiers
	}
	index := tierIndexOf(tiers, currentCandidateID)
	if index < 0 {
		return tiers
	}
	if direction == FailureNextTier {
		return tiers[index+1:]
	}
	return tiers[index:]
}

func tierIndexOf(tiers []Tier, candidateID string) int {
	for index, tier := range tiers {
		for _, candidate := range tier.Candidates {
			if candidate.ID == candidateID {
				return index
			}
		}
	}
	return -1
}

// Route reason codes are a bounded set. They name the selection rule and, for a
// fallback, the direction that was taken. They never embed a credential, a
// provider response body or a prompt.
const (
	ReasonTierOrder = "tier_order"
	ReasonTierPace  = "tier_pace"
	ReasonTierCost  = "tier_cost"

	ReasonSuffixSameTier = "_same_tier"
	ReasonSuffixNextTier = "_next_tier"
)

// RouteReason returns the persisted reason code for one selection.
//
// Only a configured tier produces a tier code. A row with no stored tier
// metadata is a legacy or default row, and its established reason code is left
// untouched so existing consumers keep working.
func RouteReason(policy TierPolicy, fallback bool) (string, bool) {
	if policy.Mode == "" {
		return "", false
	}
	base := ReasonTierOrder
	switch policy.Mode {
	case TierModePace:
		base = ReasonTierPace
	case TierModeCost:
		base = ReasonTierCost
	case TierModeOrder:
	default:
		return "", false
	}
	if !fallback {
		return base, true
	}
	if policy.OnFailure == FailureNextTier {
		return base + ReasonSuffixNextTier, true
	}
	return base + ReasonSuffixSameTier, true
}

// selectionPlan is one decision's resolved search space: which tiers to try, and
// which candidates are already spent in this transition chain.
type selectionPlan struct {
	tiers      []Tier
	excluded   map[string]bool
	ineligible map[string]string
	// chain is the transition chain this decision continues. A fresh selection
	// carries an empty chain, because a new user turn is a new chain rather than
	// a continuation of the previous one's exclusions.
	chain SelectionChain
	// fallback marks a permitted failure transition, so a tier reason carries its
	// direction suffix.
	fallback bool
	// direction is the fallback direction that governed this decision, taken
	// from the failed candidate's tier. The persisted reason records the
	// direction that was actually applied rather than the successor's own
	// configured policy.
	direction FailureDirection
}

// selectionReason prefers the bounded tier code for a configured tier and
// otherwise keeps the caller's established reason, so a legacy or default row
// does not change the codes existing consumers already read.
//
// The rule part names how the successor was chosen; the suffix names the
// fallback direction that produced the transition.
func selectionReason(candidate Candidate, tier Tier, plan selectionPlan, fallbackReason string) string {
	if candidate.Selection.Tier == nil {
		return fallbackReason
	}
	code, ok := RouteReason(TierPolicy{Mode: tier.Policy.Mode, OnFailure: plan.direction}, plan.fallback)
	if !ok {
		return fallbackReason
	}
	return code
}

// ResolveSelection builds the search space for a fresh selection: every tier in
// list order, with the caller's exclusion applied. It starts a new transition
// chain, so a previous chain's exclusions never leak into a new attempt.
func ResolveSelection(
	profile Profile,
	excludedCandidateID string,
	preferredCandidateID string,
	eligibility map[string]string,
) selectionPlan {
	excluded := map[string]bool{}
	if excludedCandidateID != "" {
		excluded[excludedCandidateID] = true
	}
	if preferredCandidateID != "" {
		delete(excluded, preferredCandidateID)
	}
	return selectionPlan{
		tiers:      DeriveTiers(profile.Candidates),
		excluded:   excluded,
		ineligible: eligibility,
	}
}

// ResolveFallbackSelection builds the search space for a permitted failure
// transition. It continues the durable chain, starts at the tier that owns the
// failed candidate, and never returns to an earlier tier:
// next_tier_on_failure skips the remaining peers of that tier, same_tier_next
// keeps it and moves on only once it is exhausted.
//
// A preferred candidate is a permitted same-candidate retry. It is not a new
// cross-candidate selection, so the chain's cross-candidate exclusion does not
// apply to it, but the chain itself is neither reset nor advanced past it.
func ResolveFallbackSelection(
	profile Profile,
	failedCandidateID string,
	preferredCandidateID string,
	chain SelectionChain,
	eligibility map[string]string,
) selectionPlan {
	tiers := DeriveTiers(profile.Candidates)
	direction := FailureSameTierNext
	if tier := tierContaining(tiers, failedCandidateID); tier != nil {
		direction = tier.Policy.OnFailure
	}
	excluded := chain.tried()
	if failedCandidateID != "" {
		excluded[failedCandidateID] = true
	}
	if preferredCandidateID != "" {
		delete(excluded, preferredCandidateID)
	}
	return selectionPlan{
		tiers:      TierSearchOrder(tiers, failedCandidateID, direction),
		excluded:   excluded,
		ineligible: eligibility,
		chain:      chain,
		fallback:   true,
		direction:  direction,
	}
}

func tierContaining(tiers []Tier, candidateID string) *Tier {
	index := tierIndexOf(tiers, candidateID)
	if index < 0 {
		return nil
	}
	return &tiers[index]
}

// firstSelectable returns the winning candidate, walking tiers in order and
// ranking only the first tier that still has one.
//
// claim is the caller's atomic admission check. A candidate it refuses was lost
// a race for its circuit probe, which is contention rather than a decision: the
// candidate is not marked tried and the ranking simply continues, so a losing
// probe never costs the chain a candidate.
func (p selectionPlan) firstSelectable(
	scores map[string]PaceScore,
	now time.Time,
	claim func(Candidate) bool,
) (Candidate, Tier, bool) {
	ineligible := make(map[string]string, len(p.ineligible))
	for id, reason := range p.ineligible {
		ineligible[id] = reason
	}
	excluded := make(map[string]bool, len(p.excluded))
	for id := range p.excluded {
		excluded[id] = true
	}
	for _, tier := range p.tiers {
		for {
			ranked := RankTier(tier, RankOptions{
				Now: now, Scores: scores, Eligible: ineligible, Excluded: excluded,
			})
			winner, ok := FirstEligible(ranked)
			if !ok {
				break
			}
			if claim == nil || claim(winner.Candidate) {
				return winner.Candidate, tier, true
			}
			ineligible[winner.Candidate.ID] = IneligibleCircuit
		}
	}
	return Candidate{}, Tier{}, false
}
