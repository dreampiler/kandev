package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	"github.com/kandev/kandev/internal/system/sessioncapacity"
	"github.com/kandev/kandev/internal/task/models"
)

type executionGradeSource interface {
	GetAgentProfile(context.Context, string) (*settingsmodels.AgentProfile, error)
}

type admittedExecutionLister interface {
	ListAdmittedExecutions(context.Context) ([]models.AdmittedExecution, error)
}

type admittedRunExecutionLister interface {
	ListAdmittedRunExecutions(context.Context) ([]models.AdmittedExecution, error)
}

type admittedGradeWriter interface {
	SetAdmittedExecutionGrade(context.Context, string, string) error
}

// SetExecutionGradeSource wires logical profile lookup before launch consumers start.
func (s *Service) SetExecutionGradeSource(source executionGradeSource) {
	if s.sessionCeiling == nil {
		return
	}
	s.sessionCeiling.mu.Lock()
	defer s.sessionCeiling.mu.Unlock()
	s.sessionCeiling.resolveGrade = func(ctx context.Context, req admissionRequest) (string, error) {
		return s.taskExecutionGrade(ctx, req, source)
	}
	if writer, ok := s.repo.(admittedGradeWriter); ok {
		s.sessionCeiling.persistGrade = writer.SetAdmittedExecutionGrade
	}
	s.sessionCeiling.profileGrade = func(ctx context.Context, id string) (string, error) { return logicalExecutionGrade(ctx, source, id) }
}

func (s *Service) taskExecutionGrade(ctx context.Context, req admissionRequest, source executionGradeSource) (string, error) {
	if req.taskID == "" {
		return req.grade, nil
	}
	task, err := s.repo.GetTask(ctx, req.taskID)
	if err != nil {
		return "", err
	}
	if task == nil {
		return "", errors.New("execution grade task unavailable")
	}
	if req.startSelection != nil && !req.profileExplicit {
		if err := s.captureExecutionGradeStartSelection(ctx, task, req); err != nil {
			return "", err
		}
		req.profileID = req.startSelection.profileID
	}
	if req.sessionID != "" {
		session, err := s.repo.GetTaskSession(ctx, req.sessionID)
		if err != nil {
			return "", err
		}
		if session == nil || session.TaskID != task.ID {
			return "", errors.New("execution grade session does not belong to task")
		}
		if models.IsAdmittedSessionState(session.State) {
			return session.AdmittedExecutionGrade, nil
		}
		if req.profileID == "" {
			req.profileID = session.AgentProfileID
		}
	}
	if task.ExecutionGrade != "" {
		return task.ExecutionGrade, nil
	}
	return s.logicalTaskExecutionGrade(ctx, task, req, source)
}

func (s *Service) logicalTaskExecutionGrade(ctx context.Context, task *models.Task, req admissionRequest, source executionGradeSource) (string, error) {
	if !req.profileExplicit && req.startSelection == nil && (req.seam == "startCreatedSession" || req.workflowStepID != "") {
		profileID, err := s.resolveEffectiveAgentProfile(ctx, task.ID, req.workflowStepID, req.profileID)
		if err != nil {
			return "", err
		}
		req.profileID = profileID
	}
	if req.profileID == "" {
		req.profileID, _ = s.resolveTaskAgentProfile(ctx, task)
	}
	return logicalExecutionGrade(ctx, source, req.profileID)
}

func logicalExecutionGrade(ctx context.Context, source executionGradeSource, id string) (string, error) {
	if source == nil || id == "" {
		return "", nil
	}
	profile, err := source.GetAgentProfile(ctx, id)
	if err != nil {
		return "", err
	}
	if profile == nil {
		return "", errors.New("logical execution profile unavailable")
	}
	if profile.ExecutionAgentProfileID != "" {
		profile, err = source.GetAgentProfile(ctx, profile.ExecutionAgentProfileID)
		if err != nil {
			return "", err
		}
		if profile == nil {
			return "", errors.New("bound logical execution profile unavailable")
		}
	}
	return profile.ExecutionGrade, nil
}

// ProfileExecutionGrade reads the logical binding without selecting a model candidate.
func (s *Service) ProfileExecutionGrade(ctx context.Context, profileID string) (string, error) {
	if s == nil || s.sessionCeiling == nil || s.sessionCeiling.profileGrade == nil {
		return "", nil
	}
	return s.sessionCeiling.profileGrade(ctx, profileID)
}

// SetExecutionCapacity atomically applies the overall, policy and grade settings.
func (s *Service) SetExecutionCapacity(effective sessioncapacity.Effective) {
	if s == nil || s.sessionCeiling == nil {
		return
	}
	c := s.sessionCeiling
	c.mu.Lock()
	c.ceiling, c.policy = effective.MaxSessions, effective.Policy
	c.gradeLimits = maps.Clone(effective.GradeLimits)
	c.mu.Unlock()
	s.signalCeilingSweep()
}

func (c *sessionCeilingController) requestGradeLocked(ctx context.Context, req admissionRequest) (admissionRequest, error) {
	if c.policy != sessioncapacity.PolicyAll {
		req.startSelection = nil
	}
	if held := c.reservations[req.sessionID]; req.sessionID != "" && held != nil {
		req.grade = held.grade
		req.reservationHeld = true
		return req, nil
	}
	if c.resolveGrade != nil {
		grade, err := c.resolveGrade(ctx, req)
		if err != nil {
			return req, err
		}
		req.grade = grade
	}
	return req, nil
}

func (c *sessionCeilingController) persistDecisionLocked(ctx context.Context, req admissionRequest, decision admissionDecision) admissionDecision {
	if !decision.admitted || req.reservationHeld || decision.reservationKey == "" || req.sessionID == "" || strings.HasPrefix(req.sessionID, "office-run:") || c.persistGrade == nil {
		return decision
	}
	if err := c.persistGrade(ctx, req.sessionID, req.grade); err != nil {
		delete(c.reservations, decision.reservationKey)
		return admissionDecision{reasonCode: ceilingReasonUnknownPopulation, ceiling: c.ceiling}
	}
	return decision
}

func (c *sessionCeilingController) gradePopulationLocked(counted map[string]struct{}, grade string) int {
	population := 0
	for id := range counted {
		if c.persistedGrades[id] == grade {
			population++
		}
	}
	for _, reservation := range c.reservations {
		if reservation.grade != grade {
			continue
		}
		if _, found := counted[reservation.sessionID]; reservation.sessionID == "" || !found {
			population++
		}
	}
	return population
}

func (c *sessionCeilingController) gradeFullLocked(counted map[string]struct{}, grade string) bool {
	maximum, limited := c.gradeLimits[grade]
	return c.policy == sessioncapacity.PolicyAll && grade != "" && limited && c.gradePopulationLocked(counted, grade) >= maximum
}

func (c *sessionCeilingController) persistReservationGrade(ctx context.Context, key, sessionID string) error {
	if c == nil || c.persistGrade == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	reservation := c.reservations[key]
	if reservation == nil {
		return errors.New("execution grade reservation unavailable")
	}
	if occupied := c.reservations[sessionID]; occupied != nil && occupied != reservation {
		return nil
	}
	counted, err := c.countedRowsLocked(ctx)
	if err != nil {
		return err
	}
	if _, active := counted[sessionID]; active {
		delete(c.reservations, key)
		return nil
	}
	return c.persistGrade(ctx, sessionID, reservation.grade)
}

func stringCeilingPayload(payload map[string]interface{}, key string) string {
	value, _ := payload[key].(string)
	return value
}

func (c *sessionCeilingController) appendGradedRowsLocked(rows []models.AdmittedExecution, counted map[string]struct{}, prefix string) {
	for _, row := range rows {
		counted[prefix+row.ID] = struct{}{}
		c.persistedGrades[prefix+row.ID] = row.Grade
	}
}

func (c *sessionCeilingController) appendTaskPopulationLocked(ctx context.Context, counted map[string]struct{}) error {
	if c.lister == nil {
		return nil
	}
	if graded, ok := c.lister.(admittedExecutionLister); ok {
		rows, err := graded.ListAdmittedExecutions(ctx)
		if err != nil {
			return err
		}
		c.appendGradedRowsLocked(rows, counted, "")
		return nil
	}
	if len(c.gradeLimits) > 0 {
		return fmt.Errorf("task execution grade population unavailable")
	}
	ids, err := c.lister.ListAdmittedSessionIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		counted[id] = struct{}{}
	}
	return nil
}

func (c *sessionCeilingController) appendRunPopulationLocked(ctx context.Context, counted map[string]struct{}) error {
	if c.runSessions == nil {
		return nil
	}
	if graded, ok := c.runSessions.(admittedRunExecutionLister); ok {
		rows, err := graded.ListAdmittedRunExecutions(ctx)
		if err != nil {
			return err
		}
		c.appendGradedRowsLocked(rows, counted, "office-run:")
		return nil
	}
	if len(c.gradeLimits) > 0 {
		return fmt.Errorf("office execution grade population unavailable")
	}
	ids, err := c.runSessions.ListAdmittedRunSessionIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		counted[runSessionCapacityKey(id)] = struct{}{}
	}
	return nil
}
