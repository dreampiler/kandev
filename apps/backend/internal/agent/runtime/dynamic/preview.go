package dynamic

import "time"

// SelectionPreview is the read-only explanation of one predicted selection. It
// answers for a new selection, so it is never a promise to switch an active
// session, and it mutates nothing: no generation claim, no probe lease, no
// circuit and no launch.
type SelectionPreview struct {
	// Available is false when there is nothing to predict at all, for example
	// an empty candidate list. Callers distinguish that from "no eligible
	// candidate", which is Available with no winner.
	Available   bool
	CandidateID string
	TierIndex   int
	TierHeadID  string
	Mode        TierMode
	// Reason is the bounded selection code, matching what an attempt persists.
	Reason string
	Score  PaceScore
	// Considered is every candidate that was ranked, in tier order, so a
	// preview can explain why the winner won.
	Considered []PreviewEntry
	// ObservedAt is the single observation instant used for the whole preview.
	ObservedAt time.Time
}

// PreviewEntry is one ranked candidate's contribution to a preview.
type PreviewEntry struct {
	CandidateID      string
	TierIndex        int
	Selected         bool
	Eligible         bool
	IneligibleReason string
	Score            PaceScore
	CostClass        CostClass
	// Suspension is the candidate's resource health at ObservedAt. An expired
	// suspension leaves the candidate eligible: the next selection retries it.
	Suspension CandidateSuspension
}

// WithSuspensions attaches each candidate's resource health to the preview.
func (p SelectionPreview) WithSuspensions(suspensions map[string]CandidateSuspension) SelectionPreview {
	for index := range p.Considered {
		if suspension, ok := suspensions[p.Considered[index].CandidateID]; ok {
			p.Considered[index].Suspension = suspension
		}
	}
	return p
}

// PreviewState separates the states a preview must distinguish rather than
// collapsing them into one "nothing available" answer.
type PreviewState string

const (
	// PreviewUnavailable means the prediction itself could not be produced,
	// for example no candidates were supplied.
	PreviewUnavailable PreviewState = "unavailable"
	// PreviewNoCandidates means the list is empty.
	PreviewNoCandidates PreviewState = "no_candidates"
	// PreviewNoEligible means every candidate was filtered out, and the reasons
	// say why.
	PreviewNoEligible PreviewState = "no_eligible_candidate"
	// PreviewReady means a candidate was predicted.
	PreviewReady PreviewState = "ready"
)

// PreviewSelection runs the same plan and the same pure ranking the engine uses
// for a selection, but claims nothing. Because both call ResolveSelection and
// RankTier against the same inputs, preview and actual selection agree when the
// profile, clock, health and usage inputs match.
//
// currentCandidateID and the chain describe an in-progress transition; passing
// empty values previews a fresh selection.
func PreviewSelection(
	profile Profile,
	scores map[string]PaceScore,
	ineligible map[string]string,
	currentCandidateID string,
	chain SelectionChain,
	now time.Time,
) SelectionPreview {
	return PreviewSelectionWith(profile, RankOptions{Scores: scores}, ineligible, currentCandidateID, chain, now)
}

// PreviewSelectionWith previews with the same random source and round-robin
// history a live selection uses. A random choice is a prediction of one draw,
// not a promise that the next session lands on the same candidate.
func PreviewSelectionWith(
	profile Profile,
	inputs RankOptions,
	ineligible map[string]string,
	currentCandidateID string,
	chain SelectionChain,
	now time.Time,
) SelectionPreview {
	// The preview must resolve the same plan the engine would for the same
	// inputs, so a fresh preview walks every tier and carries no fallback
	// direction, while a preview of an in-progress transition carries both.
	var plan selectionPlan
	if currentCandidateID == "" {
		plan = ResolveSelection(profile, "", "", ineligible)
	} else {
		plan = ResolveFallbackSelection(profile, currentCandidateID, "", chain, ineligible)
	}
	preview := SelectionPreview{ObservedAt: now}
	if len(profile.Candidates) == 0 {
		return preview
	}
	preview.Available = true
	inputs = inputs.at(now)
	ranked := plan.firstSelectable(inputs, nil)
	for _, tier := range plan.tiers {
		options := inputs
		options.Eligible, options.Excluded = ineligible, plan.excluded
		entries := RankTier(tier, options)
		for _, entry := range entries {
			preview.Considered = append(preview.Considered, PreviewEntry{
				CandidateID:      entry.Candidate.ID,
				TierIndex:        tier.Index,
				Selected:         ranked.ok && entry.Candidate.ID == ranked.candidate.ID,
				Eligible:         entry.Eligible,
				IneligibleReason: entry.IneligibleReason,
				Score:            entry.Score,
				CostClass:        entry.Candidate.Selection.Model.Cost,
			})
		}
	}
	if !ranked.ok {
		return preview
	}
	preview.CandidateID = ranked.candidate.ID
	preview.TierIndex = ranked.tier.Index
	preview.TierHeadID = ranked.tier.HeadID
	preview.Mode = ranked.tier.Policy.Mode
	preview.Score = ranked.score
	// The same discriminator the engine uses, so a legacy or default tier keeps
	// its established reason on both paths.
	preview.Reason = selectionReason(ranked.candidate, ranked.tier, plan, ReasonTierOrder)
	return preview
}

// State reports the material state a preview UI must render distinctly.
func (p SelectionPreview) State() PreviewState {
	switch {
	case len(p.Considered) == 0 && !p.Available:
		return PreviewNoCandidates
	case !p.Available:
		return PreviewUnavailable
	case p.CandidateID == "":
		return PreviewNoEligible
	default:
		return PreviewReady
	}
}
