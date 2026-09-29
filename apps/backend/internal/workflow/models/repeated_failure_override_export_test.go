package models

import (
	"encoding/json"
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
