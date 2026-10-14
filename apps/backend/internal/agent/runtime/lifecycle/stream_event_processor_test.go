package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

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
