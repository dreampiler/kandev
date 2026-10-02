package controller

import (
	"strings"

	"github.com/kandev/kandev/internal/agent/settings/dto"
	"github.com/kandev/kandev/internal/agent/settings/models"
)

// storedDynamicRow is one saved candidate's selection state, addressed by its
// concrete profile ID. Concrete identity is the only stable grouping key; a
// badge number is derived adjacency and never survives a reorder.
type storedDynamicRow struct {
	hasSelection bool
	blockHead    string
	tier         dto.DynamicAgentTierPolicyDTO
	model        dto.DynamicAgentModelPolicyDTO
}

// readStoredDynamicSelection indexes the saved document by concrete profile ID
// and resolves each row's originating tier. Tier blocks are derived before any
// row is dropped so a disabled head still owns its policy, and a row that never
// opted into the selection document forms its own block with legacy defaults.
func readStoredDynamicSelection(routes []models.DynamicAgentRoute) (map[string]storedDynamicRow, error) {
	rows := make(map[string]storedDynamicRow, len(routes))
	currentHead := ""
	currentTier := defaultDynamicTierPolicy()
	for _, route := range routes {
		policy, err := decodeDynamicPolicyDocument(route.RulesJSON, route.Position)
		if err != nil {
			return nil, err
		}
		selection := policy.Selection
		// A row that does not join the row above it owns its tier, so it starts
		// a block. A head without stored metadata is a legacy row or a fragment
		// that inherited nothing, and both mean the legacy defaults.
		if selection == nil || !selection.JoinPrevious {
			currentHead = route.ExecutionProfileID
			currentTier = defaultDynamicTierPolicy()
			if selection != nil && selection.Tier != nil {
				currentTier = normalizedDynamicTierPolicy(*selection.Tier)
			}
		}
		row := storedDynamicRow{
			hasSelection: selection != nil,
			blockHead:    currentHead,
			tier:         currentTier,
		}
		if selection != nil {
			row.model = selection.Model
		}
		rows[route.ExecutionProfileID] = row
	}
	return rows, nil
}

func normalizedDynamicTierPolicy(tier dto.DynamicAgentTierPolicyDTO) dto.DynamicAgentTierPolicyDTO {
	if strings.TrimSpace(tier.Mode) == "" {
		tier.Mode = dynamicSelectionModeOrder
	}
	if strings.TrimSpace(tier.OnFailure) == "" {
		tier.OnFailure = dynamicSelectionFailureSameTierNext
	}
	return tier
}

// mergeDynamicSelection preserves a saved row's selection when an update omits
// it, so a client that predates the selection document cannot erase joins by
// reordering candidates. A join survives only between two rows that already
// shared a saved tier, so two rows merely becoming neighbours never manufacture
// a new edge, and every resulting fragment inherits its originating policy.
// Explicitly sent selection stays authoritative and is only canonicalized by
// the per-row head rules.
func mergeDynamicSelection(routes []models.DynamicAgentRoute, candidates []dto.DynamicAgentCandidateDTO) error {
	stored, err := readStoredDynamicSelection(routes)
	if err != nil {
		return err
	}
	previousBlock := ""
	for index := range candidates {
		candidate := &candidates[index]
		if candidate.Policies == nil {
			continue
		}
		id := strings.TrimSpace(candidate.ExecutionProfileID)
		row, known := stored[id]
		block := ""
		if known {
			block = row.blockHead
		}
		if candidate.Policies.Selection == nil && row.hasSelection {
			candidate.Policies.Selection = derivedDynamicSelection(row, block, previousBlock, index)
		}
		previousBlock = block
	}
	return nil
}

func derivedDynamicSelection(
	row storedDynamicRow,
	block string,
	previousBlock string,
	index int,
) *dto.DynamicAgentSelectionDTO {
	// Same saved tier on both sides of the adjacency: the edge is preserved.
	// A moved head, a split, or a cross-tier move all break it instead.
	joined := index > 0 && block != "" && block == previousBlock
	selection := dto.DynamicAgentSelectionDTO{JoinPrevious: joined, Model: row.model}
	if !joined {
		tier := row.tier
		selection.Tier = &tier
	}
	return &selection
}
