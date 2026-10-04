package orchestrator

import (
	"context"
	"sort"

	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	"go.uber.org/zap"
)

func (s *Service) admitCeilingLaunch(ctx context.Context, req admissionRequest) admissionDecision {
	return s.finishCeilingAdmission(s.sessionCeiling.admit(ctx, s.orderedCeilingAdmission(req)))
}

func (s *Service) handOffOrAdmitCeilingLaunch(ctx context.Context, req admissionRequest) admissionDecision {
	return s.finishCeilingAdmission(s.sessionCeiling.handOffOrAdmit(ctx, s.orderedCeilingAdmission(req)))
}

func (s *Service) finishCeilingAdmission(decision admissionDecision) admissionDecision {
	if !decision.admitted && decision.populationKnown && decision.classPopulation < decision.classCeiling {
		s.signalCeilingSweep()
	}
	return decision
}

func (s *Service) orderedCeilingAdmission(req admissionRequest) admissionRequest {
	req.yieldToDeferred = func(ctx context.Context, class ceilingClass) bool {
		return s.ceilingDeferredLaunchPrecedes(ctx, req, class)
	}
	return req
}

// The controller calls this read-only selection while it owns the capacity
// mutex. It must not acquire task admission locks or perform runtime dispatch.
func (s *Service) ceilingDeferredLaunchPrecedes(ctx context.Context, req admissionRequest, class ceilingClass) bool {
	lister, ok := s.repo.(ceilingDeferredTaskLister)
	if !ok {
		return false
	}
	tasks, err := lister.ListTasksWithCeilingDeferred(ctx)
	if err != nil {
		s.logger.Zap().Warn("could not select deferred launch before automatic admission",
			zap.String("task_id", req.taskID), zap.Error(err))
		return true
	}
	sort.SliceStable(tasks, func(i, j int) bool { return ceilingReplayLess(tasks[i], tasks[j]) })
	for _, task := range tasks {
		deferral, eligible := s.ceilingAdmissionCandidate(ctx, task)
		if !eligible || s.sessionCeiling.classOfLocked(s.ceilingDeferredProfile(ctx, deferral)) != class {
			continue
		}
		if destination := models.CeilingDeferralSessionID(task, deferral); destination != "" &&
			s.sessionCeiling.reservations[destination] != nil {
			continue
		}
		return task.ID != req.taskID || sessionIDFromCeilingPayload(deferral) != req.sessionID
	}
	return false
}

func (s *Service) ceilingAdmissionCandidate(ctx context.Context, task *models.Task) (models.CeilingDeferral, bool) {
	if task == nil || taskArchived(task) || task.State == v1.TaskStateCancelled {
		return models.CeilingDeferral{}, false
	}
	record, _ := task.Metadata[models.MetaKeyDeferredLaunch].(map[string]interface{})
	deferral, err := models.ReadCeilingDeferral(record)
	if err != nil || s.deferredRetrySchedule.failureWaiting(task.ID, ceilingDeferralIdentityKey(deferral)) {
		return deferral, false
	}
	if deferral.Kind == models.CeilingLaunchStart {
		if _, _, drop := s.evaluateCeilingStartDropReasons(ctx, task, deferral.Payload); drop {
			return deferral, false
		}
	}
	disposition, _, err := s.validateCeilingEntryWithDestinationState(ctx, task, deferral, true)
	return deferral, err == nil && disposition == ceilingEntryValid
}

func (s *Service) ceilingDeferredProfile(ctx context.Context, deferral models.CeilingDeferral) string {
	if sessionID := sessionIDFromCeilingPayload(deferral); sessionID != "" {
		if session, err := s.repo.GetTaskSession(ctx, sessionID); err == nil && session != nil {
			return session.AgentProfileID
		}
		return ""
	}
	return stringField(deferral.Payload, metaKeyAgentProfileID)
}
