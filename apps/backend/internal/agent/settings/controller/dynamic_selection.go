package controller

import (
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/kandev/kandev/internal/agent/settings/dto"
)

// The selection document is additive: a candidate without it keeps ordered
// routing and the legacy failure policies. These values are the documented
// defaults, never a seeded operational configuration.
const (
	dynamicSelectionModeOrder = "order"
	dynamicSelectionModePace  = "pace"
	dynamicSelectionModeCost  = "cost"

	dynamicSelectionFailureSameTierNext = "same_tier_next"
	dynamicSelectionFailureNextTier     = "next_tier"

	dynamicSelectionCostFree         = "free"
	dynamicSelectionCostSubscription = "subscription"
	dynamicSelectionCostMetered      = "metered"

	dynamicSelectionUsageAutomatic = "automatic"
	dynamicSelectionUsageManual    = "manual"
	dynamicSelectionUsageNone      = "none"

	dynamicSelectionWindowFiveHour = "five_hour"
	dynamicSelectionWindowDay      = "day"
	dynamicSelectionWindowWeek     = "week"
	dynamicSelectionWindowMonth    = "month"

	dynamicSelectionUnitMoney  = "money"
	dynamicSelectionUnitTokens = "tokens"

	dynamicSelectionMaxManualWindows = 8
	dynamicSelectionMaxReservedPct   = 100
)

func defaultDynamicTierPolicy() dto.DynamicAgentTierPolicyDTO {
	return dto.DynamicAgentTierPolicyDTO{
		Mode:      dynamicSelectionModeOrder,
		OnFailure: dynamicSelectionFailureSameTierNext,
	}
}

// normalizeDynamicSelection applies the documented defaults and rejects every
// closed-set, head-ownership and limit violation before a row reaches storage.
// The first row is always a tier head: joining the row above it is a
// contradiction rather than something to silently repair.
func normalizeDynamicSelection(candidate *dto.DynamicAgentCandidateDTO, head bool) error {
	selection := candidate.Policies.Selection
	if selection == nil {
		return nil
	}
	if candidate.Position == 0 && selection.JoinPrevious {
		return fmt.Errorf("%w: candidates[0].policies.selection.join_previous must be false on the first row", ErrDynamicProfileRule)
	}
	if !head && selection.JoinPrevious {
		if selection.Tier != nil {
			return fmt.Errorf("%w: candidates[%d].policies.selection.tier is only allowed on a tier's first row", ErrDynamicProfileRule, candidate.Position)
		}
		return nil
	}
	selection.JoinPrevious = false
	if selection.Tier == nil {
		return fmt.Errorf("%w: candidates[%d].policies.selection.tier is required on a tier's first row", ErrDynamicProfileRule, candidate.Position)
	}
	if err := normalizeDynamicTierPolicy(candidate.Position, selection.Tier); err != nil {
		return err
	}
	return normalizeDynamicModelPolicy(candidate.Position, &selection.Model)
}

func normalizeDynamicTierPolicy(position int, tier *dto.DynamicAgentTierPolicyDTO) error {
	if strings.TrimSpace(tier.Mode) == "" {
		tier.Mode = dynamicSelectionModeOrder
	}
	if strings.TrimSpace(tier.OnFailure) == "" {
		tier.OnFailure = dynamicSelectionFailureSameTierNext
	}
	if !isDynamicSelectionMode(tier.Mode) {
		return fmt.Errorf("%w: candidates[%d].policies.selection.tier.mode=%q", ErrDynamicProfileRule, position, tier.Mode)
	}
	if !isDynamicSelectionFailureDirection(tier.OnFailure) {
		return fmt.Errorf("%w: candidates[%d].policies.selection.tier.on_failure=%q", ErrDynamicProfileRule, position, tier.OnFailure)
	}
	return nil
}

func normalizeDynamicModelPolicy(position int, model *dto.DynamicAgentModelPolicyDTO) error {
	if strings.TrimSpace(model.UsageSource) == "" {
		model.UsageSource = dynamicSelectionUsageNone
	}
	if model.Cost != "" && !isDynamicSelectionCost(model.Cost) {
		return fmt.Errorf("%w: candidates[%d].policies.selection.model.cost=%q", ErrDynamicProfileRule, position, model.Cost)
	}
	if !isDynamicSelectionUsageSource(model.UsageSource) {
		return fmt.Errorf("%w: candidates[%d].policies.selection.model.usage_source=%q", ErrDynamicProfileRule, position, model.UsageSource)
	}
	if model.ReservedUserSharePct < 0 || model.ReservedUserSharePct > dynamicSelectionMaxReservedPct {
		return fmt.Errorf("%w: candidates[%d].policies.selection.model.reserved_user_share_pct=%d must be 0 through %d",
			ErrDynamicProfileRule, position, model.ReservedUserSharePct, dynamicSelectionMaxReservedPct)
	}
	// A reservation is only meaningful against a usage source that can be
	// observed. Rejecting it for "none" keeps a silent capacity hold from
	// masquerading as a configured allowance.
	if model.ReservedUserSharePct > 0 && model.UsageSource == dynamicSelectionUsageNone {
		return fmt.Errorf("%w: candidates[%d].policies.selection.model.reserved_user_share_pct requires a usage source",
			ErrDynamicProfileRule, position)
	}
	for index := range model.Windows {
		if err := normalizeDynamicUsageWindow(position, &model.Windows[index]); err != nil {
			return err
		}
	}
	if len(model.Windows) > dynamicSelectionMaxManualWindows {
		return fmt.Errorf("%w: candidates[%d].policies.selection.model.windows has %d entries, at most %d are allowed",
			ErrDynamicProfileRule, position, len(model.Windows), dynamicSelectionMaxManualWindows)
	}
	return nil
}

func normalizeDynamicUsageWindow(position int, window *dto.DynamicAgentUsageWindowDTO) error {
	field := fmt.Sprintf("candidates[%d].policies.selection.model.windows", position)
	if !isDynamicSelectionWindowPeriod(window.Period) {
		return fmt.Errorf("%w: %s.period=%q", ErrDynamicProfileRule, field, window.Period)
	}
	if !isDynamicSelectionWindowUnit(window.Unit) {
		return fmt.Errorf("%w: %s.unit=%q", ErrDynamicProfileRule, field, window.Unit)
	}
	if err := validateDynamicSelectionLimit(window.Limit); err != nil {
		return fmt.Errorf("%w: %s.limit: %w", ErrDynamicProfileRule, field, err)
	}
	if window.Reset == nil {
		return fmt.Errorf("%w: %s.reset is required", ErrDynamicProfileRule, field)
	}
	if err := validateDynamicResetAnchor(window.Reset); err != nil {
		return fmt.Errorf("%w: %s.reset: %w", ErrDynamicProfileRule, field, err)
	}
	return nil
}

// validateDynamicSelectionLimit accepts only a finite positive decimal, parsed
// exactly so a money allowance keeps subcent precision.
func validateDynamicSelectionLimit(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("an allowance is required")
	}
	value, ok := new(big.Rat).SetString(trimmed)
	if !ok {
		return fmt.Errorf("%q is not a decimal number", raw)
	}
	if value.Sign() <= 0 {
		return fmt.Errorf("%q must be greater than zero", raw)
	}
	return nil
}

func validateDynamicResetAnchor(reset *dto.DynamicAgentResetAnchorDTO) error {
	anchor := strings.TrimSpace(reset.Anchor)
	if anchor == "" {
		return fmt.Errorf("an anchor is required")
	}
	if len(anchor) > 64 {
		return fmt.Errorf("anchor is longer than 64 characters")
	}
	timezone := strings.TrimSpace(reset.Timezone)
	if timezone == "" {
		return fmt.Errorf("a timezone is required")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return fmt.Errorf("timezone %q is not a known IANA zone", timezone)
	}
	return nil
}

func isDynamicSelectionMode(mode string) bool {
	switch mode {
	case dynamicSelectionModeOrder, dynamicSelectionModePace, dynamicSelectionModeCost:
		return true
	default:
		return false
	}
}

func isDynamicSelectionFailureDirection(direction string) bool {
	switch direction {
	case dynamicSelectionFailureSameTierNext, dynamicSelectionFailureNextTier:
		return true
	default:
		return false
	}
}

func isDynamicSelectionCost(cost string) bool {
	switch cost {
	case dynamicSelectionCostFree, dynamicSelectionCostSubscription, dynamicSelectionCostMetered:
		return true
	default:
		return false
	}
}

func isDynamicSelectionUsageSource(source string) bool {
	switch source {
	case dynamicSelectionUsageAutomatic, dynamicSelectionUsageManual, dynamicSelectionUsageNone:
		return true
	default:
		return false
	}
}

func isDynamicSelectionWindowPeriod(period string) bool {
	switch period {
	case dynamicSelectionWindowFiveHour, dynamicSelectionWindowDay, dynamicSelectionWindowWeek, dynamicSelectionWindowMonth:
		return true
	default:
		return false
	}
}

func isDynamicSelectionWindowUnit(unit string) bool {
	switch unit {
	case dynamicSelectionUnitMoney, dynamicSelectionUnitTokens:
		return true
	default:
		return false
	}
}
