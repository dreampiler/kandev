package orchestrator

import (
	"strings"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
)

// A lazy resume (a message, a queued dispatch, or any prompt that wakes an
// idle session) starts its agent asynchronously and only sees the session
// turn FAILED. When native resume refused the session's durable delivery
// state — a stream SQL never received (agent delivery checkpoint missing) or
// peer and SQL evidence that disagree (durable adoption blocked) — retrying
// the same resume can never succeed. Explicit recovery already continues such
// a session from history; the helpers below report the refusal to the lazy
// resume caller as a recovery-required error, so the message and queue paths
// take their existing automatic continue_from_history branch instead of
// failing the message.

// resumeContinuationRequiredError marks a lazy resume that needs
// continue_from_history. It unwraps to ErrSessionRecoveryRequired and to the
// startup failure, and reports the unresolved-durable-work reason.
type resumeContinuationRequiredError struct {
	cause error
}

func (e *resumeContinuationRequiredError) Error() string {
	return ErrSessionRecoveryRequired.Error() + ": " + durableDeliveryUnresolvedReason + ": " + e.cause.Error()
}

func (e *resumeContinuationRequiredError) Unwrap() []error {
	return []error{ErrSessionRecoveryRequired, e.cause}
}

func (e *resumeContinuationRequiredError) RecoveryReason() string {
	return durableDeliveryUnresolvedReason
}

// noteStartupFailureNeedingContinuation records, from the asynchronous start
// failure callback, that a session's agent start was refused for a reason only
// continue_from_history settles.
func (s *Service) noteStartupFailureNeedingContinuation(sessionID string, startErr error) {
	if sessionID == "" || !resumeNeedsContextContinuation(startErr) {
		return
	}
	s.startupFailuresNeedingContinuation.Store(sessionID, struct{}{})
}

// startupFailureNeedsContinuation reports whether a session that turned FAILED
// during startup needs continue_from_history. The typed record from the start
// failure callback is authoritative; the persisted error text covers the
// window in which the FAILED projection is visible before that callback runs.
func (s *Service) startupFailureNeedsContinuation(sessionID, errorMessage string) bool {
	if _, ok := s.startupFailuresNeedingContinuation.LoadAndDelete(sessionID); ok {
		return true
	}
	return startupFailureTextNeedsContinuation(errorMessage)
}

func startupFailureTextNeedsContinuation(errorMessage string) bool {
	return strings.Contains(errorMessage, repoerrors.ErrAgentDeliveryCheckpointMissing.Error()) ||
		strings.Contains(errorMessage, agentruntime.ErrDurableAdoptionBlocked.Error())
}
