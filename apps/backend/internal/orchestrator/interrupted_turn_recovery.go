package orchestrator

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
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
	reason := recoveryReasonFromError(err)
	return reason == unknownPromptOutcome || reason == durableDeliveryUnresolvedReason
}

func (s *Service) recoverSessionWithContinuationFallback(
	ctx context.Context,
	taskID, sessionID, action string,
	options RecoverSessionOptions,
) (*LaunchSessionResponse, error) {
	options.singleAttempt = true
	resp, err := s.RecoverSessionWithOptions(ctx, taskID, sessionID, action, options)
	if action != recoveryActionResume || !resumeNeedsContextContinuation(err) {
		return resp, err
	}
	s.logger.Warn("native resume cannot settle the interrupted turn; continuing from history",
		zap.String("task_id", taskID),
		zap.String("session_id", sessionID),
		zap.Error(err))
	fallback, fallbackErr := s.RecoverSessionWithOptions(
		ctx, taskID, sessionID, recoveryActionContinueHistory, RecoverSessionOptions{singleAttempt: true},
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
	if s == nil || taskID == "" || sessionID == "" {
		return false
	}
	reason := recoveryReasonFromError(cause)
	if reason == "" {
		block, err := s.GetOpenSessionRecoveryBlock(ctx, sessionID)
		if err != nil || block == nil {
			return false
		}
		reason = block.Reason
	}
	if !interruptedTurnRecoveryReasons[reason] {
		return false
	}
	if !s.claimInterruptedTurnAutoRecovery(sessionID) {
		return false
	}
	s.logger.Info("starting automatic continue_from_history for an interrupted turn",
		zap.String("task_id", taskID),
		zap.String("session_id", sessionID),
		zap.String("reason", reason))
	go func() {
		defer s.finishInterruptedTurnAutoRecovery(sessionID)
		recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), interruptedTurnAutoRecoveryTimeout)
		defer cancel()
		if _, err := s.RecoverSessionWithOptions(
			recoveryCtx, taskID, sessionID, recoveryActionContinueHistory, RecoverSessionOptions{singleAttempt: true},
		); err != nil {
			s.logger.Warn("automatic continue_from_history failed",
				zap.String("task_id", taskID),
				zap.String("session_id", sessionID),
				zap.Error(err))
			return
		}
		s.logger.Info("automatic continue_from_history started the interrupted session",
			zap.String("task_id", taskID),
			zap.String("session_id", sessionID))
	}()
	return true
}

type interruptedTurnAutoRecoveryState struct {
	running   bool
	startedAt time.Time
}

func (s *Service) claimInterruptedTurnAutoRecovery(sessionID string) bool {
	s.interruptedTurnAutoRecoveryMu.Lock()
	defer s.interruptedTurnAutoRecoveryMu.Unlock()
	if s.interruptedTurnAutoRecovery == nil {
		s.interruptedTurnAutoRecovery = make(map[string]interruptedTurnAutoRecoveryState)
	}
	state := s.interruptedTurnAutoRecovery[sessionID]
	now := time.Now()
	if state.running || now.Sub(state.startedAt) < interruptedTurnAutoRecoveryCooldown {
		return false
	}
	s.interruptedTurnAutoRecovery[sessionID] = interruptedTurnAutoRecoveryState{running: true, startedAt: now}
	return true
}

func (s *Service) finishInterruptedTurnAutoRecovery(sessionID string) {
	s.interruptedTurnAutoRecoveryMu.Lock()
	defer s.interruptedTurnAutoRecoveryMu.Unlock()
	state := s.interruptedTurnAutoRecovery[sessionID]
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
