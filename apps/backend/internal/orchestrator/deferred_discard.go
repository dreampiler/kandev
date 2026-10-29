package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/automation"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository"
)

// ceilingClaimOwnerDiscard labels the short-lived claim an explicit discard
// takes while it settles. It reuses the same claim key and lease the replay and
// Send Now use, so neither can start dispatching the launch while the user
// decision is being applied.
const ceilingClaimOwnerDiscard = "user_discard"

// automationStartOwnershipRelease reports what happened to the automation start
// ownership a discarded launch carries.
type automationStartOwnershipRelease int

const (
	// automationStartOwnershipNotOurs means the task holds no marker for this
	// run, or the marker belongs to a run the existing reconciliation contract
	// keeps. Nothing is removed for that case.
	automationStartOwnershipNotOurs automationStartOwnershipRelease = iota
	automationStartOwnershipReleased
	// automationStartOwnershipFailed means this request should have released the
	// marker and could not prove that it did.
	automationStartOwnershipFailed
)

// DiscardDeferredCeilingLaunch removes a pending ceiling-deferred launch at an
// explicit user's request. It is the only user-facing entry point into the
// ceiling record, and it never dispatches anything: no session is started or
// stopped and no manual launch is performed.
//
// The three outcomes are distinct by construction (see
// models.DeferredLaunchDiscardOutcome). A failed settlement, ownership release,
// or clear retains the durable record and its ownership, so the queued launch
// stays exactly as recoverable as it was before the request, and the caller is
// told that rather than being handed a success for work that did not happen.
//
// Only the ceiling half of the shared deferred_launch record is removed, and
// only for the record this call observed. A different owner's keys (a
// coexisting WIP or dependency intent) and a successor generation survive.
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
	// Take the existing dispatch claim for this exact record before any
	// settlement side effect runs. Binding the claim to the observed identity
	// and taking it under the entry-admission lock is what makes the later
	// clear safe: no replay or Send Now can start dispatching in between, and a
	// successor record that replaced this one is never settled under the old
	// decision.
	claim, matches, err := s.claimCeilingDeferralForDiscard(ctx, taskID, deferral)
	if err != nil {
		return models.DeferredLaunchDiscardResult{}, err
	}
	if !matches {
		return s.classifyUnclaimedDiscard(ctx, taskID)
	}
	if claim == nil {
		return models.DeferredLaunchDiscardResult{
			Outcome: models.DeferredLaunchDiscardRetained,
			Reason:  models.DeferredLaunchDiscardReasonClaimInFlight,
		}, nil
	}
	defer claim.releaseIfHeld(ctx)
	outcome, reason := s.discardCeilingDeferral(
		withCeilingDispatchClaim(ctx, claim), task, claim.deferral,
		"the queued launch was discarded by the user", nil,
	)
	return models.DeferredLaunchDiscardResult{Outcome: outcome, Reason: reason}, nil
}

// claimCeilingDeferralForDiscard takes the dispatch claim for the exact record
// the caller observed, using the replay/Send Now claim key and lease. The first
// return value is nil with matches true when another dispatcher already owns
// the record; matches is false when the record changed or disappeared first, in
// which case nothing was claimed and nothing may be settled.
func (s *Service) claimCeilingDeferralForDiscard(
	ctx context.Context, taskID string, expected models.CeilingDeferral,
) (*ceilingDeferredLaunchClaim, bool, error) {
	if s == nil || s.repo == nil || taskID == "" {
		return nil, false, fmt.Errorf("deferred launch discard is unavailable")
	}
	ctx, release := s.lockCeilingEntryAdmission(ctx, taskID)
	defer release()
	for attempt := 0; attempt < deferredLaunchCASRetryBudget; attempt++ {
		record, prior, err := s.repo.GetTaskDeferredLaunch(ctx, taskID)
		if err != nil {
			return nil, false, fmt.Errorf("read ceiling launch claim: %w", err)
		}
		current, err := models.ReadCeilingDeferral(record)
		if err != nil {
			return nil, false, nil
		}
		equivalent, err := sameCeilingDeferralIdentity(current, expected)
		if err != nil {
			return nil, false, err
		}
		if !equivalent {
			return nil, false, nil
		}
		if existing, claimed := models.ReadCeilingLaunchClaimDetails(record); claimed && !existing.Expired(time.Now().UTC()) {
			return nil, true, nil
		}
		updated := cloneCeilingRecord(record)
		claimID := uuid.NewString()
		updated[models.CeilingLaunchClaimKey] = map[string]interface{}{
			"id":                                  claimID,
			"owner":                               ceilingClaimOwnerDiscard,
			models.CeilingLaunchClaimExpiresAtKey: time.Now().UTC().Add(ceilingClaimLeaseDuration).Format(time.RFC3339Nano),
		}
		stored, lostCompare, err := s.repo.SetTaskDeferredLaunchIfUnchanged(ctx, taskID, prior, updated)
		if err != nil {
			return nil, false, fmt.Errorf("claim ceiling launch: %w", err)
		}
		if stored {
			return &ceilingDeferredLaunchClaim{
				svc: s, taskID: taskID, deferral: current,
				id: claimID, owner: ceilingClaimOwnerDiscard, held: true,
			}, true, nil
		}
		if !lostCompare {
			return nil, false, fmt.Errorf("claim ceiling launch: repository reported no write")
		}
	}
	return nil, false, fmt.Errorf("claim ceiling launch: compare-and-set retries exhausted")
}

// classifyUnclaimedDiscard answers a discard whose exact record was replaced or
// consumed before it could be claimed. Nothing was settled, so the answer must
// not claim otherwise.
func (s *Service) classifyUnclaimedDiscard(
	ctx context.Context, taskID string,
) (models.DeferredLaunchDiscardResult, error) {
	record, _, err := s.repo.GetTaskDeferredLaunch(ctx, taskID)
	if err != nil {
		return models.DeferredLaunchDiscardResult{}, err
	}
	if flag, _ := record[models.CeilingDeferredKey].(bool); !flag {
		return models.DeferredLaunchDiscardResult{Outcome: models.DeferredLaunchDiscardAlreadyClear}, nil
	}
	return models.DeferredLaunchDiscardResult{
		Outcome: models.DeferredLaunchDiscardRetained,
		Reason:  models.DeferredLaunchDiscardReasonRecordSuperseded,
	}, nil
}

// discardCeilingDeferral is the one settlement path for a ceiling-deferred
// launch that will never run. The automatic replay drop and the explicit user
// discard both use it, so the two cannot drift apart on ordering or ownership.
//
// The order is load-bearing. The automation run the launch serves is settled
// first, then the automation start ownership this launch holds is released, and
// only then is the durable record cleared under the observed identity CAS. A
// failure at any of those steps returns a retained outcome and removes nothing,
// leaving the record and its ownership recoverable. onSettled runs after
// ownership is settled and before the clear, so a caller can record the
// disposition without acting on a launch that is still owned.
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
	if run := automationRunFromCeilingPayload(deferral.Payload); run != nil {
		if s.releaseDiscardedLaunchOwnership(ctx, task, run.RunID) == automationStartOwnershipFailed {
			return models.DeferredLaunchDiscardRetained, models.DeferredLaunchDiscardReasonOwnership
		}
	}
	if onSettled != nil {
		onSettled()
	}
	switch s.clearCeilingDeferredRecordOutcome(ctx, task.ID, &deferral, true) {
	case ceilingClearStored:
		s.publishTaskUpdatedByID(ctx, task.ID)
		return models.DeferredLaunchDiscardDiscarded, ""
	case ceilingClearClaimed:
		return models.DeferredLaunchDiscardRetained, models.DeferredLaunchDiscardReasonClaimInFlight
	default:
		return models.DeferredLaunchDiscardRetained, models.DeferredLaunchDiscardReasonClear
	}
}

// releaseDiscardedLaunchOwnership releases the automation start ownership the
// discarded launch holds, and reports whether it had to act. It reuses the
// existing conditional release, so a marker captured from this task can never
// erase a successor's ownership. A run that is bound or live keeps its marker:
// the existing reconciliation contract owns that case, and a user discard must
// not erase another execution's ownership.
func (s *Service) releaseDiscardedLaunchOwnership(
	ctx context.Context, task *models.Task, runID string,
) automationStartOwnershipRelease {
	binding, ok := s.automationService.(automationRunBinding)
	owner, _ := task.Metadata[models.MetaKeyAutomationStartOwned].(string)
	if !ok || owner == "" || owner != runID {
		return automationStartOwnershipNotOurs
	}
	run, err := binding.GetRun(ctx, runID)
	if err != nil {
		// Without the run row this request cannot tell whether the marker is
		// still its own to release, so it must not report the launch settled.
		s.logger.Zap().Warn("could not read the automation run of a discarded launch",
			zap.String("task_id", task.ID), zap.String("run_id", runID), zap.Error(err))
		return automationStartOwnershipFailed
	}
	if run == nil || run.TaskID != task.ID {
		return automationStartOwnershipNotOurs
	}
	if run.RetryGroupID != "" || run.Status != automation.RunStatusFailed || run.SessionID != "" || run.TurnID != "" {
		s.reconcileAutomationStartOwnership(ctx, task)
		return automationStartOwnershipNotOurs
	}
	sessions, err := s.repo.ListTaskSessions(ctx, task.ID)
	if err != nil {
		s.logger.Zap().Warn("could not list sessions before releasing discarded launch ownership",
			zap.String("task_id", task.ID), zap.String("run_id", runID), zap.Error(err))
		return automationStartOwnershipFailed
	}
	if len(sessions) != 0 {
		return automationStartOwnershipNotOurs
	}
	if !s.releaseAutomationStartOwnership(ctx, task.ID, owner) {
		return automationStartOwnershipFailed
	}
	return automationStartOwnershipReleased
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
