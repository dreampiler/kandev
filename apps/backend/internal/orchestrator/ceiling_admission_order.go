package orchestrator

import (
	"context"
	"sort"
	"time"

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
	req.yieldToDeferred = func(ctx context.Context, class ceilingClass) deferredPrecedence {
		return s.ceilingDeferredLaunchPrecedes(ctx, req, class)
	}
	return req
}

// The controller calls this read-only selection while it owns the capacity
// mutex. It must not acquire task admission locks or perform runtime dispatch.
//
// A queued launch that a dispatcher already holds the durable claim for is
// being launched now, so it claims no additional priority: otherwise one
// in-flight record would keep every later automatic launch and every later
// queued launch out of a free slot until its lease expires.
func (s *Service) ceilingDeferredLaunchPrecedes(ctx context.Context, req admissionRequest, class ceilingClass) deferredPrecedence {
	lister, ok := s.repo.(ceilingDeferredTaskLister)
	if !ok {
		return deferredPrecedence{}
	}
	tasks, err := lister.ListTasksWithCeilingDeferred(ctx)
	if err != nil {
		s.logger.Zap().Warn("could not select deferred launch before automatic admission",
			zap.String("task_id", req.taskID), zap.Error(err))
		return deferredPrecedence{precedes: true, state: deferredPrecedesListUnavailable}
	}
	sort.SliceStable(tasks, func(i, j int) bool { return ceilingReplayLess(tasks[i], tasks[j]) })
	for _, task := range tasks {
		deferral, eligible := s.ceilingAdmissionCandidate(ctx, task)
		if !eligible || s.sessionCeiling.classOfLocked(s.ceilingDeferredProfile(ctx, deferral)) != class {
			continue
		}
		destination := models.CeilingDeferralSessionID(task, deferral)
		if destination != "" && s.sessionCeiling.reservations[destination] != nil {
			continue
		}
		return deferredPrecedence{
			precedes:  !s.ceilingRequestOwnsDeferredRecord(ctx, req, task.ID, deferral),
			taskID:    task.ID,
			sessionID: destination,
			kind:      deferral.Kind,
			state:     deferredPrecedesEligible,
		}
	}
	return deferredPrecedence{}
}

// ceilingRequestOwnsDeferredRecord reports whether the deferred record the
// deferred-order check just selected is the very launch this request is
// already dispatching, so it must not be ranked ahead of itself. Two forms
// count as the same launch: a request already holding the durable dispatch
// claim for that exact record (the sweep and Send Now both claim before they
// dispatch, and seam 1 has no session id to compare), and a request naming
// the same task and the same destination session the record carries. A request
// that finds an older, different record on its own task holds no claim and
// therefore still yields, as does a same-task launch naming another session.
func (s *Service) ceilingRequestOwnsDeferredRecord(
	ctx context.Context, req admissionRequest, taskID string, deferral models.CeilingDeferral,
) bool {
	if claim, _ := ctx.Value(ceilingDispatchClaimContextKey{}).(*ceilingDeferredLaunchClaim); claim != nil &&
		claim.taskID == taskID {
		if equivalent, err := sameCeilingDeferralIdentity(claim.deferral, deferral); err == nil && equivalent {
			return true
		}
	}
	return taskID == req.taskID && sessionIDFromCeilingPayload(deferral) == req.sessionID
}

func (s *Service) ceilingAdmissionCandidate(ctx context.Context, task *models.Task) (models.CeilingDeferral, bool) {
	if task == nil || taskArchived(task) || task.State == v1.TaskStateCancelled {
		return models.CeilingDeferral{}, false
	}
	record, _ := task.Metadata[models.MetaKeyDeferredLaunch].(map[string]interface{})
	if claim, held := models.ReadCeilingLaunchClaimDetails(record); held && !claim.Expired(time.Now().UTC()) {
		// A dispatcher owns this exact record and is dispatching it now. It
		// takes the next free slot itself, so it must not also reserve one
		// against every other automatic and queued launch until its lease ends.
		return models.CeilingDeferral{}, false
	}
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
