package models

import (
	"encoding/json"
	"strings"
	"testing"

	taskmodels "github.com/kandev/kandev/internal/task/models"
)

func TestBuildWorkflowExportIncludesRepeatedFailureOverride(t *testing.T) {
	disabled := false
	export := BuildWorkflowExport([]*taskmodels.Workflow{{
		ID:   "workflow-1",
		Name: "Workflow",
	}}, map[string][]*WorkflowStep{"workflow-1": {{
		ID:                           "step-1",
		Name:                         "Step",
		AllowRepeatedFailureFallback: &disabled,
	}}}, nil)

	payload, err := json.Marshal(export.Workflows[0])
	if err != nil {
		t.Fatalf("marshal portable workflow: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("decode portable workflow: %v", err)
	}
	stepFields := fields["steps"].([]any)[0].(map[string]any)
	value, ok := stepFields["allow_repeated_failure_fallback"]
	if !ok {
		t.Fatal("allow_repeated_failure_fallback missing from portable export")
	}
	if enabled, ok := value.(bool); !ok || enabled {
		t.Fatalf("allow_repeated_failure_fallback = %v, want false", value)
	}
}

func TestBuildWorkflowExportOmitsUnsetRepeatedFailureOverride(t *testing.T) {
	export := BuildWorkflowExport([]*taskmodels.Workflow{{
		ID:   "workflow-1",
		Name: "Workflow",
	}}, map[string][]*WorkflowStep{"workflow-1": {{
		ID:   "step-1",
		Name: "Step",
	}}}, nil)

	payload, err := json.Marshal(export.Workflows[0])
	if err != nil {
		t.Fatalf("marshal portable workflow: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("decode portable workflow: %v", err)
	}
	stepFields := fields["steps"].([]any)[0].(map[string]any)
	if _, ok := stepFields["allow_repeated_failure_fallback"]; ok {
		t.Fatal("unset allow_repeated_failure_fallback should be omitted")
	}
}

func TestBuildWorkflowExportProjectsNewVetoToLegacyAlias(t *testing.T) {
	export := BuildWorkflowExport([]*taskmodels.Workflow{{
		ID:   "workflow-1",
		Name: "Workflow",
	}}, map[string][]*WorkflowStep{"workflow-1": {{
		ID:                          "step-1",
		Name:                        "Step",
		DisableUnclassifiedFallback: true,
	}}}, nil)

	payload, err := json.Marshal(export.Workflows[0])
	if err != nil {
		t.Fatalf("marshal portable workflow: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("decode portable workflow: %v", err)
	}
	stepFields := fields["steps"].([]any)[0].(map[string]any)
	if got := stepFields["disable_unclassified_fallback"]; got != true {
		t.Fatalf("disable_unclassified_fallback = %v, want true", got)
	}
	if got := stepFields["allow_repeated_failure_fallback"]; got != false {
		t.Fatalf("legacy allow alias = %v, want false", got)
	}
}

func TestStepPortableFallbackFieldCompatibility(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantDisable bool
		wantAllow   *bool
		wantErr     string
	}{
		{
			name:        "legacy veto maps to new field",
			body:        `{"position":1,"allow_repeated_failure_fallback":false}`,
			wantDisable: true,
			wantAllow:   boolPtrForStepFallback(false),
		},
		{
			name:        "new veto projects the legacy alias",
			body:        `{"position":1,"disable_unclassified_fallback":true}`,
			wantDisable: true,
			wantAllow:   boolPtrForStepFallback(false),
		},
		{
			name:    "conflicting aliases are rejected",
			body:    `{"position":1,"allow_repeated_failure_fallback":false,"disable_unclassified_fallback":false}`,
			wantErr: "fallback fields conflict",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var step StepPortable
			err := json.Unmarshal([]byte(tt.body), &step)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("unmarshal error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unmarshal step: %v", err)
			}
			if step.DisableUnclassifiedFallback != tt.wantDisable {
				t.Fatalf("disable_unclassified_fallback = %v, want %v", step.DisableUnclassifiedFallback, tt.wantDisable)
			}
			if !samePortableFallback(step.AllowRepeatedFailureFallback, tt.wantAllow) {
				t.Fatalf("allow_repeated_failure_fallback = %v, want %v", step.AllowRepeatedFailureFallback, tt.wantAllow)
			}
		})
	}
}

func boolPtrForStepFallback(value bool) *bool { return &value }

func samePortableFallback(left, right *bool) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
