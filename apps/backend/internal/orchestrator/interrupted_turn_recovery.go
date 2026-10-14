package orchestrator

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
)

// A turn that was in flight when Kandev (and its agentctl runtime) restarted
// leaves an unsettled durable delivery: the SQL submission stays
// "dispatching" and the retained agentctl journal reports unresolved work.
// Native resume then cannot prove the old prompt's outcome
// (agentruntime.ErrDurableAdoptionBlocked) and every new prompt is refused with
// unresolved_durable_work / unknown_prompt_outcome until an operator chooses
// continue_from_history. The helpers below take that operator step
// automatically so an interrupted session moves again without owner action.
// continue_from_history never re-sends the interrupted prompt; it starts a
// new harness generation from the bounded transcript, which retires the older
// generation's unresolved journal work.

const (
	dynamicRouteStatusStarting = "starting"
	dynamicRouteStatusRetrying = "retrying"

	// interruptedTurnAutoRecoveryCooldown bounds automatic continuation
	// attempts per session so a persistently failing recovery is not retried
	// on every queued nudge.
	interruptedTurnAutoRecoveryCooldown = time.Minute
	interruptedTurnAutoRecoveryTimeout  = 5 * time.Minute
	// interruptedTurnAutoRecoveryMaxAttempts bounds how many model-driven
	// recoveries a single unresolved durable record may consume before
	// automatic continuation stops. The bounded record is then left to an
	// operator: the open session recovery block stays as the surface, and
	// nothing is force-settled just to end the loop.
	interruptedTurnAutoRecoveryMaxAttempts = 3
)

// interruptedTurnRecoveryReasons are the persisted block reasons produced by a
// restart-interrupted turn or dispatch that continue_from_history settles.
var interruptedTurnRecoveryReasons = map[string]bool{
	unknownPromptOutcome:               true,
	durableDeliveryUnresolvedReason:    true,
	unknownQueueDispatchRecoveryReason: true,
}

func recoveryReasonFromError(err error) string {
	var signal recoveryRequiredSignal
	if errors.As(err, &signal) {
		return signal.RecoveryReason()
	}
	return ""
}

// resumeNeedsContextContinuation reports whether a native resume failed only
// because the interrupted turn's durable delivery cannot be settled natively.
func resumeNeedsContextContinuation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, agentruntime.ErrDurableAdoptionBlocked) {
		return true
	}
	// The journal still holds a stream SQL never received (for example output
	// dropped before its first receipt). Native resume refuses that missing
	// checkpoint by design; a fresh successor generation does not depend on it.
	if errors.Is(err, repoerrors.ErrAgentDeliveryCheckpointMissing) {
		return true
	}
	reason := recoveryReasonFromError(err)
	return reason == unknownPromptOutcome || reason == durableDeliveryUnresolvedReason
}

// RecoverSessionWithContinuationFallback runs one operator recovery action.
// A native resume refused only because the restart-interrupted turn's durable
// delivery cannot be settled continues from history in the same request, so
// the caller (recovery UI, external watchdog) does not need a second round
// trip. Office tasks keep their scheduler-owned durable recovery, and a
// refusal raised while a runtime is still live for the session is returned
// unchanged so live work is never replaced.
func (s *Service) RecoverSessionWithContinuationFallback(
	ctx context.Context,
	taskID, sessionID, action string,
	options RecoverSessionOptions,
) (*LaunchSessionResponse, error) {
	resp, err := s.RecoverSessionWithOptions(ctx, taskID, sessionID, action, options)
	if action != recoveryActionResume || !resumeNeedsContextContinuation(err) {
		return resp, err
	}
	if !errors.Is(err, agentruntime.ErrDurableAdoptionBlocked) &&
		s.agentManager != nil && s.agentManager.IsAgentRunningForSession(ctx, sessionID) {
		return resp, err
	}
	if isOfficeTask, officeErr := s.lookupOfficeTask(ctx, taskID); officeErr != nil || isOfficeTask {
		return resp, err
	}
	s.logger.Warn("native resume cannot settle the interrupted turn; continuing from history",
		zap.String("task_id", taskID),
		zap.String("session_id", sessionID),
		zap.Error(err))
	fallback, fallbackErr := s.RecoverSessionWithOptions(
		ctx, taskID, sessionID, recoveryActionContinueHistory, RecoverSessionOptions{Automatic: options.Automatic},
	)
	if fallbackErr != nil {
		return nil, errors.Join(err, fallbackErr)
	}
	return fallback, nil
}

// AutoRecoverBlockedPrompt runs continue_from_history in the background when a
// prompt or queued dispatch was refused because a restart-interrupted turn
// left the session's delivery unsettled. The continuation transcript includes
// the user message that was just recorded, so that message opens the next
// turn. It returns true when a recovery was started.
func (s *Service) AutoRecoverBlockedPrompt(ctx context.Context, taskID, sessionID string, cause error) bool {
	session := s.automaticContinuationSession(ctx, taskID, sessionID)
	if session == nil {
		return false
	}
	reason := recoveryReasonFromError(cause)
	var block *models.SessionRecoveryBlock
	// blockRead reports that the canonical block lookup succeeded. A failed
	// lookup is not evidence of a different record, so it must not reopen the
	// known record's attempt budget.
	blockRead := false
	if reason == "" {
		// The block is the only source of the reason here, so a read failure
		// fails closed, exactly as before.
		var err error
		block, err = s.GetOpenSessionRecoveryBlock(ctx, sessionID)
		if err != nil {
			return false
		}
		blockRead = true
		if block != nil {
			reason = block.Reason
		} else if session.State == models.TaskSessionStateFailed && startupFailureTextNeedsContinuation(session.ErrorMessage) {
			reason = durableDeliveryUnresolvedReason
		}
	} else if current, err := s.GetOpenSessionRecoveryBlock(ctx, sessionID); err == nil {
		// The cause already carries the reason: a block read here is
		// best-effort for a richer attempt record key and never gates the
		// recovery, so a read failure does not drop a cause-known recovery.
		block = current
		blockRead = true
	}
	if !interruptedTurnRecoveryReasons[reason] {
		return false
	}
	recordKey, blockID := interruptedTurnAutoRecoveryRecordKey(block, reason)
	claimed, exhausted, effectiveKey := s.claimInterruptedTurnAutoRecovery(ctx, sessionID, recordKey, blockID, blockRead)
	if !claimed {
		if exhausted {
			s.logger.Zap().Warn("interrupted turn auto-recovery reached its attempt bound; operator action required",
				zap.String("task_id", taskID),
				zap.String("session_id", sessionID),
				zap.String("reason", reason),
				zap.String("record", effectiveKey),
				zap.Int("max_attempts", interruptedTurnAutoRecoveryMaxAttempts))
		}
		return false
	}
	s.logger.Info("starting automatic continue_from_history for an interrupted turn",
		zap.String("task_id", taskID),
		zap.String("session_id", sessionID),
		zap.String("reason", reason))
	go func() {
		defer s.finishInterruptedTurnAutoRecovery(sessionID, effectiveKey)
		recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), interruptedTurnAutoRecoveryTimeout)
		defer cancel()
		resp, err := s.RecoverSessionWithOptions(
			recoveryCtx, taskID, sessionID, recoveryActionContinueHistory, RecoverSessionOptions{Automatic: true},
		)
		if err != nil {
			s.logger.Warn("automatic continue_from_history failed",
				zap.String("task_id", taskID),
				zap.String("session_id", sessionID),
				zap.Error(err))
			return
		}
		if resp != nil && resp.ActivationDisposition == activationDispositionQueued {
			s.logger.Info("automatic continue_from_history deferred by the session ceiling",
				zap.String("task_id", taskID),
				zap.String("session_id", sessionID))
			return
		}
		s.logger.Info("automatic continue_from_history started the interrupted session",
			zap.String("task_id", taskID),
			zap.String("session_id", sessionID))
	}()
	return true
}

func (s *Service) automaticContinuationSession(ctx context.Context, taskID, sessionID string) *models.TaskSession {
	if s == nil || s.repo == nil || taskID == "" || sessionID == "" {
		return nil
	}
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil || session == nil || session.TaskID != taskID ||
		session.State == models.TaskSessionStateCancelled || session.State == models.TaskSessionStateCompleted {
		return nil
	}
	if session.State == models.TaskSessionStateFailed && !s.CanAutoRecoverFailedSession(ctx, taskID, sessionID) {
		return nil
	}
	// A live runtime still owns the interrupted turn: continuation from history
	// must wait for a confirmed termination instead of replacing work that is
	// still running. Only a session with no live execution is carried over.
	if s.sessionRuntimeLive(ctx, sessionID) {
		return nil
	}
	return session
}

// CanAutoRecoverFailedSession admits a failed primary only when its canonical
// block or classified startup refusal requires history continuation.
// A runtime whose termination could not be proved retains that block; ordinary
// startup failures, ended conversations, and live runtimes stay blocked.
func (s *Service) CanAutoRecoverFailedSession(ctx context.Context, taskID, sessionID string) bool {
	if s == nil || s.repo == nil {
		return false
	}
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil || session == nil || session.TaskID != taskID || !session.IsPrimary ||
		session.State != models.TaskSessionStateFailed || s.sessionRuntimeLive(ctx, sessionID) {
		return false
	}
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil || task == nil || taskArchived(task) {
		return false
	}
	block, err := s.GetOpenSessionRecoveryBlock(ctx, sessionID)
	if err != nil {
		return false
	}
	if block != nil {
		return interruptedTurnRecoveryReasons[block.Reason]
	}
	return startupFailureTextNeedsContinuation(session.ErrorMessage)
}

type interruptedTurnAutoRecoveryState struct {
	running   bool
	startedAt time.Time
	// recordKey names the unresolved durable record the attempt budget belongs
	// to. It prefers the delivery submission identity (the exact unresolved
	// work), then the recovery block id; a reason-only key is the fallback when
	// no block is known.
	recordKey string
	// blockID is the canonical recovery block the durable attempt count is
	// recorded on. When set, the bound survives a backend restart; when empty
	// the count in attempts is this process only.
	blockID string
	// attempts is the attempt count for recordKey. It mirrors the durable block
	// count while a block is known and is the whole budget when none is.
	attempts int
	// tracked reports that recordKey/blockID already describe a seen record, so
	// a later reason-only key from a failed read is not mistaken for a new one.
	tracked bool
}

// interruptedTurnAutoRecoveryRecordKey names the durable record an automatic
// continuation is settling. It prefers the delivery submission identity (the
// exact unresolved work), then the recovery block id, then the block reason, so
// two distinct unresolved records on one session cannot share one attempt
// budget. blockID is the row the durable count is recorded on (empty for a
// reason-only fallback).
func interruptedTurnAutoRecoveryRecordKey(block *models.SessionRecoveryBlock, reason string) (recordKey, blockID string) {
	if block != nil {
		if block.DeliverySubmissionID != "" {
			return "submission:" + block.DeliverySubmissionID, block.ID
		}
		if block.ID != "" {
			return "block:" + block.ID, block.ID
		}
	}
	return "reason:" + reason, ""
}

// claimInterruptedTurnAutoRecovery admits one automatic continuation for the
// session's current unresolved record. It returns claimed=false with
// exhausted=true once that record has consumed its whole attempt budget, so the
// caller can leave the still-open recovery block to an operator instead of
// retrying forever. The count is durable on the recovery block when one is
// known, so a backend restart does not reopen the bound; a reason-only key from
// a failed enrichment read keeps the last confirmed record instead of resetting
// it.
func (s *Service) claimInterruptedTurnAutoRecovery(
	ctx context.Context, sessionID, recordKey, blockID string, blockRead bool,
) (claimed, exhausted bool, effectiveKey string) {
	s.interruptedTurnAutoRecoveryMu.Lock()
	defer s.interruptedTurnAutoRecoveryMu.Unlock()
	if s.interruptedTurnAutoRecovery == nil {
		s.interruptedTurnAutoRecovery = make(map[string]interruptedTurnAutoRecoveryState)
	}
	state := s.interruptedTurnAutoRecovery[sessionID]
	now := time.Now()
	// The same-session in-flight ownership outranks a record-key change: while
	// one attempt is running (or its cooldown is unexpired) this entry is left
	// untouched, so a new record cannot start a second attempt and the running
	// goroutine still owns the entry.
	if state.running || now.Sub(state.startedAt) < interruptedTurnAutoRecoveryCooldown {
		return false, false, state.recordKey
	}
	switch {
	case !state.tracked:
		if !blockRead && blockID == "" {
			// A cold session's first canonical lookup failed, so the record
			// identity is unknown. A failed read is not a confirmed absence: a
			// process-local budget must not be opened without charging the real
			// still-open row. Refuse (not the attempt bound) and leave the open
			// block as the operator surface.
			s.logger.Warn("interrupted turn auto-recovery skipped: canonical record identity is unknown",
				zap.String("session_id", sessionID),
				zap.String("reason_key", recordKey))
			return false, false, ""
		}
		state.tracked = true
		state.recordKey = recordKey
		state.blockID = blockID
		state.attempts = 0
	case state.recordKey == recordKey:
		if blockID != "" {
			state.blockID = blockID
		}
	case blockID != "" || blockRead:
		// A confirmed different record, or the confirmed settlement/absence of
		// the previous one, begins with a fresh attempt budget.
		state.recordKey = recordKey
		state.blockID = blockID
		state.attempts = 0
	default:
		// The block enrichment read failed: a reason-only key is not evidence
		// of a different record, so keep the confirmed record and its budget.
		recordKey = state.recordKey
		blockID = state.blockID
	}
	attempts, allowed, uncertain := s.reserveInterruptedTurnAutoRecoveryAttempt(ctx, blockID, state.attempts)
	if !allowed {
		s.interruptedTurnAutoRecovery[sessionID] = state
		if uncertain {
			// The known record could not be charged durably. This is not the
			// attempt bound, so it must not be reported as one; reserve logged
			// the distinct operator surface. No new model recovery is admitted.
			return false, false, state.recordKey
		}
		return false, true, state.recordKey
	}
	state.attempts = attempts
	state.running = true
	state.startedAt = now
	state.recordKey = recordKey
	s.interruptedTurnAutoRecovery[sessionID] = state
	return true, false, state.recordKey
}

// reserveInterruptedTurnAutoRecoveryAttempt consumes one attempt for the
// session's current unresolved record. A record with a canonical block MUST be
// charged durably: if the durable increment cannot be confirmed (store error or
// a store without the durable boundary), the attempt is refused instead of
// being authorized on the in-memory count, so a restart plus a write error
// cannot open a fourth recovery for an already-exhausted record. Only a record
// with no canonical block falls back to the process-local in-memory count; that
// case has no durable identity to charge and is a known process-local limit,
// not a durable guarantee. uncertain reports the durable-charge failure so the
// caller does not confuse it with the attempt bound.
func (s *Service) reserveInterruptedTurnAutoRecoveryAttempt(
	ctx context.Context, blockID string, inMemoryAttempts int,
) (attempts int, allowed, uncertain bool) {
	if blockID != "" {
		store, ok := s.repo.(sessionRecoveryBlockAutoAttemptStore)
		if !ok {
			s.logger.Warn("no durable attempt store for a known recovery block; refusing automatic recovery",
				zap.String("session_recovery_block_id", blockID))
			return inMemoryAttempts, false, true
		}
		attempts, allowed, err := store.IncrementOpenSessionRecoveryBlockAutoAttempts(
			ctx, blockID, interruptedTurnAutoRecoveryMaxAttempts)
		if err != nil {
			s.logger.Warn("failed to persist the interrupted-turn recovery attempt bound; refusing automatic recovery",
				zap.String("session_recovery_block_id", blockID),
				zap.Error(err))
			return inMemoryAttempts, false, true
		}
		return attempts, allowed, false
	}
	if inMemoryAttempts >= interruptedTurnAutoRecoveryMaxAttempts {
		return inMemoryAttempts, false, false
	}
	return inMemoryAttempts + 1, true, false
}

func (s *Service) finishInterruptedTurnAutoRecovery(sessionID, recordKey string) {
	s.interruptedTurnAutoRecoveryMu.Lock()
	defer s.interruptedTurnAutoRecoveryMu.Unlock()
	state := s.interruptedTurnAutoRecovery[sessionID]
	// Only the attempt that owns this record key clears the in-flight flag; a
	// record replaced in the meantime is left untouched.
	if !state.tracked || state.recordKey != recordKey {
		return
	}
	state.running = false
	s.interruptedTurnAutoRecovery[sessionID] = state
}

// activateResumedDynamicRoute completes the durable "starting" -> "active"
// route transition once a resumed dynamic session's runtime is ready. A
// resume re-claims the persisted route as "starting" (applyResolvedExecution)
// but settles the session to WAITING_FOR_INPUT, which the process-start
// callback did not treat as a successful start; the route then stayed
// "starting" and every later explicit recovery was suppressed as
// dynamic_route_pending.
func (s *Service) activateResumedDynamicRoute(ctx context.Context, session *models.TaskSession) {
	if session == nil || session.RouteGeneration <= 0 || session.ExecutionProfileID == "" {
		return
	}
	if session.RouteState != dynamicRouteStatusStarting && session.RouteState != dynamicRouteStatusRetrying {
		return
	}
	s.markDynamicRouteActive(context.WithoutCancel(ctx), session.ID, session.RouteGeneration)
}

// explicitRecoveryOwnsStrandedStartingRoute lets an explicit recovery action
// resume a dynamic session whose route claim was left "starting" with no live
// runtime behind it (for example by a restart, or by a resume that predates
// activateResumedDynamicRoute). The recovery re-claims the same persisted
// candidate; a live launch owner keeps the dynamic_route_pending guard.
func (s *Service) explicitRecoveryOwnsStrandedStartingRoute(
	ctx context.Context,
	session *models.TaskSession,
	options executor.ResumeOptions,
) bool {
	if session == nil || options.RecoveryAction == "" ||
		session.RouteState != dynamicRouteStatusStarting ||
		session.RouteGeneration <= 0 || session.ExecutionProfileID == "" {
		return false
	}
	return s.agentManager == nil || !s.agentManager.IsAgentRunningForSession(ctx, session.ID)
}
