package controller

import (
	"encoding/json"
	"testing"

	"github.com/kandev/kandev/internal/workflow/models"
)

func TestUnclassifiedStepVetoUpdatePresence(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    *bool
		wantErr bool
	}{
		{name: "omitted preserves", body: `{"id":"step-1"}`},
		{name: "false clears", body: `{"id":"step-1","disable_unclassified_fallback":false}`, want: boolPointer(false)},
		{name: "true sets", body: `{"id":"step-1","disable_unclassified_fallback":true}`, want: boolPointer(true)},
		{name: "null is invalid", body: `{"id":"step-1","disable_unclassified_fallback":null}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var request UpdateStepRequest
			err := json.Unmarshal([]byte(tt.body), &request)
			if (err != nil) != tt.wantErr {
				t.Fatalf("unmarshal error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			encoded, err := json.Marshal(request)
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatalf("unmarshal request: %v", err)
			}
			raw, present := fields["disable_unclassified_fallback"]
			if tt.want == nil {
				if present {
					t.Fatalf("field should be omitted, got %s", raw)
				}
				return
			}
			if !present {
				t.Fatal("field presence was lost")
			}
			var got bool
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("unmarshal field: %v", err)
			}
			if got != *tt.want {
				t.Fatalf("field = %v, want %v", got, *tt.want)
			}
		})
	}
}

func TestLegacyStepFallbackRequestFieldRoundTrips(t *testing.T) {
	var request UpdateStepRequest
	if err := json.Unmarshal([]byte(`{"id":"step-1","allow_repeated_failure_fallback":false}`), &request); err != nil {
		t.Fatalf("decode legacy request: %v", err)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("encode legacy request: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("decode encoded request: %v", err)
	}
	raw, present := fields["allow_repeated_failure_fallback"]
	if !present {
		t.Fatal("allow_repeated_failure_fallback presence was lost")
	}
	var got bool
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode legacy field: %v", err)
	}
	if got {
		t.Fatal("allow_repeated_failure_fallback = true, want false")
	}
}

func TestLegacyStepFallbackUpdateNullClearsAlias(t *testing.T) {
	var request UpdateStepRequest
	if err := json.Unmarshal([]byte(`{"id":"step-1","allow_repeated_failure_fallback":null}`), &request); err != nil {
		t.Fatalf("decode explicit clear: %v", err)
	}
	if !request.allowRepeatedFailureFallbackPresent {
		t.Fatal("explicit legacy null lost field presence")
	}
	step := &models.WorkflowStep{DisableUnclassifiedFallback: true, AllowRepeatedFailureFallback: boolPointer(false)}
	if err := applyStepFallbackRequest(step, request.AllowRepeatedFailureFallback, request.allowRepeatedFailureFallbackPresent, request.DisableUnclassifiedFallback); err != nil {
		t.Fatalf("apply legacy clear: %v", err)
	}
	if step.DisableUnclassifiedFallback || step.AllowRepeatedFailureFallback != nil {
		t.Fatalf("step after explicit clear = disable:%v allow:%v, want false/nil", step.DisableUnclassifiedFallback, step.AllowRepeatedFailureFallback)
	}
}

func boolPointer(value bool) *bool { return &value }
