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
	return s.sessionCeiling.admit(ctx, s.orderedCeilingAdmission(req))
}

func (s *Service) handOffOrAdmitCeilingLaunch(ctx context.Context, req admissionRequest) admissionDecision {
	return s.sessionCeiling.handOffOrAdmit(ctx, s.orderedCeilingAdmission(req))
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
// Only a record ordered before the requesting launch's own queue position is a
// predecessor. A record a dispatcher already holds the durable claim for is
// being launched now and ranks as no additional priority for other launches;
// it must not, however, make a later record look earlier than the launch that
// owns it. The scan therefore recognizes the request's own record (even while
// its own in-flight claim would exclude it from ordinary candidacy) and stops
// there, so ownership, an already-dispatched record, and a deleted or otherwise
// invalid record never make a later record precede this request.
func (s *Service) ceilingDeferredLaunchPrecedes(ctx context.Context, req admissionRequest, class ceilingClass) deferredPrecedence {
	if claim := ceilingDispatchClaimFromContext(ctx); claim != nil && claim.taskID != "" && claim.taskID == req.taskID {
		// A request that already owns a deferred record's dispatch claim is the
		// head of the queue it is dispatching. It takes its own free slot and
		// never yields to a record ordered after it, including when its own
		// record was already cleared between the claim and this admission.
		return deferredPrecedence{}
	}
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
		deferral, readable := s.readCeilingDeferralCandidate(task)
		if !readable {
			continue
		}
		if s.ceilingRequestOwnsDeferredRecord(ctx, req, task.ID, deferral) {
			// The scan reached this request's own queued record. Every record
			// after it is later, so none of them precedes this request.
			return deferredPrecedence{}
		}
		if !s.ceilingAdmissionCandidateEligible(ctx, task, deferral) ||
			s.sessionCeiling.classOfLocked(s.ceilingDeferredProfile(ctx, deferral)) != class {
			continue
		}
		destination := models.CeilingDeferralSessionID(task, deferral)
		if destination != "" && s.sessionCeiling.reservations[destination] != nil {
			continue
		}
		return deferredPrecedence{
			precedes:  true,
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

// readCeilingDeferralCandidate reads a listed task's deferred record without
// judging eligibility. The precedence scan uses it so it can recognize the
// request's own record before the claim-based candidacy filter would hide it.
func (s *Service) readCeilingDeferralCandidate(task *models.Task) (models.CeilingDeferral, bool) {
	if task == nil {
		return models.CeilingDeferral{}, false
	}
	record, _ := task.Metadata[models.MetaKeyDeferredLaunch].(map[string]interface{})
	deferral, err := models.ReadCeilingDeferral(record)
	if err != nil {
		return models.CeilingDeferral{}, false
	}
	return deferral, true
}

// ceilingAdmissionCandidateEligible reports whether a deferred record can rank
// as an earlier queued launch: it is not already being dispatched, has not
// failed its replay for a non-capacity reason, has been retried recently
// enough to still be live, and is still a valid entry for its task.
func (s *Service) ceilingAdmissionCandidateEligible(
	ctx context.Context, task *models.Task, deferral models.CeilingDeferral,
) bool {
	if task == nil || taskArchived(task) || task.State == v1.TaskStateCancelled {
		return false
	}
	record, _ := task.Metadata[models.MetaKeyDeferredLaunch].(map[string]interface{})
	if claim, held := models.ReadCeilingLaunchClaimDetails(record); held && !claim.Expired(time.Now().UTC()) {
		// A dispatcher owns this exact record and is dispatching it now. It
		// takes the next free slot itself, so it must not also reserve one
		// against every other automatic and queued launch until its lease ends.
		return false
	}
	if s.deferredRetrySchedule.failureRecorded(task.ID, ceilingDeferralIdentityKey(deferral)) {
		// A record whose replay failed for a non-capacity reason no longer
		// holds the head of the queue: it yields the free slot to later
		// launches until it actually starts or is replaced, so a broken head
		// cannot block the queue while it never starts itself.
		return false
	}
	if s.deferredRetrySchedule.progressStalled(task.ID, ceilingDeferralIdentityKey(deferral)) {
		// A head the sweep has not started a replay for within the stall
		// interval is not being retried — the sweeper is wedged before it.
		// It yields precedence like a failed head, so later automatic
		// launches can take a free slot while the sweeper recovers.
		return false
	}
	if deferral.Kind == models.CeilingLaunchStart {
		if _, _, drop := s.evaluateCeilingStartDropReasons(ctx, task, deferral.Payload); drop {
			return false
		}
	}
	disposition, _, err := s.validateCeilingEntryWithDestinationState(ctx, task, deferral, true)
	return err == nil && disposition == ceilingEntryValid
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
