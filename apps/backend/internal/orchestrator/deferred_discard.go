package orchestrator

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository"
)

// DiscardDeferredCeilingLaunch removes a pending ceiling-deferred launch at an
// explicit user's request. It is the only user-facing entry point into the
// ceiling record, and it never dispatches anything: no session is started or
// stopped and no manual launch is performed.
//
// The three outcomes are distinct by construction (see
// models.DeferredLaunchDiscardOutcome). A failed settlement or clear retains
// the durable record and its owner, so the queued launch stays exactly as
// recoverable as it was before the request, and the caller is told that rather
// than being handed a success for work that did not happen.
//
// Only the ceiling half of the shared deferred_launch record is removed, and
// only for the record this call observed. A different owner's keys (a
// coexisting WIP or dependency intent) and a successor generation survive. A
// live dispatch claim is left alone: another dispatcher may be launching right
// now, and clearing under it would be clearing its key.
func (s *Service) DiscardDeferredCeilingLaunch(
	ctx context.Context, taskID string,
) (models.DeferredLaunchDiscardResult, error) {
	if s == nil || s.repo == nil {
		return models.DeferredLaunchDiscardResult{}, fmt.Errorf("deferred launch discard is unavailable")
	}
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		return models.DeferredLaunchDiscardResult{}, err
	}
	if task == nil {
		return models.DeferredLaunchDiscardResult{}, fmt.Errorf("task %s: %w", taskID, repository.ErrTaskNotFound)
	}
	record, _, err := s.repo.GetTaskDeferredLaunch(ctx, taskID)
	if err != nil {
		return models.DeferredLaunchDiscardResult{}, err
	}
	if flag, _ := record[models.CeilingDeferredKey].(bool); !flag {
		return models.DeferredLaunchDiscardResult{Outcome: models.DeferredLaunchDiscardAlreadyClear}, nil
	}
	deferral, err := models.ReadCeilingDeferral(record)
	if err != nil {
		// The record claims the ceiling half, so it is not "already clear"; its
		// identity cannot be compared, so it is not safe to remove either.
		s.logger.Zap().Warn("could not decode the ceiling deferred record for an explicit discard",
			zap.String("task_id", taskID), zap.Error(err))
		return models.DeferredLaunchDiscardResult{
			Outcome: models.DeferredLaunchDiscardRetained,
			Reason:  models.DeferredLaunchDiscardReasonRecordUnreadable,
		}, nil
	}
	// A live claim is advisory here and authoritative at the guarded clear; this
	// only avoids settling an automation run for a launch another dispatcher is
	// already executing.
	if s.ceilingDeferralClaimed(record) {
		return models.DeferredLaunchDiscardResult{
			Outcome: models.DeferredLaunchDiscardRetained,
			Reason:  models.DeferredLaunchDiscardReasonClaimInFlight,
		}, nil
	}
	outcome, reason := s.discardCeilingDeferral(
		ctx, task, deferral, "the queued launch was discarded by the user", nil,
	)
	return models.DeferredLaunchDiscardResult{Outcome: outcome, Reason: reason}, nil
}

// discardCeilingDeferral is the one settlement path for a ceiling-deferred
// launch that will never run. The automatic replay drop and the explicit user
// discard both use it, so the two cannot drift apart on ordering or ownership.
//
// The order is load-bearing. The automation run the launch serves is settled
// first; the durable record is cleared only after that succeeds, under the
// observed record's identity CAS. A failed settlement or clear returns a
// retained outcome and removes nothing, leaving the record and its owner
// recoverable. onSettled runs between settlement and clear so a caller can
// record the disposition without acting on a launch that is still owned.
func (s *Service) discardCeilingDeferral(
	ctx context.Context, task *models.Task, deferral models.CeilingDeferral,
	settlementReason string, onSettled func(),
) (models.DeferredLaunchDiscardOutcome, models.DeferredLaunchDiscardReason) {
	if task == nil || task.ID == "" {
		return models.DeferredLaunchDiscardRetained, models.DeferredLaunchDiscardReasonClear
	}
	if !s.failQueuedAutomationRun(ctx, deferral.Payload, settlementReason) {
		return models.DeferredLaunchDiscardRetained, models.DeferredLaunchDiscardReasonSettlement
	}
	if onSettled != nil {
		onSettled()
	}
	switch s.clearCeilingDeferredRecordOutcome(ctx, task.ID, &deferral, true) {
	case ceilingClearStored:
		if run := automationRunFromCeilingPayload(deferral.Payload); run != nil {
			s.releaseDroppedAutomationStartOwnership(ctx, task, run.RunID)
		}
		s.publishTaskUpdatedByID(ctx, task.ID)
		return models.DeferredLaunchDiscardDiscarded, ""
	case ceilingClearClaimed:
		return models.DeferredLaunchDiscardRetained, models.DeferredLaunchDiscardReasonClaimInFlight
	default:
		return models.DeferredLaunchDiscardRetained, models.DeferredLaunchDiscardReasonClear
	}
}

// ceilingDeferralClaimed reports whether the record carries a dispatch claim
// that has not expired. A record written before leases existed has no claim and
// is therefore not claimed.
func (s *Service) ceilingDeferralClaimed(record map[string]interface{}) bool {
	claim, ok := models.ReadCeilingLaunchClaimDetails(record)
	return ok && !claim.Expired(time.Now().UTC())
}

// ceilingClaimOwnedByContext reports whether the live claim on the record is
// the one this call already holds, so a dispatcher settling its own claim is
// never treated as a second owner.
func ceilingClaimOwnedByContext(ctx context.Context, taskID string, record map[string]interface{}) bool {
	own, ok := ctx.Value(ceilingDispatchClaimContextKey{}).(*ceilingDeferredLaunchClaim)
	if !ok || own == nil || own.taskID != taskID {
		return false
	}
	claim, held := models.ReadCeilingLaunchClaimDetails(record)
	return held && claim.ID == own.id
}
