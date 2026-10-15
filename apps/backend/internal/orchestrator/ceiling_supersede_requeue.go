package orchestrator

import (
	"context"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/task/models"
)

// ceilingReasonRetargetedRequeued names the re-queue of a superseded deferred
// launch against the task's current route. It is carried on the record's
// bookkeeping so the card note and the log line distinguish a retargeted wait
// from a fresh refusal.
const ceilingReasonRetargetedRequeued = "ceiling_retargeted_requeued"

// ceilingSupersedeRequeueOutcome is the one-pass result of attempting to
// re-queue a superseded record instead of dropping it.
type ceilingSupersedeRequeueOutcome int

const (
	// ceilingSupersedeRequeueDropped means the record keeps its terminal
	// disposition: a true duplicate, an entry a successor already owns, or a
	// re-queue the compare-and-set loop could not land.
	ceilingSupersedeRequeueDropped ceilingSupersedeRequeueOutcome = iota
	// ceilingSupersedeRequeued means the record was rewritten against the
	// current destination with its original queue time intact.
	ceilingSupersedeRequeued
	// ceilingSupersedeRequeueUnavailable means the current route cannot be
	// read yet: the record is kept unchanged for a later pass.
	ceilingSupersedeRequeueUnavailable
)

// ceilingSupersedeEligibleForRequeue gates the re-queue to workflow-entry
// launches. A generic session launch with no workflow hint keeps its terminal
// drop: with no entry identity there is nothing safe to retarget.
func ceilingSupersedeEligibleForRequeue(deferral models.CeilingDeferral) bool {
	_, bound, err := models.ReadCeilingWorkflowEntryBinding(deferral.Payload)
	if err != nil {
		return false
	}
	if bound {
		return true
	}
	if ceilingPayloadHasWorkflowEntryHint(deferral.Payload) {
		return true
	}
	if deferral.Kind == models.CeilingLaunchStart || deferral.Kind == models.CeilingLaunchStartCreated {
		return sessionIDFromCeilingPayload(deferral) != ""
	}
	return false
}

// requeueCeilingSupersededRetargets rewrites a superseded deferral against the
// task's current route instead of dropping it. The original queued_at is
// preserved so the launch keeps its place in the replay order, and the write
// goes through the same compare-and-set loop deferCeilingRefusal uses: a newer
// successor that won the race stays in place and this pass treats it as a
// duplicate drop. True duplicates (the current destination already
// STARTING/RUNNING for the same entry) still drop so one entry never produces
// two live sessions.
//
//nolint:cyclop // Retarget guards a successor claim, record identity, rebind, and a compare-and-set write in one pass.
func (s *Service) requeueCeilingSupersededRetargets(
	ctx context.Context, task *models.Task, deferral models.CeilingDeferral, detail string,
) (ceilingSupersedeRequeueOutcome, string) {
	if task == nil || task.ID == "" {
		return ceilingSupersedeRequeueDropped, detail
	}
	taskID := task.ID
	admissionCtx, release := s.lockCeilingEntryAdmission(ctx, taskID)
	defer release()

	// A successor dispatch claim (for example Send Now) that owns the record
	// while this replay was in flight owns the disposition: the stale replay
	// must leave the record untouched, exactly like the drop path's claim
	// guard in clearCeilingDeferredRecord.
	raw, _, rawErr := s.repo.GetTaskDeferredLaunch(admissionCtx, taskID)
	if rawErr != nil {
		return ceilingSupersedeRequeueUnavailable, detail
	}
	if ceilingSupersedeSuccessorClaimHeld(ctx, taskID, raw) {
		s.logger.Zap().Info("leaving a successor-owned ceiling deferred record in place after superseded retarget",
			zap.String("task_id", taskID))
		return ceilingSupersedeRequeueUnavailable, "a successor dispatch claim owns the record"
	}
	// A successor record queued while this replay was in flight wins: the
	// stale replay must not retarget the old launch onto it.
	current, readErr := models.ReadCeilingDeferral(raw)
	if readErr != nil {
		return ceilingSupersedeRequeueUnavailable, detail
	}
	if equivalent, compareErr := sameCeilingDeferralIdentity(current, deferral); compareErr != nil || !equivalent {
		s.logger.Zap().Info("leaving a newer ceiling deferred record in place after superseded retarget",
			zap.String("task_id", taskID))
		return ceilingSupersedeRequeueDropped, "a newer queued launch already replaced this record"
	}

	rebound, reboundDeferral, rebindErr := s.rebindCeilingDeferralToCurrentRoute(admissionCtx, task, deferral)
	if rebindErr != nil {
		return ceilingSupersedeRequeueUnavailable, detail
	}
	if !rebound {
		// No current destination to retarget onto (an ambiguous or structural
		// dead end, or the same entry already dispatching). The existing
		// superseded/drop disposition stands.
		return ceilingSupersedeRequeueDropped, detail
	}

	for attempt := 0; attempt < deferredLaunchCASRetryBudget; attempt++ {
		record, prior, err := s.repo.GetTaskDeferredLaunch(admissionCtx, taskID)
		if err != nil {
			return ceilingSupersedeRequeueUnavailable, detail
		}
		stored, err := models.ReadCeilingDeferral(record)
		if err != nil {
			return ceilingSupersedeRequeueUnavailable, detail
		}
		equivalent, err := sameCeilingDeferralIdentity(stored, deferral)
		if err != nil {
			return ceilingSupersedeRequeueUnavailable, detail
		}
		if !equivalent {
			s.logger.Zap().Info("leaving a newer ceiling deferred record in place after superseded retarget",
				zap.String("task_id", taskID))
			return ceilingSupersedeRequeueDropped, "a newer queued launch already replaced this record"
		}
		merged, _, _ := models.MergeCeilingRecord(record, reboundDeferral)
		// The record is no longer owned by this replay and its reason changed,
		// so the AC-49 surface stamps and the in-flight claim must not survive.
		delete(merged, models.CeilingSurfaceWrittenAtKey)
		delete(merged, models.CeilingSurfaceAttemptCountKey)
		delete(merged, models.CeilingLaunchClaimKey)
		written, lostCompare, err := s.repo.SetTaskDeferredLaunchIfUnchanged(admissionCtx, taskID, prior, merged)
		if err != nil {
			return ceilingSupersedeRequeueUnavailable, detail
		}
		if written {
			return ceilingSupersedeRequeued, detail
		}
		if !lostCompare {
			return ceilingSupersedeRequeueUnavailable, "the retargeted launch record could not be written"
		}
	}
	return ceilingSupersedeRequeueDropped, "the retargeted launch record could not be written after repeated compare-and-set losses"
}

// ceilingSupersedeSuccessorClaimHeld reports whether the stored record is
// currently owned by a different dispatch claim than the caller's replay claim
// (for example a Send Now successor that raced this sweep). It mirrors the
// successor guard in clearCeilingDeferredRecord so the re-queue path never
// overwrites a claim its own replay does not hold.
func ceilingSupersedeSuccessorClaimHeld(ctx context.Context, taskID string, raw map[string]interface{}) bool {
	claim, ok := ctx.Value(ceilingDispatchClaimContextKey{}).(*ceilingDeferredLaunchClaim)
	if !ok || claim == nil || claim.taskID != taskID {
		return false
	}
	claimID, _, held := models.ReadCeilingLaunchClaim(raw)
	if !held || claimID != claim.id {
		return true
	}
	return false
}

// rebindCeilingDeferralToCurrentRoute derives the binding for the task's
// current route and returns a copy of the deferral retargeted onto it, keeping
// the original queue time. It reports false when the record must keep its
// terminal disposition: the entry itself moved on (a newer entry owns the
// route), the binding cannot be derived, or the current destination is already
// dispatching the same entry (a true duplicate).
func (s *Service) rebindCeilingDeferralToCurrentRoute(
	ctx context.Context, task *models.Task, deferral models.CeilingDeferral,
) (bool, models.CeilingDeferral, error) {
	oldBinding, bound, err := models.ReadCeilingWorkflowEntryBinding(deferral.Payload)
	if err != nil {
		return false, models.CeilingDeferral{}, err
	}
	if !bound {
		// A legacy sessionless start has no recipient identity to compare with
		// the current route; retargeting it would answer an old workflow entry
		// with whichever destination happens to be current.
		if deferral.Kind == models.CeilingLaunchStart {
			return false, models.CeilingDeferral{}, nil
		}
		return s.rebindUnboundCeilingDeferral(ctx, task, deferral)
	}
	route, routePresent := models.LoadWorkflowSessionRoute(task.Metadata)
	if !routePresent || route.EntryIdentity == "" {
		return false, models.CeilingDeferral{}, nil
	}
	if !s.ceilingSupersedeKeepsEntry(ctx, task, oldBinding, route) {
		return false, models.CeilingDeferral{}, nil
	}
	fresh := oldBinding
	fresh.RouteOperationID = route.OperationID
	fresh.DestinationStepID = route.DestinationStepID
	fresh.EntryIdentity = route.EntryIdentity
	if route.DestinationID != "" {
		fresh.DestinationSessionID = route.DestinationID
	}
	if !fresh.Valid() {
		return false, models.CeilingDeferral{}, nil
	}
	return s.retargetCeilingDeferralToBinding(ctx, task, deferral, fresh)
}

// rebindUnboundCeilingDeferral retargets a legacy record that carries no
// nested binding but names a session explicitly. The session identity stays
// the guard: only a destination change for the same entry re-queues, and the
// fresh binding is derived from the current route.
//
//nolint:cyclop // Retarget resolves independent session, route, entry, step, and workflow invariants.
func (s *Service) rebindUnboundCeilingDeferral(
	ctx context.Context, task *models.Task, deferral models.CeilingDeferral,
) (bool, models.CeilingDeferral, error) {
	payloadSessionID := sessionIDFromCeilingPayload(deferral)
	if payloadSessionID == "" {
		return false, models.CeilingDeferral{}, nil
	}
	route, routePresent := models.LoadWorkflowSessionRoute(task.Metadata)
	if !routePresent || route.EntryIdentity == "" {
		return false, models.CeilingDeferral{}, nil
	}
	if route.DestinationID == "" || route.DestinationID == payloadSessionID {
		return false, models.CeilingDeferral{}, nil
	}
	currentIdentity, identityErr := s.readWorkflowEntryIdentity(ctx, task.ID)
	if identityErr != nil {
		return false, models.CeilingDeferral{}, identityErr
	}
	if currentIdentity == legacyWorkflowEntryIdentity {
		return false, models.CeilingDeferral{}, nil
	}
	stepID := stringField(deferral.Payload, metaKeyWorkflowStepID)
	if stepID == "" {
		stepID = task.WorkflowStepID
	}
	if stepID == "" || route.DestinationStepID != stepID {
		return false, models.CeilingDeferral{}, nil
	}
	workflowID := task.WorkflowID
	if s.workflowStepGetter != nil {
		if step, stepErr := s.workflowStepGetter.GetStep(ctx, stepID); stepErr != nil {
			return false, models.CeilingDeferral{}, stepErr
		} else if step != nil && step.WorkflowID != "" {
			workflowID = step.WorkflowID
		}
	}
	if workflowID == "" {
		return false, models.CeilingDeferral{}, nil
	}
	fresh := models.CeilingWorkflowEntryBinding{
		WorkflowID:           workflowID,
		DestinationStepID:    stepID,
		RouteOperationID:     route.OperationID,
		EntryIdentity:        currentIdentity,
		DestinationSessionID: route.DestinationID,
	}
	return s.retargetCeilingDeferralToBinding(ctx, task, deferral, fresh)
}

// ceilingSupersedeKeepsEntry reports whether the superseded record still
// belongs to the task's current entry. Step, workflow, or entry-identity
// changes mean a newer entry owns the task: the old launch must not be
// retargeted onto it.
func (s *Service) ceilingSupersedeKeepsEntry(
	ctx context.Context, task *models.Task, oldBinding models.CeilingWorkflowEntryBinding, route models.WorkflowSessionRoute,
) bool {
	if task == nil {
		return false
	}
	if task.WorkflowID != "" && task.WorkflowID != oldBinding.WorkflowID {
		return false
	}
	if route.DestinationStepID != oldBinding.DestinationStepID ||
		route.EntryIdentity != oldBinding.EntryIdentity {
		return false
	}
	currentIdentity, identityErr := s.readWorkflowEntryIdentity(ctx, task.ID)
	if identityErr != nil || currentIdentity == legacyWorkflowEntryIdentity {
		return identityErr == nil
	}
	return currentIdentity == oldBinding.EntryIdentity
}

// retargetCeilingDeferralToBinding copies the deferral onto the fresh binding,
// preserving the original queue time. A current destination that is already
// STARTING or RUNNING for the same entry is a true duplicate: it reports false
// so the record drops instead of producing a second live session. A terminal
// destination resolves to the route's newer destination where one exists;
// otherwise the old launch has nowhere to go and keeps its drop.
//
//nolint:cyclop // Destination retarget checks ownership, duplicate dispatch, and terminal replacement independently.
func (s *Service) retargetCeilingDeferralToBinding(
	ctx context.Context, task *models.Task, deferral models.CeilingDeferral, fresh models.CeilingWorkflowEntryBinding,
) (bool, models.CeilingDeferral, error) {
	destinationID := fresh.DestinationSessionID
	if destinationID == "" {
		destinationID = sessionIDFromCeilingPayload(deferral)
		route, routePresent := models.LoadWorkflowSessionRoute(task.Metadata)
		if routePresent && route.DestinationID != "" {
			destinationID = route.DestinationID
		}
	}
	if destinationID == "" {
		return false, models.CeilingDeferral{}, nil
	}
	session, err := s.repo.GetTaskSession(ctx, destinationID)
	if err != nil {
		if session != nil {
			return false, models.CeilingDeferral{}, nil
		}
		return false, models.CeilingDeferral{}, err
	}
	if session == nil {
		// The route names a destination row that no longer exists: this
		// launch's recipient is gone, so dropping stays terminal.
		return false, models.CeilingDeferral{}, nil
	}
	if session.TaskID != task.ID {
		return false, models.CeilingDeferral{}, nil
	}
	switch {
	case session.State == models.TaskSessionStateStarting || session.State == models.TaskSessionStateRunning:
		// The same entry is already dispatching: replaying again would
		// duplicate the workflow prompt or continuation.
		return false, models.CeilingDeferral{}, nil
	case isTerminalSessionState(session.State) && !deferredIdleFocusResumeOfCompleted(deferral, session.State):
		route, routePresent := models.LoadWorkflowSessionRoute(task.Metadata)
		if !routePresent || route.DestinationID == "" || route.DestinationID == session.ID {
			return false, models.CeilingDeferral{}, nil
		}
		replacement, replacementErr := s.repo.GetTaskSession(ctx, route.DestinationID)
		if replacementErr != nil || replacement == nil || replacement.TaskID != task.ID {
			if replacementErr != nil {
				return false, models.CeilingDeferral{}, replacementErr
			}
			return false, models.CeilingDeferral{}, nil
		}
		if replacement.State == models.TaskSessionStateStarting || replacement.State == models.TaskSessionStateRunning {
			return false, models.CeilingDeferral{}, nil
		}
		if isTerminalSessionState(replacement.State) && !deferredIdleFocusResumeOfCompleted(deferral, replacement.State) {
			return false, models.CeilingDeferral{}, nil
		}
		destinationID = replacement.ID
	}
	retargeted := deferral
	retargeted.Payload = cloneCeilingPayload(deferral.Payload)
	fresh.DestinationSessionID = destinationID
	retargeted.Payload[models.CeilingLaunchEntryBindingKey] = ceilingEntryBindingValue(fresh)
	if deferral.Kind != models.CeilingLaunchStart {
		retargeted.Payload[metaKeySessionID] = destinationID
	}
	retargeted.ReasonCode = ceilingReasonRetargetedRequeued
	return true, retargeted, nil
}

// logCeilingSupersedeRequeued records a superseded launch that was re-queued
// against the current destination, with the same identifying fields as the
// drop line so the two dispositions compare directly.
func (s *Service) logCeilingSupersedeRequeued(
	ctx context.Context, task *models.Task, deferral models.CeilingDeferral, detail string,
) {
	s.logger.Zap().Info("re-queued a ceiling-deferred launch after its workflow destination changed",
		zap.String("task_id", task.ID),
		zap.String("kind", string(deferral.Kind)),
		zap.String(ceilingFieldReasonCode, ceilingReasonSuperseded),
		zap.String("detail", detail),
		zap.String("destination_session_id", sessionIDFromCeilingPayload(deferral)),
		zap.Bool("periodic", isPeriodicCeilingSweep(ctx)))
}

// settleCeilingSupersedeRequeue applies a superseded replay's re-queue
// outcome: a retargeted record is rewritten in place (same CAS guarantees as
// the original deferral, so a successor that won the race stays), a duplicate
// keeps its terminal drop, and an unreadable route keeps the record unchanged
// for a later pass. It returns true when the supersede was fully handled here
// and the caller must not drop.
func (s *Service) settleCeilingSupersedeRequeue(
	ctx context.Context, task *models.Task, claim *ceilingDeferredLaunchClaim,
	deferral models.CeilingDeferral, detail string,
) bool {
	outcome, outcomeDetail := s.requeueCeilingSupersededRetargets(ctx, task, deferral, detail)
	switch outcome {
	case ceilingSupersedeRequeued:
		s.deferredRetrySchedule.settle(task.ID)
		if claim != nil {
			claim.releaseIfHeld(ctx)
		}
		s.attemptCeilingSurfaceWrite(ctx, task.ID)
		s.reconcileQueuedTaskState(ctx, task.ID)
		s.logCeilingSupersedeRequeued(ctx, task, deferral, outcomeDetail)
		s.publishTaskUpdatedByID(ctx, task.ID)
		return true
	case ceilingSupersedeRequeueUnavailable:
		if claim != nil {
			claim.releaseIfHeld(ctx)
		}
		s.logCeilingReplayPause(ctx, task.ID, deferral, "entry_unavailable", outcomeDetail)
		return true
	default:
		return false
	}
}

// evaluateCeilingSupersedeRequeue decides, for a pre-claim supersede found by
// evaluateCeilingDropReasons, whether to re-queue against the current route.
// It returns handled=true when the re-queue path owned the disposition
// (re-queued or kept for later); handled=false means the caller keeps the
// terminal drop.
func (s *Service) evaluateCeilingSupersedeRequeue(
	ctx context.Context, task *models.Task, deferral models.CeilingDeferral, detail string,
) bool {
	if task == nil || task.ID == "" {
		return false
	}
	if !ceilingSupersedeEligibleForRequeue(deferral) {
		return false
	}
	outcome, outcomeDetail := s.requeueCeilingSupersededRetargets(ctx, task, deferral, detail)
	switch outcome {
	case ceilingSupersedeRequeued:
		s.deferredRetrySchedule.settle(task.ID)
		s.attemptCeilingSurfaceWrite(ctx, task.ID)
		s.reconcileQueuedTaskState(ctx, task.ID)
		s.logCeilingSupersedeRequeued(ctx, task, deferral, outcomeDetail)
		s.publishTaskUpdatedByID(ctx, task.ID)
		return true
	case ceilingSupersedeRequeueUnavailable:
		s.logCeilingReplayPause(ctx, task.ID, deferral, "entry_unavailable", outcomeDetail)
		return true
	default:
		return false
	}
}
