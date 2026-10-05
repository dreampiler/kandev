package models

// Step automation is a property of the step's own configuration, never of a
// step name, a workflow, or a person. Every consumer that has to decide whether
// a task parked on a step is waiting for a human or is being driven by the
// system reads it here, so no screen answers that question a second way.

// StepAutomation is the evidence a caller already has when it read a step from
// a query rather than from the workflow service: the step's on_enter actions
// and its pull source.
type StepAutomation struct {
	OnEnter        []OnEnterAction
	PullFromStepID string
}

// RunsOnEntry reports whether arriving at the step starts work by itself:
// either an on_enter action dispatches an agent or a run, or the step pulls
// work forward from another step.
func (a StepAutomation) RunsOnEntry() bool {
	if a.PullFromStepID != "" {
		return true
	}
	for _, action := range a.OnEnter {
		switch action.Type {
		case OnEnterAutoStartAgent, OnEnterQueueRun, OnEnterQueueRunForEachParticipant, OnEnterRunCodeReview:
			return true
		}
	}
	return false
}

// StepRunsOnEntry reports whether entering step starts work by itself. A nil
// step is not an automated step: nothing about it can be read, so it is never
// treated as one.
func StepRunsOnEntry(step *WorkflowStep) bool {
	if step == nil {
		return false
	}
	return StepAutomation{
		OnEnter:        step.Events.OnEnter,
		PullFromStepID: step.PullFromStepID,
	}.RunsOnEntry()
}

// StepIsManual reports whether a task on step waits for a person to act. It is
// the negation of StepRunsOnEntry, including for a nil step.
func StepIsManual(step *WorkflowStep) bool {
	return !StepRunsOnEntry(step)
}
