package orchestrator

import (
	"context"
	"fmt"

	"github.com/kandev/kandev/internal/task/models"
)

// explicitWorkflowStartSelection is the launch's existing route frame, selected
// once before admission. Only its later preparation persists or promotes it.
type explicitWorkflowStartSelection struct {
	entryID   int64
	resolved  bool
	credited  bool
	profileID string
	session   *models.TaskSession
	route     *models.WorkflowSessionRoute
}

func (s *Service) captureExecutionGradeStartSelection(ctx context.Context, task *models.Task, req admissionRequest) error {
	selection := req.startSelection
	stepID := req.workflowStepID
	if stepID == "" {
		stepID = task.WorkflowStepID
	}
	session, profileID, route, err := s.resolveExplicitWorkflowStartRoute(ctx, task.ID, stepID, task, selection.entryID)
	if err != nil {
		return err
	}
	if profileID == "" {
		profileID = req.profileID
	}
	selection.resolved, selection.profileID = true, profileID
	selection.session, selection.route = session, route
	return nil
}

// resolveExplicitWorkflowStartRoute shares the native route selection with grade
// admission. A supplied task requests its read-only preview (no legacy marker
// backfill); the ordinary automatic-policy path keeps the original resolver.
func (s *Service) resolveExplicitWorkflowStartRoute(
	ctx context.Context, taskID, stepID string, previewTask *models.Task, entryIDs ...int64,
) (*models.TaskSession, string, *models.WorkflowSessionRoute, error) {
	if stepID == "" || s.workflowStepGetter == nil {
		return nil, "", nil, nil
	}
	step, err := s.workflowStepGetter.GetStep(ctx, stepID)
	if err != nil {
		return nil, "", nil, fmt.Errorf("load workflow step for session target: %w", err)
	}
	if step == nil || step.SessionTarget == nil {
		return nil, "", nil, nil
	}
	var session *models.TaskSession
	var profileID string
	if previewTask == nil {
		session, profileID, err = s.selectExplicitWorkflowStartSession(ctx, taskID, step)
	} else {
		session, profileID, err = s.resolvePreviewWorkflowSessionTarget(ctx, taskID, previewTask, step)
		if s.resolveStepProfileSessionStartPolicy(step) != models.WorkflowProfileSessionStartPolicyReuse ||
			(session != nil && isTerminalSessionState(session.State)) {
			session = nil
		}
	}
	if err != nil {
		return nil, "", nil, err
	}
	if profileID == "" {
		return nil, "", nil, fmt.Errorf("workflow session target has no logical agent profile")
	}
	startPolicy := s.resolveStepProfileSessionStartPolicy(step)
	entryIdentity := s.workflowEntryIdentity(ctx, taskID, entryIDs...)
	route := &models.WorkflowSessionRoute{
		OperationID:       workflowSessionRouteID(taskID, step.ID, entryIdentity, step.SessionTarget, startPolicy),
		DestinationStepID: step.ID, EntryIdentity: entryIdentity,
		TargetKind: string(step.SessionTarget.Kind), TargetStepID: step.SessionTarget.StepID,
		AgentProfileID: profileID, Phase: workflowSessionRoutePrepared,
	}
	if recordedRoute, recordedSession, loadErr := s.loadRecordedWorkflowSessionRoute(ctx, taskID, route.OperationID, step, profileID); loadErr != nil {
		return nil, "", nil, loadErr
	} else if recordedSession != nil {
		session, route = recordedSession, recordedRoute
	}
	return session, profileID, route, nil
}

// reserveSelectedStartLocked owns a separate launch key while crediting an
// already-counted destination. It neither borrows nor releases another launch's
// reservation, and retains the destination's immutable admitted grade.
func (c *sessionCeilingController) reserveSelectedStartLocked(req admissionRequest, counted map[string]struct{}, population int) (admissionDecision, bool) {
	selection := req.startSelection
	if selection == nil || selection.session == nil {
		return admissionDecision{}, false
	}
	id := selection.session.ID
	if _, active := counted[id]; !active {
		return admissionDecision{}, false
	}
	key := c.reserveLocked("", c.persistedGrades[id])
	c.reservations[key].sessionID = id
	selection.credited = true
	return admissionDecision{admitted: true, reservationKey: key, population: population, populationKnown: true, ceiling: c.ceiling}, true
}

// A destination that loses its nonterminal promotion CAS needs a fresh slot.
// Replace only this launch's alias and reuse the ordinary atomic admission;
// another launch's reservation and the old row's frozen grade stay untouched.
func (c *sessionCeilingController) readmitSelectedStart(ctx context.Context, key string, req admissionRequest) admissionDecision {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.reservations, key)
	return c.admitLocked(ctx, req, req.origin)
}
