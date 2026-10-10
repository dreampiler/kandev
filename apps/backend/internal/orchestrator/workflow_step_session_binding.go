package orchestrator

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/task/models"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
)

// workflowSessionBindingSessionSetter moves a step binding's session without
// the step-entry ordering guard the ordinary upsert applies. It is implemented
// by the SQLite repository; fakes that omit it make the promotion refresh a
// no-op rather than a silent conditional write that can be rejected.
type workflowSessionBindingSessionSetter interface {
	SetWorkflowSessionBindingSession(ctx context.Context, binding *models.WorkflowSessionBinding) (bool, error)
}

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

// buildWorkflowSourceBinding assembles the durable binding record for a step
// and the session that owns it, or nil when the step does not participate
// (no direct profile, an explicit session target, or the task is not on the
// step). It is shared by the step-entry writer and the promotion override.
func (s *Service) buildWorkflowSourceBinding(
	ctx context.Context,
	taskID string,
	step *wfmodels.WorkflowStep,
	session *models.TaskSession,
	entryIDs ...int64,
) (*models.WorkflowSessionBinding, error) {
	if step == nil || session == nil || step.AgentProfileID == "" || step.SessionTarget != nil {
		return nil, nil
	}
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load task before workflow source binding: %w", err)
	}
	if task == nil || (task.WorkflowStepID != "" && task.WorkflowStepID != step.ID) {
		return nil, nil
	}
	effectiveProfileID := s.resolveStepAgentProfileForTask(ctx, task, step)
	if effectiveProfileID == "" {
		effectiveProfileID = step.AgentProfileID
	}
	updatedAt := task.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	return &models.WorkflowSessionBinding{
		TaskID:         taskID,
		TargetKey:      workflowSessionBindingTargetKey(step.ID),
		WorkflowID:     step.WorkflowID,
		AgentProfileID: effectiveProfileID,
		SessionID:      session.ID,
		OperationID:    workflowSourceBindingOperationID(s.workflowEntryIdentity(ctx, taskID, entryIDs...)),
		UpdatedAt:      updatedAt,
	}, nil
}

// refreshCurrentStepSessionBinding moves the task's durable step binding onto
// the session that now drives it, so a promotion that re-homes the driving
// session also moves the binding the step-complete judgment reads instead of
// leaving it on the replaced session. It writes through the unconditional
// session setter: a promotion keeps the step's operation identity and timestamp,
// so the order-guarded upsert would reject it. Best-effort: a read or write
// failure is logged and never blocks the promotion.
func (s *Service) refreshCurrentStepSessionBinding(ctx context.Context, sessionID string) {
	if s == nil || s.repo == nil || sessionID == "" {
		return
	}
	setter, ok := s.repo.(workflowSessionBindingSessionSetter)
	if !ok {
		return
	}
	binding, err := s.promotedSessionStepBinding(ctx, sessionID)
	if err != nil {
		s.logger.Warn("failed to build the promoted session's step binding",
			zap.String("session_id", sessionID),
			zap.Error(err))
		return
	}
	if binding == nil {
		return
	}
	if _, err := setter.SetWorkflowSessionBindingSession(ctx, binding); err != nil {
		s.logger.Warn("failed to move the step session binding to the promoted session",
			zap.String("task_id", binding.TaskID),
			zap.String("session_id", binding.SessionID),
			zap.String("step_id", binding.TargetKey),
			zap.Error(err))
	}
}

// promotedSessionStepBinding builds the durable binding record for the current
// step of the task the promoted session belongs to, or nil when the task is not
// on an eligible step.
func (s *Service) promotedSessionStepBinding(ctx context.Context, sessionID string) (*models.WorkflowSessionBinding, error) {
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil || session.TaskID == "" {
		return nil, nil
	}
	task, err := s.repo.GetTask(ctx, session.TaskID)
	if err != nil {
		return nil, err
	}
	if task == nil || task.WorkflowStepID == "" {
		return nil, nil
	}
	step := s.lookupWorkflowStep(ctx, task.WorkflowStepID)
	if step == nil || step.ID != task.WorkflowStepID {
		return nil, nil
	}
	return s.buildWorkflowSourceBinding(ctx, session.TaskID, step, session)
}
