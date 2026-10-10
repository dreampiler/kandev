package orchestrator

import (
	"context"

	"go.uber.org/zap"
)

// BoundWorkflowStepSession returns the session id durably bound to a task's
// workflow step, or "" when the step has no recorded binding. The step-complete
// judgment reads it as the authoritative owner of a step: a restart or
// substitution can move the task off the session it entered the step with, and
// the durable binding is what still names the session that owns the step.
func (s *Service) BoundWorkflowStepSession(ctx context.Context, taskID, workflowStepID string) (string, error) {
	if s == nil || s.repo == nil || taskID == "" || workflowStepID == "" {
		return "", nil
	}
	store, ok := s.repo.(workflowSessionBindingStore)
	if !ok {
		return "", nil
	}
	binding, err := store.GetWorkflowSessionBinding(ctx, taskID, workflowSessionBindingTargetKey(workflowStepID))
	if err != nil {
		return "", err
	}
	if binding == nil || binding.TaskID != taskID || binding.SessionID == "" {
		return "", nil
	}
	return binding.SessionID, nil
}

// refreshCurrentStepSessionBinding moves the task's durable step binding onto
// the session that now drives it, so a promotion that re-homes the driving
// session also moves the binding the step-complete judgment reads instead of
// leaving it on the replaced session. Best-effort: a read or write failure is
// logged and never blocks the promotion.
func (s *Service) refreshCurrentStepSessionBinding(ctx context.Context, sessionID string) {
	if s == nil || s.repo == nil || sessionID == "" {
		return
	}
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil || session == nil || session.TaskID == "" {
		return
	}
	task, err := s.repo.GetTask(ctx, session.TaskID)
	if err != nil || task == nil || task.WorkflowStepID == "" {
		return
	}
	step := s.lookupWorkflowStep(ctx, task.WorkflowStepID)
	if step == nil || step.ID != task.WorkflowStepID {
		return
	}
	if err := s.recordWorkflowSourceBinding(ctx, session.TaskID, step, session); err != nil {
		s.logger.Warn("failed to move the step session binding to the promoted session",
			zap.String("task_id", session.TaskID),
			zap.String("session_id", sessionID),
			zap.String("step_id", step.ID),
			zap.Error(err))
	}
}
