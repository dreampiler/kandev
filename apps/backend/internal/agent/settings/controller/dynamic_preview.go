package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/settings/dto"
)

// DynamicPreviewProvider computes a read-only current-choice prediction. The
// runtime owns the usage observation and the health verdict, so the settings
// controller depends on this narrow seam rather than on the routing engine.
type DynamicPreviewProvider interface {
	PreviewDynamicSelection(
		ctx context.Context,
		profile dynamic.Profile,
		ineligible map[string]string,
		now time.Time,
	) dynamic.SelectionPreview
}

// DynamicTierPreviewDTO is one candidate's contribution to a preview.
type DynamicTierPreviewDTO struct {
	Position           int     `json:"position"`
	ExecutionProfileID string  `json:"execution_profile_id"`
	TierIndex          int     `json:"tier_index"`
	Selected           bool    `json:"selected"`
	Eligible           bool    `json:"eligible"`
	IneligibleReason   string  `json:"ineligible_reason,omitempty"`
	ControllingWindow  string  `json:"controlling_window,omitempty"`
	UsagePercent       float64 `json:"usage_percent"`
	ElapsedPercent     float64 `json:"elapsed_percent"`
	Pace               float64 `json:"pace"`
	UsageKnown         bool    `json:"usage_known"`
	UsageComplete      bool    `json:"usage_complete"`
	ElapsedFloorUsed   bool    `json:"elapsed_floor_used"`
	CostClass          string  `json:"cost_class,omitempty"`
	ReservedSharePct   int     `json:"reserved_share_pct"`
}

// DynamicPreviewDTO answers "which candidate would be chosen now". It is a
// prediction for a new selection, never a promise to switch an active session,
// and computing it never saves settings, claims a generation, opens a circuit or
// launches an agent.
type DynamicPreviewDTO struct {
	// State separates the states the editor renders differently rather than
	// collapsing them: no_candidates, no_eligible_candidate, ready, unavailable.
	State         string                  `json:"state"`
	CandidateID   string                  `json:"candidate_id,omitempty"`
	TierIndex     int                     `json:"tier_index,omitempty"`
	TierHeadID    string                  `json:"tier_head_id,omitempty"`
	Mode          string                  `json:"mode,omitempty"`
	Reason        string                  `json:"reason,omitempty"`
	ObservedAt    time.Time               `json:"observed_at"`
	Considered    []DynamicTierPreviewDTO `json:"considered"`
	UsageComplete bool                    `json:"usage_complete"`
}

// DynamicPreviewRequest carries an unsaved draft so a create-time preview works
// before the profile exists. ExpectedVersion is advisory: preview never writes,
// so it never enforces the optimistic version.
type DynamicPreviewRequest struct {
	Dynamic *dto.DynamicAgentProfileDTO `json:"dynamic"`
	// ProfileID is the saved profile from the request path, empty for a draft.
	// It lets a round-robin preview continue from the profile's history.
	ProfileID string `json:"-"`
}

// SetDynamicPreviewProvider injects the runtime preview seam.
func (c *Controller) SetDynamicPreviewProvider(provider DynamicPreviewProvider) {
	c.dynamicPreview = provider
}

// PreviewDynamicProfile predicts the current choice for a saved profile or an
// unsaved draft.
//
// The draft is validated exactly as it would be for editing, so an invalid
// draft cannot produce a confident-looking prediction. A provider that cannot
// answer returns an explicit unavailable state rather than a guess.
func (c *Controller) PreviewDynamicProfile(
	ctx context.Context,
	request DynamicPreviewRequest,
) (*DynamicPreviewDTO, error) {
	if request.Dynamic == nil || len(request.Dynamic.Candidates) == 0 {
		return &DynamicPreviewDTO{State: string(dynamic.PreviewNoCandidates)}, nil
	}
	// Validation shares the save path so preview and save accept the same drafts.
	if err := validateDynamicAgentProfile(request.Dynamic); err != nil {
		return nil, err
	}
	if c.dynamicPreview == nil {
		return &DynamicPreviewDTO{State: string(dynamic.PreviewUnavailable)}, nil
	}
	profile := previewProfile(request.Dynamic)
	if id := strings.TrimSpace(request.ProfileID); id != "" {
		profile.ID = id
	}
	preview := c.dynamicPreview.PreviewDynamicSelection(
		ctx, profile, previewEligibility(profile), time.Now(),
	)
	return previewDTO(preview, request.Dynamic.Candidates), nil
}

// previewProfile converts a validated draft into the runtime shape the pure
// ranking needs. A fresh preview never evaluates failure policy, so only the
// candidate identity, enabled flag and selection metadata are carried.
func previewProfile(document *dto.DynamicAgentProfileDTO) dynamic.Profile {
	profile := dynamic.Profile{
		ID:                    documentProfileID(document),
		Version:               document.Version,
		KeepModelWhileRunning: keepModelWhileRunning(document),
		Candidates:            make([]dynamic.Candidate, 0, len(document.Candidates)),
	}
	for index := range document.Candidates {
		candidate := &document.Candidates[index]
		profile.Candidates = append(profile.Candidates, dynamic.Candidate{
			ID:        strings.TrimSpace(candidate.ExecutionProfileID),
			Enabled:   candidate.Enabled,
			Selection: previewSelection(candidate),
		})
	}
	return profile
}

// documentProfileID lets a preview group rows by the saved profile when one
// exists. A create-time draft has no identity yet, and grouping only needs
// stable per-row IDs, so the candidate IDs carry the ranking.
func documentProfileID(document *dto.DynamicAgentProfileDTO) string {
	return "dynamic-preview"
}

func previewSelection(candidate *dto.DynamicAgentCandidateDTO) dynamic.Selection {
	selection := dynamic.Selection{}
	source := candidate.Policies.Selection
	if source == nil {
		return selection
	}
	selection.JoinPrevious = source.JoinPrevious
	if source.Tier != nil {
		selection.Tier = &dynamic.TierPolicy{
			Mode:      dynamic.TierMode(source.Tier.Mode),
			OnFailure: dynamic.FailureDirection(source.Tier.OnFailure),
		}
	}
	selection.Model = dynamic.ModelOptions{
		Cost:                 dynamic.CostClass(source.Model.Cost),
		UsageSource:          dynamic.UsageSource(source.Model.UsageSource),
		ReservedUserSharePct: source.Model.ReservedUserSharePct,
	}
	for _, window := range source.Model.Windows {
		converted := dynamic.UsageWindow{
			Period: window.Period, Unit: window.Unit, Limit: window.Limit, Scope: window.Scope,
		}
		if window.Reset != nil {
			converted.ResetAnchor = window.Reset.Anchor
			converted.Timezone = window.Reset.Timezone
		}
		selection.Model.Windows = append(selection.Model.Windows, converted)
	}
	return selection
}

// previewEligibility reports the rows a preview cannot choose, using the same
// bounded codes the ranking persists.
func previewEligibility(profile dynamic.Profile) map[string]string {
	ineligible := make(map[string]string)
	for _, candidate := range profile.Candidates {
		if !candidate.Enabled {
			ineligible[candidate.ID] = dynamic.IneligibleDisabled
		}
	}
	return ineligible
}

func previewDTO(preview dynamic.SelectionPreview, candidates []dto.DynamicAgentCandidateDTO) *DynamicPreviewDTO {
	positions := make(map[string]int, len(candidates))
	reserved := make(map[string]int, len(candidates))
	for index := range candidates {
		candidate := &candidates[index]
		id := strings.TrimSpace(candidate.ExecutionProfileID)
		positions[id] = index
		if candidate.Policies != nil && candidate.Policies.Selection != nil {
			reserved[id] = candidate.Policies.Selection.Model.ReservedUserSharePct
		}
	}
	result := &DynamicPreviewDTO{
		State: string(preview.State()), CandidateID: preview.CandidateID,
		TierIndex: preview.TierIndex, TierHeadID: preview.TierHeadID,
		Mode: string(preview.Mode), Reason: preview.Reason,
		ObservedAt: preview.ObservedAt, Considered: make([]DynamicTierPreviewDTO, 0, len(preview.Considered)),
	}
	for _, entry := range preview.Considered {
		result.Considered = append(result.Considered, DynamicTierPreviewDTO{
			Position:           positions[entry.CandidateID],
			ExecutionProfileID: entry.CandidateID,
			TierIndex:          entry.TierIndex,
			Selected:           entry.Selected,
			Eligible:           entry.Eligible,
			IneligibleReason:   entry.IneligibleReason,
			ControllingWindow:  entry.Score.Controlling,
			UsagePercent:       entry.Score.UsageFraction * 100,
			ElapsedPercent:     entry.Score.ElapsedFraction * 100,
			Pace:               entry.Score.Pace,
			UsageKnown:         entry.Score.Known,
			UsageComplete:      entry.Score.Complete,
			ElapsedFloorUsed:   entry.Score.FloorApplied,
			CostClass:          string(entry.CostClass),
			ReservedSharePct:   reserved[entry.CandidateID],
		})
	}
	if winner, ok := selectedScore(result.Considered); ok {
		result.UsageComplete = winner.UsageComplete
	}
	return result
}

func selectedScore(considered []DynamicTierPreviewDTO) (DynamicTierPreviewDTO, bool) {
	for _, entry := range considered {
		if entry.Selected {
			return entry, true
		}
	}
	return DynamicTierPreviewDTO{}, false
}

var _ = fmt.Sprintf
