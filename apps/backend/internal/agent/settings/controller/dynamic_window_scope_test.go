package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/agent/settings/dto"
)

func manualWindowWithScope(scope string) []dto.DynamicAgentCandidateDTO {
	policy := selectionPolicy(dynamicSelectionModeOrder, dynamicSelectionFailureSameTierNext, false, true)
	policy.Selection.Model.UsageSource = dynamicSelectionUsageManual
	policy.Selection.Model.Windows = []dto.DynamicAgentUsageWindowDTO{{
		Period: dynamicSelectionWindowDay, Unit: dynamicSelectionUnitTokens, Limit: "50000000",
		Reset: &dto.DynamicAgentResetAnchorDTO{Anchor: "00:00", Timezone: "UTC"}, Scope: scope,
	}}
	return []dto.DynamicAgentCandidateDTO{{Position: 0, ExecutionProfileID: "a", Policies: policy}}
}

func TestManualWindowScopeValidation(t *testing.T) {
	for _, scope := range []string{"", dynamicSelectionScopeCandidate, dynamicSelectionScopeAccount} {
		err := validateDynamicAgentProfile(&dto.DynamicAgentProfileDTO{Version: 1, Candidates: manualWindowWithScope(scope)})
		if err != nil {
			t.Fatalf("scope %q: %v", scope, err)
		}
	}
	err := validateDynamicAgentProfile(&dto.DynamicAgentProfileDTO{Version: 1, Candidates: manualWindowWithScope("workspace")})
	if !errors.Is(err, ErrDynamicProfileRule) {
		t.Fatalf("error = %v, want an unknown scope rejected", err)
	}
}

func TestManualWindowScopeReachesThePreview(t *testing.T) {
	document := &dto.DynamicAgentProfileDTO{Version: 1, Candidates: manualWindowWithScope(dynamicSelectionScopeAccount)}
	profile := previewProfile(document)
	if windows := profile.Candidates[0].Selection.Model.Windows; len(windows) != 1 || !windows[0].AccountScoped() {
		t.Fatalf("windows = %#v, want the account scope carried into ranking", windows)
	}
}

type staticProfileUsage struct{ profiles []dto.AgentProfileUsageDTO }

func (s staticProfileUsage) ListProfileUsage(context.Context) ([]dto.AgentProfileUsageDTO, error) {
	return s.profiles, nil
}

func TestListProfileUsage(t *testing.T) {
	c := &Controller{}
	resp, err := c.ListProfileUsage(context.Background())
	if err != nil || resp.Profiles == nil || len(resp.Profiles) != 0 {
		t.Fatalf("without a provider = %#v, %v; want an empty list", resp, err)
	}
	c.SetProfileUsageProvider(staticProfileUsage{profiles: []dto.AgentProfileUsageDTO{{ProfileID: "p", State: "ok"}}})
	resp, err = c.ListProfileUsage(context.Background())
	if err != nil || len(resp.Profiles) != 1 || resp.Profiles[0].ProfileID != "p" {
		t.Fatalf("ListProfileUsage = %#v, %v", resp, err)
	}
}

func TestRandomAndRoundRobinTierModesAreAccepted(t *testing.T) {
	for _, mode := range []string{dynamicSelectionModeRandom, dynamicSelectionModeRoundRobin} {
		candidates := []dto.DynamicAgentCandidateDTO{{
			Position: 0, ExecutionProfileID: "a",
			Policies: selectionPolicy(mode, dynamicSelectionFailureSameTierNext, false, true),
		}}
		if err := validateDynamicAgentProfile(&dto.DynamicAgentProfileDTO{Version: 1, Candidates: candidates}); err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
	}
}
