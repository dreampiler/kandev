package models

import "testing"

func TestWorkflowStepAdvancesOnTurnComplete(t *testing.T) {
	cases := []struct {
		name    string
		actions []OnTurnCompleteAction
		want    bool
	}{
		{name: "no actions", want: false},
		{name: "disable_plan_mode only", actions: []OnTurnCompleteAction{{Type: OnTurnCompleteDisablePlanMode}}, want: false},
		{name: "move_to_next", actions: []OnTurnCompleteAction{{Type: OnTurnCompleteMoveToNext}}, want: true},
		{name: "move_to_previous", actions: []OnTurnCompleteAction{{Type: OnTurnCompleteMoveToPrevious}}, want: true},
		{name: "move_to_step", actions: []OnTurnCompleteAction{{Type: OnTurnCompleteMoveToStep}}, want: true},
		{
			name: "disable_plan_mode plus move_to_step",
			actions: []OnTurnCompleteAction{
				{Type: OnTurnCompleteDisablePlanMode},
				{Type: OnTurnCompleteMoveToStep, Config: map[string]interface{}{"step_id": "review"}},
			},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step := &WorkflowStep{Events: StepEvents{OnTurnComplete: tc.actions}}
			if got := step.AdvancesOnTurnComplete(); got != tc.want {
				t.Fatalf("AdvancesOnTurnComplete() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestRemapStepEvents_RemapGenericMoveToStep(t *testing.T) {
	events := StepEvents{
		OnChildrenCompleted: []GenericAction{
			{Type: GenericActionMoveToStep, Config: map[string]any{"step_id": "old-step"}},
			{Type: GenericActionMoveToNext},
		},
	}

	remapped := RemapStepEvents(events, map[string]string{"old-step": "new-step"})

	if got := remapped.OnChildrenCompleted[0].Config["step_id"]; got != "new-step" {
		t.Fatalf("generic move_to_step step_id = %v, want new-step", got)
	}
	if got := events.OnChildrenCompleted[0].Config["step_id"]; got != "old-step" {
		t.Fatalf("source events mutated, step_id = %v", got)
	}
}
