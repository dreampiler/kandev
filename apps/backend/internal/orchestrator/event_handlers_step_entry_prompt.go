package orchestrator

import (
	"context"

	"github.com/kandev/kandev/internal/task/models"
)

// workflowEntryPromptPendingForStep reports whether the workflow entry that
// brought the task to stepID still has a valid queued entry prompt
// (prompt_ensure or workflow_step_ensure). KANDEV-W-086: advancing on the
// step's completion signal while that prompt was never delivered is exactly
// the "entered the step but the step's turn never ran" skip, so the signal
// gate holds the transition until the sweep delivers or drops the record.
// Fail closed on ownership uncertainty: a record that cannot be read is not
// proof the entry prompt was delivered.
func (s *Service) workflowEntryPromptPendingForStep(
	ctx context.Context,
	task *models.Task,
	stepID string,
) bool {
	if task == nil || stepID == "" || task.WorkflowStepID != stepID {
		return false
	}
	deferral, ok := s.pendingStepEntryDeferral(ctx, task)
	if !ok {
		return false
	}
	if !isStepEntryPromptKind(deferral.Kind) {
		return false
	}
	if !stepEntryBindingTargetsStep(deferral, stepID) {
		return false
	}
	disposition, _, validationErr := s.validateCeilingEntry(ctx, task, deferral)
	if validationErr != nil {
		return true
	}
	return disposition == ceilingEntryValid || disposition == ceilingEntryUnavailable
}

// pendingStepEntryDeferral reads the task's ceiling-deferred launch, when one
// exists. ok is false when there is no ceiling record at all; a record that
// cannot be decoded is returned as-is so the caller can fail closed on it.
func (s *Service) pendingStepEntryDeferral(
	ctx context.Context,
	task *models.Task,
) (models.CeilingDeferral, bool) {
	raw, _, err := s.repo.GetTaskDeferredLaunch(ctx, task.ID)
	if err != nil || raw == nil {
		return models.CeilingDeferral{}, false
	}
	ceilingFlag, hasCeilingFlag := raw[models.CeilingDeferredKey]
	if !hasCeilingFlag || ceilingFlag != true {
		return models.CeilingDeferral{}, false
	}
	deferral, err := models.ReadCeilingDeferral(raw)
	if err != nil {
		return deferral, true
	}
	return deferral, true
}

// isStepEntryPromptKind reports whether kind is one of the deferred launches
// that carries a workflow step's own entry prompt.
func isStepEntryPromptKind(kind models.CeilingLaunchKind) bool {
	return kind == models.CeilingLaunchPromptEnsure ||
		kind == models.CeilingLaunchWorkflowStepEnsure
}

// stepEntryBindingTargetsStep reports whether deferral carries a valid
// workflow-entry binding addressed to stepID. A binding read error, an absent
// binding, or a binding for another step is not this step's pending prompt.
func stepEntryBindingTargetsStep(deferral models.CeilingDeferral, stepID string) bool {
	binding, present, err := models.ReadCeilingWorkflowEntryBinding(deferral.Payload)
	if err != nil || !present {
		return false
	}
	return binding.Valid() && binding.DestinationStepID == stepID
}
