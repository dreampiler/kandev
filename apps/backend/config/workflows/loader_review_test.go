package workflows

import (
	"testing"

	"github.com/kandev/kandev/internal/workflow/models"
	"gopkg.in/yaml.v3"
)

// TestConvertEvents_RunCodeReview round-trips a run_code_review on_enter action
// through the YAML loader: the action type passes the allow-list and its
// optional agent_profile_id survives into the typed StepEvents.
func TestConvertEvents_RunCodeReview(t *testing.T) {
	const yamlDoc = `
on_enter:
  - type: run_code_review
    config:
      agent_profile_id: profile-review
`
	var e stepEventsYAML
	if err := yaml.Unmarshal([]byte(yamlDoc), &e); err != nil {
		t.Fatalf("unmarshal yaml: %v", err)
	}
	events, err := convertEvents(e)
	if err != nil {
		t.Fatalf("convertEvents returned error: %v", err)
	}
	if len(events.OnEnter) != 1 {
		t.Fatalf("expected 1 on_enter action, got %d", len(events.OnEnter))
	}
	if events.OnEnter[0].Type != models.OnEnterRunCodeReview {
		t.Fatalf("unexpected action type %q", events.OnEnter[0].Type)
	}
	if profile, _ := events.OnEnter[0].Config[models.ReviewAgentProfileConfigKey].(string); profile != "profile-review" {
		t.Fatalf("expected agent_profile_id=profile-review, got %q", profile)
	}
}

// TestConvertEvents_RunCodeReviewWithoutProfile keeps the profile optional: an
// action with no config means "use the configured code-review utility agent".
func TestConvertEvents_RunCodeReviewWithoutProfile(t *testing.T) {
	var e stepEventsYAML
	if err := yaml.Unmarshal([]byte("on_enter:\n  - type: run_code_review\n"), &e); err != nil {
		t.Fatalf("unmarshal yaml: %v", err)
	}
	events, err := convertEvents(e)
	if err != nil {
		t.Fatalf("convertEvents returned error: %v", err)
	}
	if len(events.OnEnter) != 1 || events.OnEnter[0].Type != models.OnEnterRunCodeReview {
		t.Fatalf("expected 1 run_code_review action, got %+v", events.OnEnter)
	}
}

// TestConvertEvents_RunCodeReviewRejectsNonStringProfile verifies the loader
// fails fast when a run_code_review action carries a non-string
// agent_profile_id, rather than silently dropping it at compile time.
func TestConvertEvents_RunCodeReviewRejectsNonStringProfile(t *testing.T) {
	doc := "on_enter:\n  - type: run_code_review\n    config:\n      agent_profile_id: 7\n"
	var e stepEventsYAML
	if err := yaml.Unmarshal([]byte(doc), &e); err != nil {
		t.Fatalf("unmarshal yaml: %v", err)
	}
	if _, err := convertEvents(e); err == nil {
		t.Fatal("expected convertEvents to reject a non-string run_code_review agent_profile_id")
	}
}

// TestLoadTemplates_FeatureDevSingleModelReview guards the shipped template
// contract for the single-model workflow: the review step no longer clears the
// agent context on entry, and the verification (QA) step starts a native code
// review.
func TestLoadTemplates_FeatureDevSingleModelReview(t *testing.T) {
	templates, err := LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates() returned error: %v", err)
	}
	var featureDev *models.WorkflowTemplate
	for _, template := range templates {
		if template.ID == "feature-dev" {
			featureDev = template
			break
		}
	}
	if featureDev == nil {
		t.Fatal("feature-dev template not found")
	}

	review := templateStepByName(t, featureDev, "Review")
	for _, a := range review.Events.OnEnter {
		if a.Type == models.OnEnterResetAgentContext {
			t.Error("feature-dev Review step must not reset agent context on entry")
		}
	}

	qa := templateStepByName(t, featureDev, "QA")
	foundReview := false
	for _, a := range qa.Events.OnEnter {
		if a.Type == models.OnEnterRunCodeReview {
			foundReview = true
		}
	}
	if !foundReview {
		t.Error("feature-dev QA step must start a native code review on entry")
	}
}

func templateStepByName(t *testing.T, template *models.WorkflowTemplate, name string) models.StepDefinition {
	t.Helper()
	for _, step := range template.Steps {
		if step.Name == name {
			return step
		}
	}
	t.Fatalf("template %q has no step named %q", template.ID, name)
	return models.StepDefinition{}
}
