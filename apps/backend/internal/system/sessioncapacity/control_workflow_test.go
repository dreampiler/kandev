package sessioncapacity

import (
	"encoding/json"
	"testing"
)

func TestIsControlSessionMatchesEitherIdentity(t *testing.T) {
	profiles := []string{"ctrl-profile"}
	workflows := []string{"wf-control"}
	cases := []struct {
		name     string
		profile  string
		workflow string
		want     bool
	}{
		{"profile match", "ctrl-profile", "wf-other", true},
		{"workflow match", "worker", "wf-control", true},
		{"neither matches", "worker", "wf-other", false},
		{"empty profile, workflow match", "", "wf-control", true},
		{"profile match, empty workflow", "ctrl-profile", "", true},
		{"both empty", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsControlSession(tc.profile, tc.workflow, profiles, workflows); got != tc.want {
				t.Fatalf("IsControlSession(%q,%q) = %v, want %v", tc.profile, tc.workflow, got, tc.want)
			}
		})
	}
}

func TestControlLaneConfiguredNeedsCeilingAndIdentity(t *testing.T) {
	cases := []struct {
		name     string
		settings Settings
		want     bool
	}{
		{"workflow with ceiling", Settings{ControlMaxSessions: 2, ControlWorkflowIDs: []string{"wf"}}, true},
		{"profile with ceiling", Settings{ControlMaxSessions: 2, ControlProfileIDs: []string{"p"}}, true},
		{"no ceiling", Settings{ControlMaxSessions: 0, ControlWorkflowIDs: []string{"wf"}}, false},
		{"no identity", Settings{ControlMaxSessions: 2}, false},
	}
	for _, tc := range cases {
		if got := ControlLaneConfigured(tc.settings); got != tc.want {
			t.Fatalf("%s: ControlLaneConfigured = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSettingsPatchDecodesAndAppliesControlWorkflowIDs(t *testing.T) {
	var patch SettingsPatch
	if err := json.Unmarshal([]byte(`{"control_workflow_ids":[" wf-a ","wf-a","","wf-b"]}`), &patch); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if patch.ControlWorkflowIDs == nil {
		t.Fatal("control_workflow_ids was not decoded")
	}
	got := patch.Apply(Settings{ControlProfileIDs: []string{"keep"}})
	want := []string{"wf-a", "wf-b"}
	if len(got.ControlWorkflowIDs) != len(want) {
		t.Fatalf("control workflows = %v, want %v", got.ControlWorkflowIDs, want)
	}
	for i := range want {
		if got.ControlWorkflowIDs[i] != want[i] {
			t.Fatalf("control workflows = %v, want %v", got.ControlWorkflowIDs, want)
		}
	}
	if len(got.ControlProfileIDs) != 1 || got.ControlProfileIDs[0] != "keep" {
		t.Fatalf("omitted profile list must be retained, got %v", got.ControlProfileIDs)
	}
}

func TestEffectiveCarriesControlWorkflowIDs(t *testing.T) {
	resolution, err := Resolve(&Settings{MaxSessions: 4, ControlMaxSessions: 2, ControlWorkflowIDs: []string{"wf"}}, Environment{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolution.Effective.ControlWorkflowIDs) != 1 || resolution.Effective.ControlWorkflowIDs[0] != "wf" {
		t.Fatalf("effective control workflows = %v", resolution.Effective.ControlWorkflowIDs)
	}
	if !ControlLaneConfigured(resolution.Settings) {
		t.Fatal("control lane should be configured with a ceiling and a workflow")
	}
}
