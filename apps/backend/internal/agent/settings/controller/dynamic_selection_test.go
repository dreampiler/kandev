package controller

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kandev/kandev/internal/agent/settings/dto"
	"github.com/kandev/kandev/internal/agent/settings/models"
)

func selectionPolicy(tierMode, onFailure string, joinPrevious bool, tierOwned bool) *dto.DynamicAgentPolicyDTO {
	policy := baseDynamicPolicy()
	selection := dto.DynamicAgentSelectionDTO{
		JoinPrevious: joinPrevious,
		Model: dto.DynamicAgentModelPolicyDTO{
			Cost:        dynamicSelectionCostSubscription,
			UsageSource: dynamicSelectionUsageAutomatic,
		},
	}
	if tierOwned {
		selection.Tier = &dto.DynamicAgentTierPolicyDTO{Mode: tierMode, OnFailure: onFailure}
	}
	policy.Selection = &selection
	return policy
}

func baseDynamicPolicy() *dto.DynamicAgentPolicyDTO {
	return &dto.DynamicAgentPolicyDTO{
		Version: dynamicPolicyVersion,
		Transient: dto.DynamicErrorPolicyDTO{
			Retry:        dto.DynamicRetryPolicyDTO{Enabled: true, MaxRetries: 2, InitialIntervalSeconds: 5},
			WaitForReset: dto.DynamicResetWaitPolicyDTO{Enabled: true, MaxWaitSeconds: 300},
			OnExhausted:  dynamicPolicyOutcomeSkip,
		},
		Hard:         dto.DynamicErrorPolicyDTO{OnExhausted: dynamicPolicyOutcomeStop},
		Unclassified: &dto.DynamicUnclassifiedPolicyDTO{Enabled: true, ConsecutiveFailureThreshold: 3},
	}
}

func TestDynamicTierSelectionRoundTrip(t *testing.T) {
	profile := &dto.DynamicAgentProfileDTO{
		Version: 1,
		Candidates: []dto.DynamicAgentCandidateDTO{
			{Position: 0, ExecutionProfileID: "a", Enabled: true,
				Policies: selectionPolicy(dynamicSelectionModePace, dynamicSelectionFailureSameTierNext, false, true)},
			{Position: 1, ExecutionProfileID: "b", Enabled: true,
				Policies: selectionPolicy("", "", true, false)},
			{Position: 2, ExecutionProfileID: "c", Enabled: true,
				Policies: selectionPolicy(dynamicSelectionModeCost, dynamicSelectionFailureNextTier, false, true)},
		},
	}
	keep := false
	profile.KeepModelWhileRunning = &keep

	routes, err := dynamicRoutesFromDTO("dynamic-1", profile)
	if err != nil {
		t.Fatalf("dynamicRoutesFromDTO: %v", err)
	}
	stored := &models.DynamicAgentProfile{ProfileID: "dynamic-1", Version: 3, KeepModelWhileRunning: keep}
	decoded, err := dynamicProfileDTO(stored, routes)
	if err != nil {
		t.Fatalf("dynamicProfileDTO: %v", err)
	}
	if decoded.Version != 3 {
		t.Fatalf("version = %d, want 3", decoded.Version)
	}
	if decoded.KeepModelWhileRunning == nil || *decoded.KeepModelWhileRunning {
		t.Fatalf("keep model = %v, want explicit false", decoded.KeepModelWhileRunning)
	}
	if len(decoded.Candidates) != 3 {
		t.Fatalf("candidates = %d, want 3", len(decoded.Candidates))
	}

	head := decoded.Candidates[0].Policies
	if head.Selection == nil || head.Selection.JoinPrevious {
		t.Fatalf("head join = %#v, want an unjoined first row", head.Selection)
	}
	if head.Selection.Tier == nil || head.Selection.Tier.Mode != dynamicSelectionModePace ||
		head.Selection.Tier.OnFailure != dynamicSelectionFailureSameTierNext {
		t.Fatalf("head tier = %#v", head.Selection.Tier)
	}
	joined := decoded.Candidates[1].Policies.Selection
	if joined == nil || !joined.JoinPrevious || joined.Tier != nil {
		t.Fatalf("joined row = %#v, want joined with no tier", joined)
	}
	tail := decoded.Candidates[2].Policies.Selection
	if tail.JoinPrevious || tail.Tier.Mode != dynamicSelectionModeCost ||
		tail.Tier.OnFailure != dynamicSelectionFailureNextTier {
		t.Fatalf("tail tier = %#v", tail.Tier)
	}

	// The additive selection must not disturb any existing failure policy.
	if !head.Transient.Retry.Enabled || head.Transient.Retry.MaxRetries != 2 ||
		head.Transient.WaitForReset.MaxWaitSeconds != 300 ||
		head.Transient.OnExhausted != dynamicPolicyOutcomeSkip ||
		head.Hard.OnExhausted != dynamicPolicyOutcomeStop ||
		head.Unclassified == nil || head.Unclassified.ConsecutiveFailureThreshold != 3 {
		t.Fatalf("failure policy lost: %#v", head)
	}
}

func TestDynamicTierSelectionValidation(t *testing.T) {
	manualWindow := dto.DynamicAgentUsageWindowDTO{
		Period: dynamicSelectionWindowMonth,
		Unit:   dynamicSelectionUnitMoney,
		Limit:  "120.50",
		Reset:  &dto.DynamicAgentResetAnchorDTO{Anchor: "09:00", Timezone: "Asia/Seoul"},
	}
	withModel := func(mutate func(*dto.DynamicAgentModelPolicyDTO)) *dto.DynamicAgentPolicyDTO {
		policy := selectionPolicy(dynamicSelectionModeOrder, dynamicSelectionFailureSameTierNext, false, true)
		mutate(&policy.Selection.Model)
		return policy
	}
	tests := []struct {
		name       string
		candidates []dto.DynamicAgentCandidateDTO
		wantErr    bool
	}{
		{
			name: "first row cannot join the row above it",
			candidates: []dto.DynamicAgentCandidateDTO{
				{Position: 0, ExecutionProfileID: "a",
					Policies: selectionPolicy(dynamicSelectionModeOrder, dynamicSelectionFailureSameTierNext, true, true)},
			},
			wantErr: true,
		},
		{
			name: "a joined row cannot also own a tier",
			candidates: []dto.DynamicAgentCandidateDTO{
				{Position: 0, ExecutionProfileID: "a",
					Policies: selectionPolicy(dynamicSelectionModeOrder, dynamicSelectionFailureSameTierNext, false, true)},
				{Position: 1, ExecutionProfileID: "b",
					Policies: selectionPolicy(dynamicSelectionModeOrder, dynamicSelectionFailureSameTierNext, true, true)},
			},
			wantErr: true,
		},
		{
			name: "a tier head must carry a tier policy",
			candidates: []dto.DynamicAgentCandidateDTO{
				{Position: 0, ExecutionProfileID: "a",
					Policies: selectionPolicy(dynamicSelectionModeOrder, dynamicSelectionFailureSameTierNext, false, false)},
			},
			wantErr: true,
		},
		{
			name: "unknown tier mode is rejected",
			candidates: []dto.DynamicAgentCandidateDTO{
				{Position: 0, ExecutionProfileID: "a",
					Policies: selectionPolicy("cheapest", dynamicSelectionFailureSameTierNext, false, true)},
			},
			wantErr: true,
		},
		{
			name: "unknown failure direction is rejected",
			candidates: []dto.DynamicAgentCandidateDTO{
				{Position: 0, ExecutionProfileID: "a",
					Policies: selectionPolicy(dynamicSelectionModeOrder, "skip_tier", false, true)},
			},
			wantErr: true,
		},
		{
			name: "a manual money window is accepted",
			candidates: []dto.DynamicAgentCandidateDTO{
				{Position: 0, ExecutionProfileID: "a",
					Policies: withModel(func(m *dto.DynamicAgentModelPolicyDTO) {
						m.UsageSource = dynamicSelectionUsageManual
						m.Windows = []dto.DynamicAgentUsageWindowDTO{manualWindow}
					})},
			},
		},
		{
			name: "a nonpositive allowance is rejected",
			candidates: []dto.DynamicAgentCandidateDTO{
				{Position: 0, ExecutionProfileID: "a",
					Policies: withModel(func(m *dto.DynamicAgentModelPolicyDTO) {
						m.UsageSource = dynamicSelectionUsageManual
						zero := manualWindow
						zero.Limit = "0"
						m.Windows = []dto.DynamicAgentUsageWindowDTO{zero}
					})},
			},
			wantErr: true,
		},
		{
			name: "a nonfinite allowance is rejected",
			candidates: []dto.DynamicAgentCandidateDTO{
				{Position: 0, ExecutionProfileID: "a",
					Policies: withModel(func(m *dto.DynamicAgentModelPolicyDTO) {
						m.UsageSource = dynamicSelectionUsageManual
						bad := manualWindow
						bad.Limit = "NaN"
						m.Windows = []dto.DynamicAgentUsageWindowDTO{bad}
					})},
			},
			wantErr: true,
		},
		{
			name: "an unknown timezone is rejected",
			candidates: []dto.DynamicAgentCandidateDTO{
				{Position: 0, ExecutionProfileID: "a",
					Policies: withModel(func(m *dto.DynamicAgentModelPolicyDTO) {
						m.UsageSource = dynamicSelectionUsageManual
						bad := manualWindow
						bad.Reset = &dto.DynamicAgentResetAnchorDTO{Anchor: "09:00", Timezone: "Mars/Olympus"}
						m.Windows = []dto.DynamicAgentUsageWindowDTO{bad}
					})},
			},
			wantErr: true,
		},
		{
			name: "a reservation without a usage source is rejected",
			candidates: []dto.DynamicAgentCandidateDTO{
				{Position: 0, ExecutionProfileID: "a",
					Policies: withModel(func(m *dto.DynamicAgentModelPolicyDTO) {
						m.UsageSource = dynamicSelectionUsageNone
						m.ReservedUserSharePct = 20
					})},
			},
			wantErr: true,
		},
		{
			name: "a reservation above one hundred percent is rejected",
			candidates: []dto.DynamicAgentCandidateDTO{
				{Position: 0, ExecutionProfileID: "a",
					Policies: withModel(func(m *dto.DynamicAgentModelPolicyDTO) {
						m.ReservedUserSharePct = 101
					})},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDynamicAgentProfile(&dto.DynamicAgentProfileDTO{
				Version: 1, Candidates: tt.candidates,
			})
			if tt.wantErr {
				if !errors.Is(err, ErrDynamicProfileRule) {
					t.Fatalf("error = %v, want %v", err, ErrDynamicProfileRule)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateDynamicAgentProfile: %v", err)
			}
		})
	}
}

func TestDynamicSelectionDefaultsAndLegacyAbsence(t *testing.T) {
	// An empty tier document normalizes to the documented defaults rather than
	// failing, so an older client can send a head without naming a mode.
	profile := &dto.DynamicAgentProfileDTO{
		Version: 1,
		Candidates: []dto.DynamicAgentCandidateDTO{{
			Position: 0, ExecutionProfileID: "a",
			Policies: selectionPolicy("", "", false, true),
		}},
	}
	if err := validateDynamicAgentProfile(profile); err != nil {
		t.Fatalf("validateDynamicAgentProfile: %v", err)
	}
	head := profile.Candidates[0].Policies.Selection
	if head.Tier.Mode != dynamicSelectionModeOrder ||
		head.Tier.OnFailure != dynamicSelectionFailureSameTierNext {
		t.Fatalf("tier = %#v, want the documented defaults", head.Tier)
	}
	if head.Model.ReservedUserSharePct != 0 {
		t.Fatalf("reserved share = %d, want the zero default", head.Model.ReservedUserSharePct)
	}

	// An omitted usage source normalizes to "none" rather than an invented
	// automatic binding, so unavailable usage stays visibly unknown.
	unset := selectionPolicy("", "", false, true)
	unset.Selection.Model.UsageSource = ""
	if err := validateDynamicAgentProfile(&dto.DynamicAgentProfileDTO{
		Version: 1, Candidates: []dto.DynamicAgentCandidateDTO{{
			Position: 0, ExecutionProfileID: "a", Policies: unset,
		}},
	}); err != nil {
		t.Fatalf("validateDynamicAgentProfile: %v", err)
	}
	if unset.Selection.Model.UsageSource != dynamicSelectionUsageNone {
		t.Fatalf("usage source = %q, want %q", unset.Selection.Model.UsageSource, dynamicSelectionUsageNone)
	}

	// A legacy row without a selection document keeps ordered routing, and a
	// decoder must not invent one.
	legacy, err := decodeDynamicPolicyDocument(`{"version":1,"transient":{"on_exhausted":"skip"},"hard":{"on_exhausted":"stop"}}`, 0)
	if err != nil {
		t.Fatalf("decodeDynamicPolicyDocument: %v", err)
	}
	if legacy.Selection != nil {
		t.Fatalf("selection = %#v, want absent for a legacy document", legacy.Selection)
	}
	if legacy.Hard.OnExhausted != dynamicPolicyOutcomeStop {
		t.Fatalf("hard policy = %#v", legacy.Hard)
	}
}

func TestDynamicSelectionLegacyRulesStillDecodeWithoutSelection(t *testing.T) {
	policy, err := decodeDynamicPolicyDocument(`{"on_provider_error":"try_next"}`, 0)
	if err != nil {
		t.Fatalf("decodeDynamicPolicyDocument: %v", err)
	}
	if policy.Selection != nil {
		t.Fatalf("selection = %#v, want absent for a legacy action map", policy.Selection)
	}
	if policy.Transient.OnExhausted != dynamicPolicyOutcomeSkip {
		t.Fatalf("transient = %#v", policy.Transient)
	}
}

// storedRoutesWithJoins builds the saved document for the group-mutation cases
// from a compact notation: "A=" means a row joined to the row above it.
func storedRoutesWithJoins(t *testing.T, rows ...string) []models.DynamicAgentRoute {
	t.Helper()
	routes := make([]models.DynamicAgentRoute, 0, len(rows))
	for position, row := range rows {
		joined := strings.HasSuffix(row, "=")
		id := strings.TrimSuffix(row, "=")
		mode := dynamicSelectionModeOrder
		if position == 0 || !joined {
			mode = dynamicSelectionModePace
		}
		policy := selectionPolicy(mode, dynamicSelectionFailureSameTierNext, joined, !joined)
		raw, err := json.Marshal(policy)
		if err != nil {
			t.Fatalf("marshal policy: %v", err)
		}
		routes = append(routes, models.DynamicAgentRoute{
			DynamicProfileID: "dynamic-1", Position: position,
			ExecutionProfileID: id, Enabled: true, RulesJSON: string(raw),
		})
	}
	return routes
}

func reorderedCandidates(ids ...string) []dto.DynamicAgentCandidateDTO {
	candidates := make([]dto.DynamicAgentCandidateDTO, 0, len(ids))
	for position, id := range ids {
		candidates = append(candidates, dto.DynamicAgentCandidateDTO{
			Position: position, ExecutionProfileID: id, Policies: baseDynamicPolicy(),
		})
	}
	return candidates
}

func summarizeSelection(t *testing.T, candidates []dto.DynamicAgentCandidateDTO) string {
	t.Helper()
	var builder strings.Builder
	for _, candidate := range candidates {
		selection := candidate.Policies.Selection
		if selection == nil {
			builder.WriteString(candidate.ExecutionProfileID + "[legacy] ")
			continue
		}
		if selection.JoinPrevious {
			builder.WriteString(candidate.ExecutionProfileID + "[=] ")
			continue
		}
		mode := "?"
		if selection.Tier != nil {
			mode = selection.Tier.Mode
		}
		builder.WriteString(candidate.ExecutionProfileID + "[" + mode + "] ")
	}
	return strings.TrimSpace(builder.String())
}

func TestDynamicSelectionGroupMutationsPreserveJoins(t *testing.T) {
	tests := []struct {
		name  string
		saved []string
		order []string
		want  string
	}{
		{
			// A row crossing a tier boundary must not bind its new neighbours.
			name:  "cross tier move breaks both joins without binding new neighbours",
			saved: []string{"A", "B=", "C", "D="},
			order: []string{"A", "C", "B", "D"},
			want:  "A[pace] C[pace] B[pace] D[pace]",
		},
		{
			name:  "same tier move keeps the block and its policy",
			saved: []string{"A", "B=", "C="},
			order: []string{"B", "A", "C"},
			want:  "B[pace] A[=] C[=]",
		},
		{
			name:  "removing a head leaves the rest of the tier grouped",
			saved: []string{"A", "B=", "C", "D"},
			order: []string{"B", "C", "D"},
			want:  "B[pace] C[pace] D[pace]",
		},
		{
			name:  "removing a middle row does not merge two unrelated tiers",
			saved: []string{"A", "B=", "C", "D="},
			order: []string{"A", "C", "D"},
			want:  "A[pace] C[pace] D[=]",
		},
		{
			name:  "an unchanged order is untouched",
			saved: []string{"A", "B=", "C"},
			order: []string{"A", "B", "C"},
			want:  "A[pace] B[=] C[pace]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates := reorderedCandidates(tt.order...)
			if err := mergeDynamicSelection(storedRoutesWithJoins(t, tt.saved...), candidates); err != nil {
				t.Fatalf("mergeDynamicSelection: %v", err)
			}
			if got := summarizeSelection(t, candidates); got != tt.want {
				t.Fatalf("selection = %q, want %q", got, tt.want)
			}
			if err := validateDynamicAgentProfile(&dto.DynamicAgentProfileDTO{
				Version: 1, Candidates: candidates,
			}); err != nil {
				t.Fatalf("validateDynamicAgentProfile: %v", err)
			}
		})
	}
}

func TestDynamicSelectionOmissionPreservesSavedRow(t *testing.T) {
	saved := storedRoutesWithJoins(t, "A", "B=", "C")
	saved[1].RulesJSON = strings.Replace(
		strings.Replace(saved[1].RulesJSON, `"cost":"subscription"`, `"cost":"metered"`, 1),
		`"usage_source":"automatic"`, `"usage_source":"manual"`, 1)

	candidates := reorderedCandidates("A", "B", "C")
	if err := mergeDynamicSelection(saved, candidates); err != nil {
		t.Fatalf("mergeDynamicSelection: %v", err)
	}
	joined := candidates[1].Policies.Selection
	if joined == nil || !joined.JoinPrevious {
		t.Fatalf("selection = %#v, want the saved join preserved", joined)
	}
	if joined.Model.Cost != dynamicSelectionCostMetered || joined.Model.UsageSource != dynamicSelectionUsageManual {
		t.Fatalf("model = %#v, want the saved per-row model options", joined.Model)
	}
}

func TestDynamicSelectionLegacyRowStaysUnconfigured(t *testing.T) {
	legacy := models.DynamicAgentRoute{
		DynamicProfileID: "dynamic-1", Position: 1, ExecutionProfileID: "B", Enabled: true,
		RulesJSON: `{"version":1,"transient":{"on_exhausted":"skip"},"hard":{"on_exhausted":"stop"}}`,
	}
	saved := append(storedRoutesWithJoins(t, "A"), legacy)
	candidates := reorderedCandidates("A", "B", "C")
	if err := mergeDynamicSelection(saved, candidates); err != nil {
		t.Fatalf("mergeDynamicSelection: %v", err)
	}
	if candidates[1].Policies.Selection != nil {
		t.Fatalf("selection = %#v, want a legacy row left unconfigured", candidates[1].Policies.Selection)
	}
	if candidates[2].Policies.Selection != nil {
		t.Fatalf("new row selection = %#v, want defaults owned by the save path", candidates[2].Policies.Selection)
	}
}
