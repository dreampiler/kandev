package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/orchestrator/watcher"
	"github.com/kandev/kandev/internal/task/models"
	workflowmove "github.com/kandev/kandev/internal/workflow/move"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// ceilingDeferredTaskLister is the narrow repository capability AC-50b's
// sweep needs. Reached through a type assertion on s.repo, following the
// admittedSessionLister idiom in session_ceiling_config.go, rather than
// growing sessionExecutorStore for a capability only the sweep uses.
type ceilingDeferredTaskLister interface {
	ListTasksWithCeilingDeferred(ctx context.Context) ([]*models.Task, error)
}

// ceilingReplayOutcome is the three-way disposition every per-kind replay
// function reduces to, however differently each of the 7 entry points
// reports it.
type ceilingReplayOutcome int

const (
	// ceilingReplayStillDeferred means the launch asked for admission again
	// and was refused again: the record is left exactly as stored (AC-32),
	// including its original queued_at, for a later pass.
	ceilingReplayStillDeferred ceilingReplayOutcome = iota
	// ceilingReplaySucceeded means the launch was admitted and dispatched:
	// the record is cleared.
	ceilingReplaySucceeded
	// ceilingReplayFailed means the launch failed for a reason unrelated to
	// the ceiling (AC-15d): the record is retained for a later pass and the
	// failure is logged, since — unlike a refusal — nothing else observes it.
	ceilingReplayFailed
	// ceilingReplaySuperseded is a terminal disposition for a claimed launch
	// whose workflow entry or destination changed before dispatch.
	ceilingReplaySuperseded
	// ceilingReplayRunClosed is a terminal disposition for a claimed start
	// whose automation run closed before dispatch: nothing was launched.
	ceilingReplayRunClosed
)

// drainDeferredCeilingLaunches is AC-15c's pass driver: one sweep tick walks
// every ceiling-deferred task once, in priority/position/queue-time/ID order,
// retrying each in turn. A candidate that fails for a non-ceiling reason is
// skipped for the rest of this pass (AC-15d) rather than retried immediately or
// stopping the pass.
//
// A periodic pass is a backstop, not a driver: it dispatches only for a lane
// that actually has free capacity right now (read once for the whole pass).
// While a lane is saturated, re-running admission for records waiting in it
// cannot change the outcome, so the pass leaves them untouched. A release, an
// applied capacity change, or a failed launch signals a pass immediately, and a
// direct caller that is not the sweeper (Send Now, startup recovery) always
// dispatches.
func (s *Service) drainDeferredCeilingLaunches(ctx context.Context) {
	lister, ok := s.repo.(ceilingDeferredTaskLister)
	if !ok {
		return
	}
	tasks, err := lister.ListTasksWithCeilingDeferred(ctx)
	if err != nil {
		s.logger.Zap().Warn("could not list ceiling-deferred tasks for the retry sweep", zap.Error(err))
		return
	}
	sort.SliceStable(tasks, func(i, j int) bool { return ceilingReplayLess(tasks[i], tasks[j]) })
	paced := isPeriodicCeilingSweep(ctx)
	allowDispatch := func(models.CeilingDeferral) bool { return true }
	if paced {
		deferred := make(map[string]struct{}, len(tasks))
		for _, task := range tasks {
			if task != nil {
				deferred[task.ID] = struct{}{}
			}
		}
		s.deferredRetrySchedule.prune(deferred)
		workerFree, controlFree, freeErr := s.sessionCeiling.freeCapacityByLane(ctx)
		if freeErr != nil {
			// Without a capacity reading there is nothing to re-decide yet;
			// admission itself fails closed on an unknown population, so a
			// retry here would only repeat the same refusal. The next pass,
			// release, or capacity change retries.
			s.logger.Zap().Warn("ceiling backstop could not read free capacity; deferring retries to the next pass",
				zap.Error(freeErr))
			return
		}
		allowDispatch = func(deferral models.CeilingDeferral) bool {
			if s.sessionCeiling.classOf(s.ceilingDeferredProfile(ctx, deferral)) == ceilingClassControl {
				return controlFree
			}
			return workerFree
		}
	}
	for _, task := range tasks {
		s.retryOneDeferredCeilingLaunch(ctx, task, allowDispatch)
	}
}

// reconcileDeferredCeilingTaskState repairs the state written by older
// versions that left a valid, idle queued destination in REVIEW. It only moves
// REVIEW to SCHEDULING after authoritative queue and session checks; a read
// failure leaves the existing state untouched.
//
//nolint:cyclop // State repair validates queue ownership, route identity, and a final compare-and-set.
func (s *Service) reconcileDeferredCeilingTaskState(
	ctx context.Context, task *models.Task, deferral models.CeilingDeferral,
) {
	if task == nil || task.State != v1.TaskStateReview {
		return
	}
	ctx, release := s.lockCeilingEntryAdmission(ctx, task.ID)
	defer release()
	observedDeferral, valid, err := s.readValidCeilingDeferredLaunch(ctx, task)
	if err != nil || !valid {
		return
	}
	if equivalent, compareErr := sameCeilingDeferralIdentity(observedDeferral, deferral); compareErr != nil || !equivalent {
		return
	}
	if blockingSessionID, ok := s.otherWorkingSessionID(ctx, task.ID, ""); !ok || blockingSessionID != "" {
		return
	}
	// The queue record and task route are independently mutable. Re-read both
	// immediately before the state CAS so an older sweep snapshot cannot repair
	// REVIEW after a successor launch replaced the record.
	latestTask, taskErr := s.repo.GetTask(ctx, task.ID)
	if taskErr != nil || latestTask == nil || latestTask.State != v1.TaskStateReview ||
		latestTask.IsFromOffice || taskArchived(latestTask) {
		return
	}
	current, currentValid, readErr := s.readValidCeilingDeferredLaunch(ctx, latestTask)
	if readErr != nil || !currentValid {
		return
	}
	equivalent, compareErr := sameCeilingDeferralIdentity(current, observedDeferral)
	if compareErr != nil || !equivalent {
		return
	}
	updated, err := s.taskRepo.UpdateTaskStateIfCurrentIn(
		ctx, task.ID, v1.TaskStateScheduling, []v1.TaskState{v1.TaskStateReview},
	)
	if err != nil {
		s.logger.Zap().Warn("could not repair queued task state",
			zap.String("task_id", task.ID), zap.Error(err))
		return
	}
	if updated {
		s.logger.Zap().Info("repaired queued task state from REVIEW to SCHEDULING",
			zap.String("task_id", task.ID), zap.String("kind", string(deferral.Kind)))
		task.State = v1.TaskStateScheduling
	}
}

// retryOneDeferredCeilingLaunch reads one task's deferral, evaluates AC-17b's
// drop reasons, and otherwise dispatches to the kind's own replay. allowDispatch
// gates only the admission/dispatch half: the reload, record read, binding
// enrichment, state repair, and drop evaluation always run, so the periodic
// backstop keeps detecting eligibility changes. A nil gate dispatches.
func (s *Service) retryOneDeferredCeilingLaunch(
	ctx context.Context, task *models.Task, allowDispatch func(models.CeilingDeferral) bool,
) {
	task, deferral, ok := s.loadDeferredCeilingRetry(ctx, task)
	if !ok {
		return
	}
	if reasonCode, detail, drop := s.evaluateCeilingDropReasons(ctx, task, deferral); drop {
		s.deferredRetrySchedule.settle(task.ID)
		s.dropCeilingDeferral(ctx, task, sessionIDFromCeilingPayload(deferral), deferral, reasonCode, detail)
		return
	}
	if !s.deferredCeilingRetryAllowed(ctx, task.ID, deferral, allowDispatch) {
		return
	}
	claim, found, claimErr := s.claimCeilingDeferredLaunch(
		ctx, task.ID, sessionIDFromCeilingPayload(deferral), ceilingClaimOwnerReplay,
	)
	if claimErr != nil {
		s.logger.Zap().Warn("could not claim deferred launch for retry sweep",
			zap.String("task_id", task.ID), zap.Error(claimErr))
		return
	}
	if !found || claim == nil {
		s.logDeferredCeilingClaimMiss(ctx, task.ID, deferral, found)
		return
	}
	deferral = claim.deferral
	ctx = withCeilingDispatchClaim(ctx, claim)

	defer claim.releaseIfHeld(ctx)
	currentTask := s.validateClaimedCeilingReplay(ctx, task.ID, claim)
	if currentTask == nil {
		return
	}

	s.settleCeilingReplay(ctx, task, claim, deferral, s.replayCeilingDeferral(ctx, currentTask, deferral))
}

// loadDeferredCeilingRetry reloads the task and its deferral, binds the retry
// schedule to the record actually read, enriches the workflow-entry binding,
// and repairs a legacy queued projection. It leaves the record for the caller
// to drop or dispatch, and reports false when the record is unavailable,
// unreplayable, or was superseded while its binding was enriched.
func (s *Service) loadDeferredCeilingRetry(
	ctx context.Context, task *models.Task,
) (*models.Task, models.CeilingDeferral, bool) {
	if task == nil || task.ID == "" {
		return nil, models.CeilingDeferral{}, false
	}
	// The lister snapshot is only an index. Reload both task and record before
	// admission so a later workflow entry cannot inherit the old destination.
	freshTask, err := s.repo.GetTask(ctx, task.ID)
	if err != nil || freshTask == nil {
		s.logCeilingReplayPause(ctx, task.ID, models.CeilingDeferral{}, "task_unavailable", "task could not be reloaded")
		return nil, models.CeilingDeferral{}, false
	}
	task = freshTask
	raw, _, err := s.repo.GetTaskDeferredLaunch(ctx, task.ID)
	if err != nil {
		s.logger.Zap().Warn("could not read deferred launch for retry sweep",
			zap.String("task_id", task.ID), zap.Error(err))
		return nil, models.CeilingDeferral{}, false
	}
	deferral, err := models.ReadCeilingDeferral(raw)
	if err != nil {
		var unreplayable *models.UnreplayableCeilingRecordError
		if errors.As(err, &unreplayable) {
			s.dropCeilingDeferral(ctx, task, "", deferral, ceilingReasonDroppedUnreplayableRecord,
				fmt.Sprintf("the deferred launch record could not be replayed: %v", err))
		}
		// Any other read failure (most likely: a concurrent writer already
		// cleared the record between the list and this read) is not this
		// task's fault; leave it for the next tick to see the current state.
		s.logCeilingReplayPause(ctx, task.ID, deferral, "record_unavailable", "record no longer carries a replayable launch")
		return nil, models.CeilingDeferral{}, false
	}
	// Re-bind the retry schedule to the record this pass actually read, so a
	// record replaced while it waited starts fresh instead of inheriting the
	// previous launch's failure wait.
	s.deferredRetrySchedule.observe(task.ID, ceilingDeferralIdentityKey(deferral))

	deferral, err = s.enrichCeilingDeferralBinding(ctx, task, deferral)
	if err != nil {
		if errors.Is(err, ErrCeilingLaunchSuperseded) {
			// A successor record won while this legacy record was being enriched.
			// Leave the successor untouched for the next sweep; this pass must not
			// retarget the old launch to it.
			s.logCeilingReplayPause(ctx, task.ID, deferral, "binding_changed", "a successor won binding enrichment")
			return nil, models.CeilingDeferral{}, false
		}
		s.logger.Zap().Warn("could not bind deferred workflow entry for retry sweep",
			zap.String("task_id", task.ID), zap.Error(err))
		return nil, models.CeilingDeferral{}, false
	}

	// A valid queued destination repairs the legacy REVIEW projection before a
	// replay attempt. The repair itself is fail-closed on queue/route reads.
	s.reconcileDeferredCeilingTaskState(ctx, task, deferral)
	return task, deferral, true
}

// deferredCeilingRetryAllowed applies the two gates that stop the periodic
// backstop from re-deciding an unchanged refusal: the record's lane must have
// free capacity, and a record whose last replay failed for a non-capacity
// reason must have finished its short wait. It never signals the sweep, so a
// waiting refusal cannot drive itself.
func (s *Service) deferredCeilingRetryAllowed(
	ctx context.Context, taskID string, deferral models.CeilingDeferral,
	allowDispatch func(models.CeilingDeferral) bool,
) bool {
	if allowDispatch != nil && !allowDispatch(deferral) {
		// No admission input changed in this record's lane: it is still
		// saturated, so re-running admission cannot change the outcome. Leave
		// the record exactly as stored for the pass that follows a release, a
		// capacity change, or the next tick that sees free capacity.
		return false
	}
	if isPeriodicCeilingSweep(ctx) &&
		s.deferredRetrySchedule.failureWaiting(taskID, ceilingDeferralIdentityKey(deferral)) {
		s.logCeilingReplayPause(ctx, taskID, deferral, "failure_backoff", "a non-capacity failure is still backing off")
		return false
	}
	return true
}

// logDeferredCeilingClaimMiss reports a replay that could not acquire its claim
// because no matching record remains, or because another dispatcher owns it.
func (s *Service) logDeferredCeilingClaimMiss(
	ctx context.Context, taskID string, deferral models.CeilingDeferral, found bool,
) {
	reason := "recipient_changed"
	if found {
		reason = "claimed"
	}
	s.logCeilingReplayPause(ctx, taskID, deferral, reason, "deferred launch claim was not acquired")
}

// settleCeilingReplay applies a claimed replay's outcome to its record.
func (s *Service) settleCeilingReplay(
	ctx context.Context, task *models.Task, claim *ceilingDeferredLaunchClaim,
	deferral models.CeilingDeferral, result ceilingReplayResult,
) {
	switch result.outcome {
	case ceilingReplaySucceeded:
		s.deferredRetrySchedule.settle(task.ID)
		claim.settle(ctx)
		s.publishTaskUpdatedByID(ctx, task.ID)
	case ceilingReplaySuperseded:
		s.deferredRetrySchedule.settle(task.ID)
		s.dropCeilingDeferral(ctx, task, sessionIDFromCeilingPayload(deferral), deferral,
			ceilingReasonSuperseded, "workflow destination changed before dispatch")
	case ceilingReplayRunClosed:
		s.deferredRetrySchedule.settle(task.ID)
		s.dropCeilingDeferral(ctx, task, sessionIDFromCeilingPayload(deferral), deferral,
			ceilingReasonDroppedTaskIneligible, ceilingDetailAutomationRunClosed)
	case ceilingReplayFailed:
		s.deferredRetrySchedule.recordFailure(task.ID, ceilingDeferralIdentityKey(deferral))
		s.logger.Zap().Warn("ceiling retry replay failed for a non-ceiling reason; will retry on a later sweep",
			zap.String("task_id", task.ID), zap.String("kind", string(deferral.Kind)),
			zap.String("detail", result.detail), zap.Error(result.err))
	case ceilingReplayStillDeferred:
		// Remove the in-flight claim after the replay gate has restored the
		// durable deferral. The next sweep must be able to own it again.
		claim.releaseIfHeld(ctx)
		// Left exactly as stored: the replay's own admission gate already
		// re-persisted this same record via deferCeilingRefusal's
		// equivalence check (AC-32/AC-55a). AC-49g: retry the card note if
		// an earlier attempt to write it failed.
		s.attemptCeilingSurfaceWrite(ctx, task.ID)
		// Refresh the task projection so the queue keeps the latest capacity
		// observation while its durable recipient and queue time remain stable.
		s.publishTaskUpdatedByID(ctx, task.ID)
	}
}

// sessionIDFromCeilingPayload extracts the session id a deferral's payload
// names, for the six kinds that carry one. CeilingLaunchStart carries none:
// no session exists yet at seam 1.
func sessionIDFromCeilingPayload(deferral models.CeilingDeferral) string {
	if binding, present, err := models.ReadCeilingWorkflowEntryBinding(deferral.Payload); err == nil && present {
		if binding.DestinationSessionID != "" {
			return binding.DestinationSessionID
		}
	}
	if deferral.Kind == models.CeilingLaunchStart {
		return ""
	}
	return stringField(deferral.Payload, metaKeySessionID)
}

// evaluateCeilingDropReasons is AC-17b's closed set of drop reasons. Reasons
// (a)-(c) are universal; (d) and (e) are scoped to the "start" kind only —
// concrete tracing showed only startTask's own gate embeds the terminal-PR
// guard, and none of the other six entry points call shouldAutoStartStep
// internally, so applying either check to an unrelated kind risks dropping a
// legitimate continuation that has nothing to do with step-entry auto-start
// semantics. Reason (f), the unreplayable record, is handled by the caller
// before this is ever reached.
func (s *Service) evaluateCeilingDropReasons(
	ctx context.Context, task *models.Task, deferral models.CeilingDeferral,
) (reasonCode, detail string, drop bool) {
	if task.ArchivedAt != nil {
		return ceilingReasonDroppedTaskIneligible, "task is archived", true
	}
	if task.State == v1.TaskStateCancelled {
		return ceilingReasonDroppedTaskIneligible, "task is cancelled", true
	}
	if sessionID := sessionIDFromCeilingPayload(deferral); sessionID != "" {
		if _, err := s.repo.GetTaskSession(ctx, sessionID); err != nil {
			if errors.Is(err, models.ErrTaskSessionNotFound) {
				return ceilingReasonDroppedTaskIneligible, "session no longer exists", true
			}
			// An unrelated read failure isn't grounds to drop the record;
			// the replay attempt below will hit the same failure and retain it.
			return "", "", false
		}
	}
	if deferral.Kind == models.CeilingLaunchStart {
		if reasonCode, detail, drop := s.evaluateCeilingStartDropReasons(ctx, task, deferral.Payload); drop {
			return reasonCode, detail, true
		}
	}
	if disposition, detail, validationErr := s.validateCeilingEntry(ctx, task, deferral); validationErr != nil {
		// A route or step read error is uncertainty, not proof that the queued
		// entry is obsolete. Preserve the record for a later sweep.
		return "", "", false
	} else if disposition == ceilingEntrySuperseded {
		return ceilingReasonSuperseded, detail, true
	} else if disposition == ceilingEntryUnavailable {
		// Read uncertainty is not a terminal disposition. Keep the durable
		// record for a later pass and leave the current task state untouched.
		return "", "", false
	}
	return "", "", false
}

// evaluateCeilingStartDropReasons applies the drop reasons scoped to the
// "start" kind. An automation run's trigger is its start signal, so a start
// serving a run is not governed by workflow-step auto-start eligibility, as
// Office starts are not; it is dropped instead once its run is no longer open.
func (s *Service) evaluateCeilingStartDropReasons(
	ctx context.Context, task *models.Task, payload map[string]interface{},
) (reasonCode, detail string, drop bool) {
	run := automationRunFromCeilingPayload(payload)
	if run != nil && !s.automationRunAwaitsLaunch(ctx, run.RunID) {
		return ceilingReasonDroppedTaskIneligible, ceilingDetailAutomationRunClosed, true
	}
	if run == nil && !task.IsFromOffice && !s.shouldAutoStartStep(ctx, task.WorkflowStepID) {
		return ceilingReasonDroppedTaskIneligible, "workflow step no longer auto-starts", true
	}
	if s.shouldSkipTerminalPRAutoStart(ctx, task) {
		return ceilingReasonDroppedLaunchGateDeclined, "terminal PR auto-start gate declined the launch", true
	}
	return "", "", false
}

// dropCeilingDeferral terminally disposes of a deferred launch that will never
// be retried. It first fails the automation run the launch serves, if that run
// still waits, and keeps the record when the run cannot be failed, so a later
// sweep drops it again instead of leaving the run open with nothing to settle
// it. It then logs the drop, writes a one-shot drop note where a session
// exists to carry it, and clears the record.
func (s *Service) dropCeilingDeferral(
	ctx context.Context, task *models.Task, sessionID string, deferral models.CeilingDeferral, reasonCode, detail string,
) {
	if !s.failQueuedAutomationRun(ctx, deferral.Payload, fmt.Sprintf("the queued launch was dropped: %s", detail)) {
		return
	}
	s.logger.Zap().Warn("dropping a ceiling-deferred launch",
		zap.String("task_id", task.ID),
		zap.String("kind", string(deferral.Kind)),
		zap.String(ceilingFieldReasonCode, reasonCode),
		zap.String("detail", detail))

	if sessionID != "" && s.messageCreator != nil {
		if err := s.messageCreator.CreateSessionMessage(
			ctx, task.ID,
			fmt.Sprintf("The queued launch for this session was dropped: %s.", detail),
			sessionID, string(v1.MessageTypeStatus), "",
			map[string]interface{}{metaKeyVariant: metaVariantCeiling, ceilingFieldReasonCode: reasonCode},
			false,
		); err != nil {
			s.logger.Zap().Warn("could not write the ceiling drop card note",
				zap.String("task_id", task.ID), zap.String("session_id", sessionID), zap.Error(err))
		}
	}

	s.clearCeilingDeferredRecord(ctx, task.ID, deferral)
	s.publishTaskUpdatedByID(ctx, task.ID)
}

// clearCeilingDeferredRecord is AC-17's "clear the whole record" disposition,
// applied on both a successful replay and a drop, under the same
// compare-and-set retry loop deferCeilingRefusal itself uses. When an expected
// deferral is supplied, the clear is also bound to that exact record. A stale
// replay must not remove a newer successor that won the intervening CAS.
func (s *Service) clearCeilingDeferredRecord(ctx context.Context, taskID string, expected ...models.CeilingDeferral) {
	ctx, release := s.lockCeilingEntryAdmission(ctx, taskID)
	defer release()
	for attempt := 0; attempt < deferredLaunchCASRetryBudget; attempt++ {
		existingRaw, prior, err := s.repo.GetTaskDeferredLaunch(ctx, taskID)
		if err != nil {
			s.logger.Zap().Warn("could not read deferred launch while clearing a ceiling record",
				zap.String("task_id", taskID), zap.Error(err))
			return
		}
		if len(expected) > 0 {
			current, readErr := models.ReadCeilingDeferral(existingRaw)
			if readErr != nil {
				return
			}
			equivalent, compareErr := sameCeilingDeferralIdentity(current, expected[0])
			if compareErr != nil {
				s.logger.Zap().Warn("could not compare deferred launch while clearing a ceiling record",
					zap.String("task_id", taskID), zap.Error(compareErr))
				return
			}
			if !equivalent {
				s.logger.Zap().Info("leaving a newer ceiling deferred record in place after stale replay",
					zap.String("task_id", taskID))
				return
			}
		}
		if claim, ok := ctx.Value(ceilingDispatchClaimContextKey{}).(*ceilingDeferredLaunchClaim); ok && claim.taskID == taskID {
			claimID, _, held := models.ReadCeilingLaunchClaim(existingRaw)
			if !held || claimID != claim.id {
				s.logger.Zap().Debug("skipping ceiling record clear: claim replaced by successor",
					zap.String("task_id", taskID))
				return
			}
		}
		record := stripCeilingRecordKeys(existingRaw)
		stored, lostCompare, err := s.repo.SetTaskDeferredLaunchIfUnchanged(ctx, taskID, prior, record)
		if err != nil {
			s.logger.Zap().Warn("could not clear a ceiling deferred record",
				zap.String("task_id", taskID), zap.Error(err))
			return
		}
		if stored {
			return
		}
		if lostCompare {
			continue
		}
		return
	}
	s.logger.Zap().Warn("could not clear a ceiling deferred record after repeated compare-and-set losses",
		zap.String("task_id", taskID))
}

// ceilingDeferralIdentityKey fingerprints a queued launch for the retry
// schedule. Kind and queue time are the identity sameCeilingDeferralIdentity
// compares, so two records that differ only in capacity observations or surface
// bookkeeping share a key and keep their backoff.
func ceilingDeferralIdentityKey(deferral models.CeilingDeferral) string {
	return string(deferral.Kind) + "|" + deferral.QueuedAt.UTC().Format(time.RFC3339Nano)
}

// sameCeilingDeferralIdentity compares the stable identity of one queued
// launch. Capacity observations and surface bookkeeping may change while a
// record waits, but a replacement workflow entry must never inherit the state
// repair or dispatch of its predecessor.
func sameCeilingDeferralIdentity(a, b models.CeilingDeferral) (bool, error) {
	if a.Origin != b.Origin || !a.QueuedAt.Equal(b.QueuedAt) {
		return false, nil
	}
	return models.CeilingDeferralsEquivalent(a, b)
}

// stripCeilingRecordKeys removes every ceiling_-prefixed key from a
// deferred_launch record, leaving any coexisting WIP-overflow or
// dependency-chain intent untouched: the ceiling_ prefix (IsCeilingRecordKey)
// is exactly how the two halves of the shared record partition without
// either side tracking the other's keys. SetTaskDeferredLaunchIfUnchanged
// rejects a nil value, so a record with no keys at all is an empty object,
// never nil.
func stripCeilingRecordKeys(existing interface{}) map[string]interface{} {
	current, ok := existing.(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	stripped := make(map[string]interface{}, len(current))
	for key, value := range current {
		if models.IsCeilingRecordKey(key) {
			continue
		}
		stripped[key] = value
	}
	return stripped
}

// loadCeilingReplayTask releases admission before session guards or provider
// callbacks can run. Only the immutable workflow binding crosses into dispatch;
// the durable claim owns replay until settlement.
func (s *Service) loadCeilingReplayTask(ctx context.Context, taskID string, deferral models.CeilingDeferral) (*models.Task, ceilingReplayResult) {
	ctx, release := s.lockCeilingEntryAdmission(ctx, taskID)
	defer release()
	current, err := s.repo.GetTask(ctx, taskID)
	if err != nil || current == nil {
		return nil, ceilingReplayFailedResult(err, "task could not be reloaded")
	}
	disposition, detail, validationErr := s.validateCeilingEntry(ctx, current, deferral)
	if validationErr != nil || disposition == ceilingEntryUnavailable {
		return nil, ceilingReplayFailedResult(validationErr, "workflow entry ownership unavailable: "+detail)
	}
	if disposition == ceilingEntrySuperseded {
		return nil, ceilingReplaySupersededResult()
	}
	return current, ceilingReplaySucceededResult()
}

// replayCeilingDeferral dispatches to the one replay function matching the
// deferral's kind. The closed set is exhaustive; a kind outside it was
// already rejected as unreplayable by ReadCeilingDeferral before this point.
func (s *Service) replayCeilingDeferral(ctx context.Context, task *models.Task, deferral models.CeilingDeferral) ceilingReplayResult {
	if task == nil || task.ID == "" {
		return ceilingReplayFailedResult(nil, "task is unavailable")
	}
	current, load := s.loadCeilingReplayTask(ctx, task.ID, deferral)
	if current == nil {
		return load
	}
	replayCtx := ctx
	if binding, present, bindingErr := models.ReadCeilingWorkflowEntryBinding(deferral.Payload); bindingErr == nil && present {
		replayCtx = withCeilingEntryBinding(ctx, &binding)
	}
	replayCtx = withCeilingEntryKind(replayCtx, deferral.Kind)
	switch deferral.Kind {
	case models.CeilingLaunchStart:
		return s.replayCeilingLaunchStart(replayCtx, current, deferral.Payload)
	case models.CeilingLaunchStartCreated:
		return s.replayCeilingLaunchStartCreated(replayCtx, current, deferral.Payload)
	case models.CeilingLaunchPromptEnsure:
		return s.replayCeilingLaunchPromptEnsure(replayCtx, current, deferral.Payload)
	case models.CeilingLaunchWorkflowStepEnsure:
		return s.replayCeilingLaunchWorkflowStepEnsure(replayCtx, current, deferral.Payload)
	case models.CeilingLaunchQueueDrainEnsure:
		return s.replayCeilingLaunchQueueDrainEnsure(replayCtx, current, deferral.Payload)
	case models.CeilingLaunchResume:
		return s.replayCeilingLaunchResume(replayCtx, current, deferral.Payload)
	case models.CeilingLaunchDynamicRelaunch:
		return s.replayCeilingLaunchDynamicRelaunch(replayCtx, current, deferral.Payload)
	default:
		return ceilingReplayFailedResult(nil, "unrecognized ceiling launch kind")
	}
}

// stringField and boolField read a primitive out of a generically-decoded
// payload map, defaulting to the zero value for an absent or mistyped key.
func stringField(payload map[string]interface{}, key string) string {
	v, _ := payload[key].(string)
	return v
}

func boolField(payload map[string]interface{}, key string) bool {
	v, _ := payload[key].(bool)
	return v
}

// int64Field reads an integer out of a generically-decoded payload map. A
// stored payload reaches the orchestrator through the repository's own read
// path, which decodes with json.Decoder.UseNumber, so a stored integer arrives
// as json.Number rather than the float64 an ordinary
// map[string]interface{} decode would produce. All three representations are
// accepted: missing the json.Number case silently reads every stored integer as
// zero, which makes a re-derived launch payload differ from the record it came
// from and turns a repeat refusal into a launch collision.
func int64Field(payload map[string]interface{}, key string) int64 {
	switch v := payload[key].(type) {
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			return 0
		}
		return parsed
	case float64:
		return int64(v)
	case int64:
		return v
	default:
		return 0
	}
}

// remintLaunchCredentials refreshes the short-lived Office runtime
// credentials redactedCeilingLaunchEnv stripped before persisting, using
// whichever LaunchCredentialReminter was wired (nil for a non-Office
// deployment, or when no reminter is registered — env round-trips unchanged
// in that case). sessionID is the session this launch will use; a
// ceiling-deferred replay has none yet, so it passes "" and the re-minter
// falls back to the run's persisted session id.
//
// A re-mint failure is returned, never swallowed: the captured env is not a
// safe fallback for an Office launch. On a first launch it carries the token
// minted before this task session existed (empty session id, which every
// session-scoped runtime action rejects), and on a ceiling replay the bearer
// keys were redacted before persisting, so proceeding would dispatch with no
// runtime credentials at all. Both recreate the handoff failure this re-mint
// exists to prevent, so the caller must fail or retain the launch instead.
func (s *Service) remintLaunchCredentials(ctx context.Context, taskID, sessionID string, env map[string]string) (map[string]string, error) {
	if s.launchCredentialReminter == nil || len(env) == 0 {
		return env, nil
	}
	refreshed, err := s.launchCredentialReminter.RemintLaunchCredentials(ctx, taskID, sessionID, env)
	if err != nil {
		return nil, fmt.Errorf("launch credential re-mint: %w", err)
	}
	return refreshed, nil
}

// decodeCeilingPayloadField reconstructs a typed replay field from its
// generically-decoded form. The original payload was built by placing typed
// Go values directly into a map[string]interface{} that the repository then
// json.Marshaled; marshaling this generic form back to bytes and unmarshaling
// into target recovers the original shape through the same struct tags. A
// malformed or absent value leaves target at its zero value.
func decodeCeilingPayloadField(raw interface{}, target interface{}) {
	if raw == nil {
		return
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, target)
}

// replayCeilingLaunchStart replays a "start" record by calling startTask with
// the payload seam1StartPayload captured. A start that serves an automation
// run is launched inside that run's dispatch, so the launch binds the run or
// fails it, and a run that closed first yields ceilingReplayRunClosed without
// a launch.
func (s *Service) replayCeilingLaunchStart(ctx context.Context, task *models.Task, payload map[string]interface{}) ceilingReplayResult {
	var attachments []v1.MessageAttachment
	decodeCeilingPayloadField(payload[metaKeyAttachments], &attachments)
	var env map[string]string
	decodeCeilingPayloadField(payload["env"], &env)
	var route *executor.RouteOverride
	decodeCeilingPayloadField(payload["route"], &route)
	var additionalSkillSlugs []string
	decodeCeilingPayloadField(payload["additional_skill_slugs"], &additionalSkillSlugs)
	var entryOptions *workflowmove.EntryOptions
	decodeCeilingPayloadField(payload["entry_options"], &entryOptions)
	var entryBinding *models.CeilingWorkflowEntryBinding
	if binding, present, err := models.ReadCeilingWorkflowEntryBinding(payload); err == nil && present {
		entryBinding = &binding
	}

	// No session exists yet at seam 1's re-mint point; the start this replay
	// drives re-mints again with the session it creates, so the token ends up
	// session-scoped even though this early pass is not. A failed re-mint must
	// not fall through to the start below: the persisted env has had its
	// bearer keys redacted, so proceeding would dispatch without runtime
	// credentials. Retain the record for a later sweep instead.
	remintedEnv, remintErr := s.remintLaunchCredentials(ctx, task.ID, "", env)
	if remintErr != nil {
		s.logger.Zap().Warn("ceiling replay: credential re-mint failed; retaining the record for a later sweep",
			zap.String("task_id", task.ID), zap.Error(remintErr))
		return ceilingReplayFailedResult(remintErr, "credential re-mint failed")
	}
	env = remintedEnv
	automationRun := automationRunFromCeilingPayload(payload)

	opts := startTaskOptions{
		ProfileExplicit:      boolField(payload, "profile_explicit"),
		Env:                  env,
		Route:                route,
		AdditionalSkillSlugs: additionalSkillSlugs,
		EntryOptions:         entryOptions,
		WorkflowEntryID:      int64Field(payload, "workflow_entry_id"),
		// Origin must round-trip rather than be re-derived from auto_start:
		// an AC-13d caller (Office-routed launch) passes autoStart=false with
		// an explicit automatic Origin override, and re-deriving from
		// auto_start alone would replay it as a manual override that bypasses
		// the ceiling.
		Origin:              launchOrigin(stringField(payload, "origin")),
		AutomationRun:       automationRun,
		ceilingEntryBinding: entryBinding,
	}
	if spawnRaw, ok := payload["spawn_origin"].(map[string]interface{}); ok {
		opts.SpawnOrigin = &SpawnOrigin{
			TaskID:      stringField(spawnRaw, metaKeyTaskID),
			SessionID:   stringField(spawnRaw, metaKeySessionID),
			SessionName: stringField(spawnRaw, "session_name"),
		}
	}

	execution, err := s.startQueuedAutomationRun(ctx, task.ID, automationRun, func() (*executor.TaskExecution, error) {
		return s.startTask(
			ctx, task.ID,
			stringField(payload, metaKeyAgentProfileID),
			stringField(payload, "executor_id"),
			stringField(payload, metaKeyExecutorProfile),
			stringField(payload, "priority"),
			stringField(payload, metaKeyPrompt),
			stringField(payload, metaKeyWorkflowStepID),
			boolField(payload, metaKeyPlanMode),
			boolField(payload, "auto_start"),
			attachments,
			opts,
		)
	})
	// startTask reports a repeat refusal as ErrCeilingLaunchDeferred, not as
	// (nil, nil) — ceilingReplayResultFromExecution's execution==nil case
	// would otherwise never fire and a still-refused replay would be
	// misclassified as ceilingReplayFailed (a non-ceiling failure), skipping
	// the AC-49g card-surface retry and logging a misleading warning.
	if errors.Is(err, ErrCeilingLaunchDeferred) || errors.Is(err, ErrCeilingLaunchConflict) {
		return ceilingReplayDeferredResult()
	}
	if errors.Is(err, errCeilingAutomationRunClosed) {
		return ceilingReplayRunClosedResult()
	}
	return ceilingReplayResultFromExecution(execution, err)
}

// replayCeilingLaunchStartCreated replays an AC-42d "start_created" record.
func (s *Service) replayCeilingLaunchStartCreated(ctx context.Context, task *models.Task, payload map[string]interface{}) ceilingReplayResult {
	var attachments []v1.MessageAttachment
	decodeCeilingPayloadField(payload[metaKeyAttachments], &attachments)
	var references []v1.EntityReference
	decodeCeilingPayloadField(payload["references"], &references)
	var entryBinding *models.CeilingWorkflowEntryBinding
	if binding, present, err := models.ReadCeilingWorkflowEntryBinding(payload); err == nil && present {
		entryBinding = &binding
	}

	options := startCreatedSessionOptions{
		skipTaskDescriptionFallback: boolField(payload, "skip_task_description_fallback"),
		promptAlreadyComposed:       boolField(payload, "prompt_already_composed"),
		retryPrompt:                 stringField(payload, "retry_prompt"),
		canvasGuidanceResolved:      boolField(payload, "canvas_guidance_resolved"),
		includeCanvasGuidance:       boolField(payload, "include_canvas_guidance"),
		promptReferencesPrepared:    boolField(payload, "prompt_references_prepared"),
		preserveDirectPrompt:        boolField(payload, "preserve_direct_prompt"),
		ceilingEntryBinding:         entryBinding,
	}

	execution, err := s.startCreatedSession(
		ctx, task.ID,
		stringField(payload, metaKeySessionID),
		stringField(payload, metaKeyAgentProfileID),
		stringField(payload, metaKeyPrompt),
		boolField(payload, "skip_message_record"),
		boolField(payload, metaKeyPlanMode),
		boolField(payload, "auto_start"),
		attachments,
		references,
		stringField(payload, "prompt_reference_context"),
		options,
	)
	return ceilingReplayResultFromExecution(execution, err)
}

// replayCeilingLaunchPromptEnsure replays an AC-42 "prompt_ensure" record.
// promptTask has no seam-3 "deferred" return of its own; the caller (seam 3's
// own ensureSessionRunning gate) reports a refusal as a *seam3Refusal error,
// and an accept-then-fail-after dispatch as an acceptedPromptDispatchError —
// both distinguishable from an ordinary failure.
func (s *Service) replayCeilingLaunchPromptEnsure(ctx context.Context, task *models.Task, payload map[string]interface{}) ceilingReplayResult {
	var attachments []v1.MessageAttachment
	decodeCeilingPayloadField(payload[metaKeyAttachments], &attachments)

	options := promptTaskOptions{
		lifecyclePrompt:           boolField(payload, "lifecycle_prompt"),
		preservePromptContext:     boolField(payload, "preserve_prompt_context"),
		reserveTurnUntilDispatch:  boolField(payload, "reserve_turn_until_dispatch"),
		requireNonterminalSession: boolField(payload, "require_nonterminal_session"),
		promptAlreadyComposed:     boolField(payload, "prompt_already_composed"),
		fallbackLaunchPrompt:      stringField(payload, "fallback_launch_prompt"),
		fallbackRetryPrompt:       stringField(payload, "fallback_retry_prompt"),
		promptReferenceContext:    stringField(payload, "prompt_reference_context"),
	}
	if binding, present, bindingErr := models.ReadCeilingWorkflowEntryBinding(payload); bindingErr == nil && present {
		options.ceilingEntryBinding = &binding
	}

	_, err := s.promptTask(
		ctx, task.ID,
		stringField(payload, metaKeySessionID),
		stringField(payload, metaKeyPrompt),
		stringField(payload, sessionModelConfigKey),
		boolField(payload, metaKeyPlanMode),
		attachments,
		boolField(payload, "dispatch_only"),
		launchOriginAutomatic,
		options,
	)
	if err == nil {
		return ceilingReplaySucceededResult()
	}
	if errors.Is(err, ErrCeilingLaunchSuperseded) {
		return ceilingReplaySupersededResult()
	}
	if _, ok := isSeam3Refusal(err); ok {
		return ceilingReplayDeferredResult()
	}
	if accepted, ok := err.(interface{ DetachedResumeAccepted() bool }); ok && accepted.DetachedResumeAccepted() {
		// agentctl accepted the prompt despite the reported error: the
		// launch did happen, so this is not a ceiling matter any longer.
		return ceilingReplaySucceededResult()
	}
	return ceilingReplayFailedResult(err, "prompt ensure dispatch failed")
}

// replayCeilingLaunchWorkflowStepEnsure replays an AC-42 "workflow_step_ensure"
// record. StartSessionForWorkflowStep reports its own still-deferred outcome
// via the distinguishable errSeam3WorkflowStepEnsureDeferred sentinel.
func (s *Service) replayCeilingLaunchWorkflowStepEnsure(ctx context.Context, task *models.Task, payload map[string]interface{}) ceilingReplayResult {
	err := s.StartSessionForWorkflowStep(
		ctx, task.ID,
		stringField(payload, metaKeySessionID),
		stringField(payload, metaKeyWorkflowStepID),
	)
	switch {
	case err == nil:
		return ceilingReplaySucceededResult()
	case errors.Is(err, ErrCeilingLaunchSuperseded):
		return ceilingReplaySupersededResult()
	case errors.Is(err, errSeam3WorkflowStepEnsureDeferred):
		return ceilingReplayDeferredResult()
	default:
		return ceilingReplayFailedResult(err, "workflow step ensure dispatch failed")
	}
}

// replayCeilingLaunchQueueDrainEnsure replays an AC-42 "queue_drain_ensure"
// record. tryEnsureExecution reports nothing at all — success is read back
// from the session's own state, the same signal AC-1 population counting
// itself uses.
func (s *Service) replayCeilingLaunchQueueDrainEnsure(ctx context.Context, task *models.Task, payload map[string]interface{}) ceilingReplayResult {
	sessionID := stringField(payload, metaKeySessionID)
	err := s.tryEnsureExecutionWithBinding(ctx, sessionID, seam3CallShapeQueueDrain, launchOriginAutomatic, stringField(payload, "queued_message_id"), ceilingEntryBindingFromContext(ctx))
	if errors.Is(err, ErrCeilingLaunchSuperseded) {
		return ceilingReplaySupersededResult()
	}
	if err != nil {
		if _, ok := isSeam3Refusal(err); !ok {
			return ceilingReplayFailedResult(err, "queue drain ensure dispatch failed")
		}
	}

	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		return ceilingReplayDeferredResult()
	}
	if isAC1SessionState(session.State) {
		return ceilingReplaySucceededResult()
	}
	return ceilingReplayDeferredResult()
}

// replayCeilingLaunchResume replays an AC-42 "resume" record.
func (s *Service) replayCeilingLaunchResume(ctx context.Context, task *models.Task, payload map[string]interface{}) ceilingReplayResult {
	execution, err := s.ResumeTaskSessionWithOptions(
		ctx, task.ID,
		stringField(payload, metaKeySessionID),
		executor.ResumeOptions{
			AllowBranchReplacement: boolField(payload, "allow_branch_replacement"),
			Origin:                 string(launchOriginAutomatic),
		},
	)
	return ceilingReplayResultFromExecution(execution, err)
}

// replayCeilingLaunchDynamicRelaunch replays an AC-42f "dynamic_relaunch"
// record. A repeated ceiling refusal remains deferred; a launch failure after
// admission is retained as a non-ceiling failure for the next sweep without
// being reported as another refusal.
func (s *Service) replayCeilingLaunchDynamicRelaunch(ctx context.Context, task *models.Task, payload map[string]interface{}) ceilingReplayResult {
	data := watcher.AgentEventData{
		TaskID:           stringField(payload, metaKeyTaskID),
		SessionID:        stringField(payload, metaKeySessionID),
		AgentExecutionID: stringField(payload, "agent_execution_id"),
		AgentProfileID:   stringField(payload, metaKeyAgentProfileID),
	}
	switch s.relaunchDynamicTaskAfterFailureOutcomeWithBinding(ctx, data, stringField(payload, "execution_profile_id"), launchOriginAutomatic, ceilingEntryBindingFromContext(ctx)) {
	case dynamicRelaunchSucceeded:
		return ceilingReplaySucceededResult()
	case dynamicRelaunchDeferred:
		return ceilingReplayDeferredResult()
	case dynamicRelaunchSuperseded:
		return ceilingReplaySupersededResult()
	default:
		return ceilingReplayFailedResult(nil, "dynamic relaunch failed")
	}
}
