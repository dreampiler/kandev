package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	"github.com/stretchr/testify/require"
)

// retryStreamAdmission returns nil exactly when the admission succeeded, and a
// non-nil error otherwise; streamEventProcessor.run calls stop() only on the
// non-nil result, so these cases pin the no-stop / stop decision directly.
func TestRetryStreamAdmission(t *testing.T) {
	noSleep := func(time.Duration) bool { return true }
	noLog := func(int) {}

	t.Run("transient deadline then success retries in place without stopping", func(t *testing.T) {
		attempts := 0
		err := retryStreamAdmission(t.Context(), noSleep, noLog, func() error {
			attempts++
			if attempts < 3 {
				return context.DeadlineExceeded
			}
			return nil
		})
		require.NoError(t, err, "a retryable admission failure must not surface as a stop")
		require.Equal(t, 3, attempts)
	})

	t.Run("bound exhaustion returns the deadline to the existing stop path", func(t *testing.T) {
		attempts := 0
		err := retryStreamAdmission(t.Context(), noSleep, noLog, func() error {
			attempts++
			return context.DeadlineExceeded
		})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, streamAdmissionRetryAttempts, attempts)
	})

	t.Run("permanent error fails closed without retrying", func(t *testing.T) {
		attempts := 0
		permanent := errors.New("durable delivery identity mismatch")
		err := retryStreamAdmission(t.Context(), noSleep, noLog, func() error {
			attempts++
			return permanent
		})
		require.ErrorIs(t, err, permanent)
		require.Equal(t, 1, attempts)
	})

	t.Run("cancelled processor context does not retry", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		attempts := 0
		err := retryStreamAdmission(ctx, noSleep, noLog, func() error {
			attempts++
			return context.DeadlineExceeded
		})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, 1, attempts)
	})

	t.Run("cancellation during the backoff does not reach another attempt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		attempts := 0
		err := retryStreamAdmission(ctx, func(time.Duration) bool {
			cancel()
			return true
		}, noLog, func() error {
			attempts++
			return context.DeadlineExceeded
		})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, 1, attempts, "the context is re-checked after the pause")
	})

	t.Run("stop signal aborts the retry", func(t *testing.T) {
		attempts := 0
		err := retryStreamAdmission(t.Context(), func(time.Duration) bool { return false }, noLog, func() error {
			attempts++
			return context.DeadlineExceeded
		})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, 1, attempts, "a stop signal must abort before another attempt")
	})
}

// TestSleepOrStopContextAbortsOnCancel pins the delta-1 requirement: a
// processor or parent cancellation during a retry backoff must end the wait
// immediately, not after the full interval, so the admission retry does not run
// once more against a stopped transport.
func TestSleepOrStopContextAbortsOnCancel(t *testing.T) {
	sm := NewStreamManager(newTestLogger(), StreamCallbacks{}, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.False(t, sm.sleepOrStopContext(ctx, time.Second))
}

// TestPrepareAgentEventDoesNotRetryNegotiation pins the delta-2 scope: the
// retry wraps only the durable admission, so a negotiation deadline returns
// without reaching the admission closure or another backoff.
func TestPrepareAgentEventDoesNotRetryNegotiation(t *testing.T) {
	sm := NewStreamManager(newTestLogger(), StreamCallbacks{}, nil, nil)
	execution := &AgentExecution{}
	gate := execution.beginDeliveryNegotiation()
	gate.finish(context.DeadlineExceeded)

	sleeps := 0
	_, err := sm.prepareAgentEventWithRetry(
		t.Context(), execution, nil, nil, agentctl.AgentEvent{},
		streamAdmissionRetry{
			ctx:      t.Context(),
			sleep:    func(time.Duration) bool { sleeps++; return true },
			logRetry: func(int) {},
		},
	)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Zero(t, sleeps, "a negotiation deadline must not be retried")
}
