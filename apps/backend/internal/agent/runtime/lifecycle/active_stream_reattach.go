package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"time"

	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	"go.uber.org/zap"
)

var errActiveStreamReattachIneligible = errors.New("active update stream reattachment no longer eligible")

func (sm *StreamManager) retryActiveUpdateStream(ctx context.Context, eligible func() bool, connect func() error) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !eligible() {
			return errActiveStreamReattachIneligible
		}
		if err := connect(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt < 2 && !sm.sleepOrStop(time.Duration(attempt+1)*100*time.Millisecond) {
			return context.Canceled
		}
	}
	return lastErr
}

func (execution *AgentExecution) ownsUpdateStream(generation uint64) bool {
	return execution != nil && execution.updateStreamGeneration.Load() == generation
}

func (execution *AgentExecution) ownsAgentCtlClient(expected *agentctl.Client) bool {
	if execution == nil || expected == nil || !execution.agentctlLifecycleMu.TryRLock() {
		return false
	}
	defer execution.agentctlLifecycleMu.RUnlock()
	return execution.currentAgentCtlClient() == expected
}

func (m *Manager) claimActiveStreamReattach(execution *AgentExecution, promptGeneration uint64) bool {
	if execution == nil || promptGeneration == 0 || execution.stopRequested.Load() || execution.cancelRequested.Load() || m.IsShuttingDown() ||
		m.executionStore.ActivePromptGeneration(execution.ID) != promptGeneration {
		return false
	}
	execution.reattachMu.Lock()
	defer execution.reattachMu.Unlock()
	if execution.reattachClaimedPromptGeneration == promptGeneration {
		return false
	}
	execution.reattachClaimedPromptGeneration = promptGeneration
	return true
}

func (m *Manager) activeStreamReattachEligible(execution *AgentExecution, promptGeneration, startupGeneration uint64) bool {
	if execution.stopRequested.Load() || execution.cancelRequested.Load() || m.IsShuttingDown() ||
		!execution.acceptsStartupAttempt(startupGeneration) ||
		m.executionStore.ActivePromptGeneration(execution.ID) != promptGeneration {
		return false
	}
	current, exists := m.executionStore.Get(execution.ID)
	if !exists || current != execution {
		return false
	}
	client, release := execution.AcquireAgentCtlClient()
	defer release()
	return client != nil && !client.IsClosed()
}

func (m *Manager) tryActiveStreamReattach(execution *AgentExecution, promptGeneration, startupGeneration uint64, connect func() error) bool {
	if m.streamManager == nil || !m.activeStreamReattachEligible(execution, promptGeneration, startupGeneration) ||
		!m.claimActiveStreamReattach(execution, promptGeneration) {
		return false
	}
	eligible := func() bool {
		return m.activeStreamReattachEligible(execution, promptGeneration, startupGeneration)
	}
	err := m.streamManager.retryActiveUpdateStream(context.Background(), eligible, connect)
	if err == nil {
		if !eligible() {
			return true // A stop, replacement, or live completion settled during the dial.
		}
		err = m.reconcileActiveStreamTurnOutcome(execution, promptGeneration, startupGeneration)
	}
	if err == nil {
		m.logger.Info("active update stream reattached", zap.String("execution_id", execution.ID),
			zap.Uint64("prompt_generation", promptGeneration))
		return true
	}
	if !eligible() {
		return true // A stop or replacement owns settlement now.
	}
	// Fence the new reader before closing it so its delayed callback cannot
	// consume the same prompt's failure path a second time.
	execution.updateStreamGeneration.Add(1)
	client, release := execution.AcquireAgentCtlClient()
	if client != nil {
		client.CloseUpdatesStream()
	}
	release()
	m.logger.Warn("active update stream reattachment failed",
		zap.String("execution_id", execution.ID), zap.Uint64("prompt_generation", promptGeneration), zap.Error(err))
	return false
}

func (m *Manager) reconcileActiveStreamTurnOutcome(execution *AgentExecution, promptGeneration, startupGeneration uint64) error {
	if execution.standaloneInstanceID == "" || m.executorRegistry == nil {
		return nil
	}
	backend, err := m.executorRegistry.GetBackend(execution.RuntimeName)
	if err != nil {
		return err
	}
	applier, ok := backend.(turnOutcomeApplier)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !m.runtimeExecutionCurrent(execution) {
		return errActiveStreamReattachIneligible
	}
	outcome, err := applier.fetchTurnOutcomeForEpoch(ctx, execution.standaloneInstanceID, execution.runtimeEpoch)
	if err != nil || outcome == nil {
		return err
	}
	if !m.runtimeExecutionCurrent(execution) || m.executionStore.ActivePromptGeneration(execution.ID) != promptGeneration {
		return nil // A live terminal frame already settled this generation.
	}
	if outcome.Event.PromptGeneration != promptGeneration {
		return fmt.Errorf("retained turn outcome generation %d does not match active generation %d", outcome.Event.PromptGeneration, promptGeneration)
	}
	applied := execution.withStartupAttempt(startupGeneration, func(attemptID string) {
		event := outcome.Event
		event.AttemptID = attemptID
		event.SessionSettingsSourceGeneration = startupGeneration
		execution.markRecoveryTurnOutcomeApplied(outcome.TurnID)
		m.handleAgentEventWithAttempt(execution, event, attemptID)
	})
	if !applied {
		return errActiveStreamReattachIneligible
	}
	if !m.runtimeExecutionCurrent(execution) {
		return errActiveStreamReattachIneligible
	}
	if err := applier.ackTurnOutcomeForEpoch(ctx, execution.standaloneInstanceID, outcome.TurnID, execution.runtimeEpoch); err != nil {
		m.logger.Warn("failed to acknowledge reattached turn outcome",
			zap.String("execution_id", execution.ID), zap.Int64("turn_id", outcome.TurnID), zap.Error(err))
	}
	return nil
}
