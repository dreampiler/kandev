package lifecycle

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// stallPublishWaitLimit bounds how long waitForPromptDone waits for one
// agent.stalled publish to return. The in-memory bus delivers synchronously and
// the orchestrator's stall handler takes the session's cancel guard before it
// touches the database, so a handler that is stuck behind that guard would
// otherwise hold the wait loop forever: the five-minute advisory publish would
// never return, the terminal threshold would never be evaluated, and the
// prompt would keep its ceiling slot indefinitely. A handler that returns in
// time keeps the original order (terminal outcome recorded before the
// synthetic completion is injected); one that does not is left running and the
// loop proceeds.
const stallPublishWaitLimit = 30 * time.Second

// publishAgentStalledBounded publishes one agent.stalled signal from a separate
// goroutine and waits for it for at most stallPublishWaitLimit.
func (sm *SessionManager) publishAgentStalledBounded(
	ctx context.Context,
	execution *AgentExecution,
	promptGeneration uint64,
	lastActivity time.Time,
	elapsed time.Duration,
	activityEpoch uint64,
	neverStarted bool,
	prolongedStall bool,
) {
	if sm.eventPublisher == nil {
		return
	}
	publisher := sm.eventPublisher
	done := make(chan struct{})
	go func() {
		defer close(done)
		publisher.PublishAgentStalled(
			ctx,
			execution,
			promptGeneration,
			lastActivity,
			elapsed,
			activityEpoch,
			neverStarted,
			prolongedStall,
		)
	}()

	timer := time.NewTimer(stallPublishWaitLimit)
	defer timer.Stop()
	select {
	case <-done:
	case <-ctx.Done():
	case <-timer.C:
		sm.logger.Warn("agent stall publish did not return in time; continuing prompt wait",
			zap.String("execution_id", execution.ID),
			zap.Duration("wait_limit", stallPublishWaitLimit),
			zap.Bool("never_started", neverStarted),
			zap.Bool("prolonged_stall", prolongedStall))
	}
}
