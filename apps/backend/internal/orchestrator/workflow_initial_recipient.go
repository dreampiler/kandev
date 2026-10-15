package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kandev/kandev/internal/task/models"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
)

func (s *Service) applyInitialRecipientBinding(
	ctx context.Context,
	task *models.Task,
	initial *models.TaskSession,
	profileID string,
) (*models.TaskSession, string, error) {
	binding, err := models.LoadWorkflowInitialRecipientBinding(task.Metadata)
	if err != nil || binding == nil {
		return initial, profileID, err
	}
	initialID := binding.InitialSessionID
	if initial != nil {
		initialID = initial.ID
	}
	if snapshot, ok := models.LoadWorkflowInitialSessionSnapshot(task.Metadata); ok {
		initialID = snapshot.SessionID
	}
	if initialID == "" || initialID != binding.InitialSessionID {
		return nil, "", fmt.Errorf("initial recipient binding provenance mismatch")
	}
	recipient, err := s.repo.GetTaskSession(ctx, binding.SessionID)
	if errors.Is(err, models.ErrTaskSessionNotFound) {
		return nil, binding.AgentProfileID, nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("load initial recovery recipient: %w", err)
	}
	if recipient == nil || recipient.TaskID != task.ID || recipient.AgentProfileID != binding.AgentProfileID {
		return nil, "", fmt.Errorf("initial recovery recipient identity mismatch")
	}
	if isTerminalSessionState(recipient.State) {
		return nil, binding.AgentProfileID, nil
	}
	return recipient, binding.AgentProfileID, nil
}

type initialRecipientPromoter interface {
	SetSessionPrimaryWithInitialRecipientBinding(context.Context, string, models.WorkflowInitialRecipientBinding) error
}

func (s *Service) resolveWorkflowEntryTarget(
	ctx context.Context, taskID string, step *wfmodels.WorkflowStep, operationID string,
) (workflowSessionTargetResolution, error) {
	if step.SessionTarget == nil || step.SessionTarget.Kind != wfmodels.WorkflowSessionTargetInitial {
		return s.resolveWorkflowSessionTarget(ctx, taskID, step)
	}
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		return workflowSessionTargetResolution{}, err
	}
	if task != nil {
		route, ok := models.LoadWorkflowSessionRoute(task.Metadata)
		if ok && route.OperationID == operationID && workflowSessionRouteMatchesStep(&route, step, route.AgentProfileID) {
			return workflowSessionTargetResolution{profileID: route.AgentProfileID}, nil
		}
	}
	return s.resolveWorkflowSessionTarget(ctx, taskID, step)
}

// RecoverInitialWorkflowRecipient is an explicit recovery selection. Ordinary
// primary promotion never changes the recipient of future initial entries.
func (s *Service) RecoverInitialWorkflowRecipient(ctx context.Context, sessionID, sourceID string) error {
	if strings.TrimSpace(sourceID) == "" || sourceID == sessionID {
		return fmt.Errorf("a distinct initial recovery source is required")
	}
	if err := s.authorizeSession(ctx, sessionID); err != nil {
		return err
	}
	destination, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		return err
	}
	releaseRoute := s.acquireSessionRouteOperationLock(sourceID)
	defer releaseRoute()
	ctx, release := s.lockCeilingEntryAdmission(ctx, destination.TaskID)
	defer release()
	return s.setPrimarySessionTransferringQueue(ctx, sessionID, sourceID)
}

func (s *Service) registerInitialRecipientPrimary(ctx context.Context, sessionID, sourceID string) error {
	promoter, ok := s.repo.(initialRecipientPromoter)
	if !ok {
		return fmt.Errorf("repository does not support initial recipient recovery")
	}
	destination, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		return err
	}
	task, err := s.repo.GetTask(ctx, destination.TaskID)
	if err != nil {
		return err
	}
	if task == nil || taskArchived(task) || task.IsFromOffice {
		return fmt.Errorf("task does not allow ordinary initial recipient recovery")
	}
	if s.agentManager == nil {
		return fmt.Errorf("initial recipient profile resolver is unavailable")
	}
	if s.profileExecutionResolver != nil {
		if err := s.profileExecutionResolver.ValidateProfile(ctx, destination.AgentProfileID); err != nil {
			return fmt.Errorf("validate initial recovery recipient profile: %w", err)
		}
	} else if _, err := s.agentManager.ResolveAgentProfile(ctx, destination.AgentProfileID); err != nil {
		return fmt.Errorf("resolve initial recovery recipient profile: %w", err)
	}
	if s.sessionRuntimeLive(ctx, sourceID) {
		return fmt.Errorf("initial recovery source still has a live runtime")
	}
	sessions, err := s.repo.ListTaskSessions(ctx, task.ID)
	if err != nil {
		return err
	}
	initialID, err := models.RecordedWorkflowInitialSessionID(task.Metadata)
	if err != nil {
		return err
	}
	if original := previewOriginalTaskSessionFromTask(task, sessions); original != nil {
		initialID = original.ID
	}
	return promoter.SetSessionPrimaryWithInitialRecipientBinding(ctx, sessionID, models.WorkflowInitialRecipientBinding{
		InitialSessionID: initialID, SourceSessionID: sourceID,
		SessionID: sessionID, AgentProfileID: destination.AgentProfileID,
	})
}
