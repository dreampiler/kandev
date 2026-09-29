package repository

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/workflow/models"
)

func boolPtr(value bool) *bool { return &value }

func TestStepAllowRepeatedFailureFallback_TriStateRoundTrip(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		value *bool
	}{
		{name: "nil defers to profile", value: nil},
		{name: "false forbids fallback", value: boolPtr(false)},
		{name: "true defers to profile", value: boolPtr(true)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := setupTestRepo(t)
			step := &models.WorkflowStep{
				WorkflowID:                   "wf-test",
				Name:                         "Step",
				Position:                     0,
				Color:                        "#000000",
				DisableUnclassifiedFallback:  tt.value != nil && !*tt.value,
				AllowRepeatedFailureFallback: tt.value,
			}
			if err := repo.CreateStep(ctx, step); err != nil {
				t.Fatalf("CreateStep: %v", err)
			}
			retrieved, err := repo.GetStep(ctx, step.ID)
			if err != nil {
				t.Fatalf("GetStep: %v", err)
			}
			if !sameOptionalBool(retrieved.AllowRepeatedFailureFallback, tt.value) {
				t.Fatalf("created value = %v, want %v", retrieved.AllowRepeatedFailureFallback, tt.value)
			}

			// Update to the opposite tri-state value and re-read.
			updated := boolPtr(true)
			if tt.value == nil || *tt.value {
				updated = boolPtr(false)
			}
			retrieved.AllowRepeatedFailureFallback = updated
			retrieved.DisableUnclassifiedFallback = !*updated
			if err := repo.UpdateStep(ctx, retrieved); err != nil {
				t.Fatalf("UpdateStep: %v", err)
			}
			after, err := repo.GetStep(ctx, step.ID)
			if err != nil {
				t.Fatalf("GetStep after update: %v", err)
			}
			if !sameOptionalBool(after.AllowRepeatedFailureFallback, updated) {
				t.Fatalf("updated value = %v, want %v", after.AllowRepeatedFailureFallback, updated)
			}
		})
	}
}

func sameOptionalBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
