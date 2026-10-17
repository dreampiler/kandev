package dynamic

import (
	"context"
	"time"
)

const tierModeRoundRobin = "round_robin"

type rankedTierRow struct {
	candidate Candidate
	entry     TierPreviewEntry
}

// TierPreview observes existing identities and eligibility without a decision.
type TierPreview struct {
	State       string             `json:"state"`
	CandidateID string             `json:"candidate_id,omitempty"`
	ObservedAt  time.Time          `json:"observed_at"`
	Considered  []TierPreviewEntry `json:"considered"`
}

type TierPreviewEntry struct {
	CandidateID           string   `json:"candidate_id"`
	Tier                  int      `json:"tier"`
	Mode                  string   `json:"mode"`
	Eligible              bool     `json:"eligible"`
	Reason                string   `json:"reason,omitempty"`
	Selected              bool     `json:"selected"`
	UsageKnown            bool     `json:"usage_known"`
	RemainingPct          *float64 `json:"remaining_pct,omitempty"`
	ConsumptionRateKnown  bool     `json:"consumption_rate_known"`
	ConsumptionPctPerHour *float64 `json:"consumption_pct_per_hour,omitempty"`
}

func tierEligibilityReason(candidate Candidate, excluded map[string]bool, preferred string, unavailable map[string]string) string {
	switch {
	case !candidate.Enabled:
		return "disabled"
	case excluded[candidate.ID] && candidate.ID != preferred:
		return "tried"
	case preferred != "" && candidate.ID != preferred:
		return "not_requested"
	default:
		return unavailable[candidate.ID]
	}
}

func tierHasEligibleRows(tier orderedTier, excluded map[string]bool, preferred string, unavailable map[string]string) bool {
	for _, candidate := range tier.rows {
		if tierEligibilityReason(candidate, excluded, preferred, unavailable) == "" {
			return true
		}
	}
	return false
}

func rankTierRows(tier orderedTier, number int, excluded map[string]bool, preferred string, unavailable map[string]string, history map[string]int64, now time.Time) []rankedTierRow {
	rows := make([]rankedTierRow, 0, len(tier.rows))
	for _, candidate := range tier.rows {
		reason := tierEligibilityReason(candidate, excluded, preferred, unavailable)
		rows = append(rows, rankedTierRow{candidate: candidate, entry: TierPreviewEntry{CandidateID: candidate.ID, Tier: number, Mode: tier.mode, Eligible: reason == "", Reason: reason}})
	}
	applyTierUsage(rows, now)
	if tier.mode == tierModeRoundRobin {
		return rotateRoundRobinRows(rows, history)
	}
	return rankUsageRows(rows, tier.mode)
}

// Observe an expired circuit's probe availability; only candidateSelectable
// may acquire its exclusive probe when an actual selection reaches it.
func (r *CircuitRegistry) selectionAvailability(candidate Candidate, now time.Time) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.circuits[accountBlockKey(candidate.ID)].held() {
		return "account_blocked"
	}
	entry := r.circuits[candidate.BindingKey]
	if candidate.BindingKey == "" || entry.state == CircuitClosed {
		return ""
	}
	if entry.state != CircuitHalfOpen && !entry.held() && (entry.state != CircuitOpen || entry.until.IsZero()) {
		return ""
	}
	if entry.held() || now.Before(entry.until) {
		return "circuit_open"
	}
	if now.Before(entry.probeUntil) {
		return "circuit_probing"
	}
	return ""
}

func (e *Engine) tierAvailability(profile Profile, now time.Time) map[string]string {
	result := make(map[string]string, len(profile.Candidates))
	for _, candidate := range profile.Candidates {
		result[candidate.ID] = e.circuits.selectionAvailability(candidate, now)
	}
	return result
}

func (e *Engine) PreviewTiers(profile Profile) (TierPreview, error) {
	return e.PreviewTiersContext(context.Background(), profile)
}

func (e *Engine) PreviewTiersContext(ctx context.Context, profile Profile) (TierPreview, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	preview := TierPreview{State: "disabled", ObservedAt: e.now(), Considered: []TierPreviewEntry{}}
	if !e.orderedTiers {
		return preview, nil
	}
	tiers, err := deriveOrderedTiers(profile.Candidates)
	if err != nil {
		return preview, err
	}
	unavailable := e.tierAvailability(profile, preview.ObservedAt)
	var history map[string]int64
	var loaded bool
	for index, tier := range tiers {
		unreached := tier.mode == tierModeRoundRobin && !loaded && preview.CandidateID != ""
		if tier.mode == tierModeRoundRobin && !loaded && !unreached && tierHasEligibleRows(tier, nil, "", unavailable) {
			history, err = e.tierSelectionHistory(ctx, profile.ID, []orderedTier{tier})
			if err != nil {
				preview.State = "observation_unknown"
				return preview, err
			}
			loaded = true
		}
		preview.appendTierRows(rankTierRows(tier, index+1, nil, "", unavailable, history, preview.ObservedAt), unreached)
	}
	preview.State = "no_candidates"
	if len(preview.Considered) > 0 {
		preview.State = "no_eligible"
	}
	if preview.CandidateID != "" {
		preview.State = "selected"
	}
	return preview, nil
}

func (p *TierPreview) appendTierRows(rows []rankedTierRow, unreached bool) {
	for _, row := range rows {
		entry := row.entry
		if unreached && entry.Eligible {
			entry.Eligible = false
			entry.Reason = "not_reached"
		}
		if entry.Eligible && p.CandidateID == "" {
			entry.Selected = true
			p.CandidateID = entry.CandidateID
		}
		p.Considered = append(p.Considered, entry)
	}
}
