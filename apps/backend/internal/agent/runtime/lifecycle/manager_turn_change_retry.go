package lifecycle

import (
	"context"
	"errors"
	"time"

	"github.com/kandev/kandev/internal/task/repository/repoerrors"
	"go.uber.org/zap"
)

const turnChangeMaximumAttempts = 3

// Retries retain the captured terminal identity, including across the inline
// completion attempt and its background continuation.
func (m *Manager) retryTurnChangeOperation(
	execution *AgentExecution, terminal TurnChangeTerminal, completed int, timeout time.Duration,
	operation func(context.Context, TurnChangeTerminal, TurnChangeCheckpointClient) error,
) error {
	var err error
	for attempt := completed; attempt < turnChangeMaximumAttempts; attempt++ {
		if !m.waitTurnChangeRetry(attempt) {
			return context.Canceled
		}
		client, release := execution.AcquireAgentCtlClient()
		ctx, cancel := m.turnChangeAttemptContext(timeout)
		err = operation(ctx, terminal, client)
		cancel()
		release()
		if err == nil || permanentTurnChangeError(err) {
			return err
		}
		timeout = turnChangeTerminalCaptureTimeout
	}
	return err
}

func (m *Manager) waitTurnChangeRetry(attempt int) bool {
	delay := time.Duration(0)
	if attempt > 0 {
		delay = min(turnChangeRetryInitialDelay<<uint(attempt-1), turnChangeRetryMaximumDelay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-m.stopCh:
		return false
	case <-timer.C:
		return !m.shuttingDown.Load()
	}
}

// Every leaf of a joined error must be permanent. An unrelated storage failure
// must retain its retry policy even when it accompanies a missing summary.
func permanentTurnChangeError(err error) bool {
	if err == repoerrors.ErrTurnChangeSetNotFound || err == repoerrors.ErrTurnChangeRelationship {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !permanentTurnChangeError(child) {
				return false
			}
		}
		return true
	}
	if wrapped := errors.Unwrap(err); wrapped != nil {
		return permanentTurnChangeError(wrapped)
	}
	return false
}

func (m *Manager) abandonTurnChange(execution *AgentExecution, terminal TurnChangeTerminal, cause error) bool {
	select {
	case <-m.stopCh:
		return false
	default:
	}
	handler, ok := m.turnChangeCaptureHandler.(interface {
		AbandonTurnChanges(context.Context, TurnChangeTerminal, TurnChangeCheckpointClient) error
	})
	if !ok {
		m.logger.Error("turn-change terminal failure has no settlement handler",
			zap.String("execution_id", terminal.ExecutionID), zap.Error(cause))
		return false
	}
	client, release := execution.AcquireAgentCtlClient()
	defer release()
	ctx, cancel := m.turnChangeAttemptContext(turnChangeTerminalCaptureTimeout)
	defer cancel()
	err := handler.AbandonTurnChanges(ctx, terminal, client)
	m.logger.Warn("turn-change processing stopped after terminal failure",
		zap.String("execution_id", terminal.ExecutionID), zap.String("turn_id", terminal.TurnID),
		zap.Uint64("prompt_generation", terminal.PromptGeneration), zap.Error(cause), zap.NamedError("settlement_error", err))
	return err == nil
}

func finishRetriedTurnChangeCapture(execution *AgentExecution, terminal TurnChangeTerminal) {
	execution.startupCallbackMu.RLock()
	defer execution.startupCallbackMu.RUnlock()
	if execution.ID != terminal.ExecutionID || execution.currentStartupAttemptID() != terminal.StartupAttemptID {
		return
	}
	execution.promptLifecycleMu.Lock()
	defer execution.promptLifecycleMu.Unlock()
	clearTurnChangeCaptureAnchor(execution, terminal.PromptGeneration)
	finishTurnChangeCaptureLocked(execution, terminal.PromptGeneration)
}

func (m *Manager) startTurnChangeRetry(work func()) {
	if m == nil || m.stopCh == nil || m.shuttingDown.Load() {
		return
	}
	m.turnChangeRetryMu.Lock()
	defer m.turnChangeRetryMu.Unlock()
	select {
	case <-m.stopCh:
		return
	default:
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		work()
	}()
}
