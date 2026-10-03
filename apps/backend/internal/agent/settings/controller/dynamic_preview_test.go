package controller

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/settings/dto"
)

// stubPreviewProvider records what the controller asked for and returns a
// canned prediction.
type stubPreviewProvider struct {
	preview    dynamic.SelectionPreview
	calls      int
	profiles   []dynamic.Profile
	ineligible map[string]string
}

func (s *stubPreviewProvider) PreviewDynamicSelection(
	_ context.Context,
	profile dynamic.Profile,
	ineligible map[string]string,
	_ time.Time,
) dynamic.SelectionPreview {
	s.calls++
	s.profiles = append(s.profiles, profile)
	s.ineligible = ineligible
	return s.preview
}

func newPreviewController(provider DynamicPreviewProvider) *Controller {
	controller := &Controller{}
	if provider != nil {
		controller.SetDynamicPreviewProvider(provider)
	}
	return controller
}

func previewDraft() *dto.DynamicAgentProfileDTO {
	profile := &dto.DynamicAgentProfileDTO{Version: 1}
	profile.Candidates = []dto.DynamicAgentCandidateDTO{
		{Position: 0, ExecutionProfileID: "head", Enabled: true, Policies: selectionPolicy(
			dynamicSelectionModePace, dynamicSelectionFailureSameTierNext, false, true)},
		{Position: 1, ExecutionProfileID: "joined", Enabled: true, Policies: selectionPolicy(
			"", "", true, false)},
		{Position: 2, ExecutionProfileID: "disabled", Enabled: false, Policies: selectionPolicy(
			dynamicSelectionModeOrder, dynamicSelectionFailureSameTierNext, false, true)},
	}
	return profile
}

func TestPreviewDynamicProfileReturnsPredictionForADraft(t *testing.T) {
	stub := &stubPreviewProvider{preview: dynamic.SelectionPreview{
		Available: true, CandidateID: "joined", TierIndex: 1, TierHeadID: "head",
		Mode: dynamic.TierModePace, Reason: dynamic.ReasonTierPace,
		ObservedAt: time.Unix(1000, 0),
		Considered: []dynamic.PreviewEntry{
			{CandidateID: "joined", TierIndex: 1, Selected: true, Eligible: true,
				Score: dynamic.PaceScore{Known: true, Complete: true, Pace: 0.125,
					Controlling: "five_hour", UsageFraction: 0.05, ElapsedFraction: 0.4}},
		},
	}}
	controller := newPreviewController(stub)
	result, err := controller.PreviewDynamicProfile(context.Background(), DynamicPreviewRequest{
		Dynamic: previewDraft(),
	})
	if err != nil {
		t.Fatalf("PreviewDynamicProfile: %v", err)
	}
	if result.State != string(dynamic.PreviewReady) || result.CandidateID != "joined" {
		t.Fatalf("result = %#v, want a ready prediction for joined", result)
	}
	if result.Reason != dynamic.ReasonTierPace || result.TierIndex != 1 {
		t.Fatalf("result = %#v, want the tier and reason carried through", result)
	}
	if len(result.Considered) != 1 {
		t.Fatalf("considered = %d, want 1", len(result.Considered))
	}
	entry := result.Considered[0]
	// The preview must expose the values a user needs to judge the choice.
	if entry.ControllingWindow != "five_hour" || entry.UsagePercent != 5 || entry.ElapsedPercent != 40 {
		t.Fatalf("entry = %#v, want the controlling window, usage and elapsed shares", entry)
	}
	if entry.Position != 1 || !entry.Selected || !entry.UsageKnown {
		t.Fatalf("entry = %#v, want position, selection and known usage", entry)
	}
	// A disabled row must be reported ineligible rather than silently dropped.
	if stub.ineligible["disabled"] != dynamic.IneligibleDisabled {
		t.Fatalf("ineligible = %#v, want the disabled row filtered", stub.ineligible)
	}
	if len(stub.profiles) != 1 || len(stub.profiles[0].Candidates) != 3 {
		t.Fatalf("profiles = %#v, want the whole draft evaluated", stub.profiles)
	}
}

func TestPreviewDynamicProfileReportsNoCandidatesWithoutProvider(t *testing.T) {
	controller := newPreviewController(nil)
	result, err := controller.PreviewDynamicProfile(context.Background(), DynamicPreviewRequest{})
	if err != nil {
		t.Fatalf("PreviewDynamicProfile: %v", err)
	}
	if result.State != string(dynamic.PreviewNoCandidates) {
		t.Fatalf("state = %q, want no candidates", result.State)
	}
}

// TestPreviewDynamicProfileReportsUnavailableWithoutAProvider keeps a missing
// seam explicit instead of letting it look like an empty profile.
func TestPreviewDynamicProfileReportsUnavailableWithoutAProvider(t *testing.T) {
	controller := newPreviewController(nil)
	result, err := controller.PreviewDynamicProfile(context.Background(), DynamicPreviewRequest{
		Dynamic: previewDraft(),
	})
	if err != nil {
		t.Fatalf("PreviewDynamicProfile: %v", err)
	}
	if result.State != string(dynamic.PreviewUnavailable) {
		t.Fatalf("state = %q, want unavailable rather than a guess", result.State)
	}
}

func TestPreviewDynamicProfileRejectsAnInvalidDraft(t *testing.T) {
	stub := &stubPreviewProvider{}
	controller := newPreviewController(stub)
	draft := previewDraft()
	// A joined row may not also own a tier, which is a save-time rejection.
	draft.Candidates[1].Policies.Selection.Tier = &dto.DynamicAgentTierPolicyDTO{
		Mode: dynamicSelectionModeOrder, OnFailure: dynamicSelectionFailureSameTierNext,
	}
	if _, err := controller.PreviewDynamicProfile(context.Background(), DynamicPreviewRequest{
		Dynamic: draft,
	}); err == nil {
		t.Fatal("invalid draft accepted, want the same rejection the save path gives")
	}
	if stub.calls != 0 {
		t.Fatalf("provider calls = %d, want an invalid draft to never reach the runtime", stub.calls)
	}
}

func TestPreviewDynamicProfilePreservesModelOptionsForThePrediction(t *testing.T) {
	stub := &stubPreviewProvider{preview: dynamic.SelectionPreview{Available: true, CandidateID: "head"}}
	controller := newPreviewController(stub)
	draft := previewDraft()
	draft.Candidates[0].Policies.Selection.Model.Cost = dynamicSelectionCostMetered
	draft.Candidates[0].Policies.Selection.Model.ReservedUserSharePct = 25
	draft.Candidates[0].Policies.Selection.Model.Windows = []dto.DynamicAgentUsageWindowDTO{{
		Period: dynamicSelectionWindowMonth, Unit: dynamicSelectionUnitMoney, Limit: "100.00",
		Reset: &dto.DynamicAgentResetAnchorDTO{Anchor: "00:00 day 1", Timezone: "UTC"},
	}}
	if _, err := controller.PreviewDynamicProfile(context.Background(), DynamicPreviewRequest{
		Dynamic: draft,
	}); err != nil {
		t.Fatalf("PreviewDynamicProfile: %v", err)
	}
	if len(stub.profiles) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(stub.profiles))
	}
	model := stub.profiles[0].Candidates[0].Selection.Model
	if model.Cost != dynamic.CostMetered || model.ReservedUserSharePct != 25 {
		t.Fatalf("model = %#v, want the configured cost and reserve", model)
	}
	if len(model.Windows) != 1 || model.Windows[0].Limit != "100.00" ||
		model.Windows[0].Timezone != "UTC" || model.Windows[0].ResetAnchor != "00:00 day 1" {
		t.Fatalf("windows = %#v, want the manual window carried through", model.Windows)
	}
}
