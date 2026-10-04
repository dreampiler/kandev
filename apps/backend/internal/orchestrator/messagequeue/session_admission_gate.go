package messagequeue

import (
	"context"
	"errors"
	"sync"
	"time"
)

// sessionAdmissionGate is the per-session queue admission lock. Acquisition is
// bounded (see admissionWaitTimeout) so a queue operation never waits on another
// admitted callback without a limit, and waiters are served first-in-first-out so
// a retry loop cannot be starved by new arrivals.
type sessionAdmissionGate struct {
	stateMu sync.Mutex
	locked  bool
	waiters []*sessionAdmissionWaiter
}

type sessionAdmissionWaiter struct {
	ready   chan struct{}
	granted bool
}

func newSessionAdmissionGate() *sessionAdmissionGate {
	return &sessionAdmissionGate{}
}

func (g *sessionAdmissionGate) lock(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	g.stateMu.Lock()
	if !g.locked && len(g.waiters) == 0 {
		g.locked = true
		g.stateMu.Unlock()
		return g.recheckAfterGrant(ctx)
	}
	waiter := &sessionAdmissionWaiter{ready: make(chan struct{})}
	g.waiters = append(g.waiters, waiter)
	g.stateMu.Unlock()

	select {
	case <-waiter.ready:
		return g.recheckAfterGrant(ctx)
	case <-ctx.Done():
		g.stateMu.Lock()
		if waiter.granted {
			g.handoffLocked()
		} else {
			g.removeWaiterLocked(waiter)
		}
		g.stateMu.Unlock()
		return ctx.Err()
	}
}

func (g *sessionAdmissionGate) recheckAfterGrant(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		g.unlock()
		return err
	}
	return nil
}

func (g *sessionAdmissionGate) unlock() {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	if !g.locked {
		return
	}
	g.handoffLocked()
}

func (g *sessionAdmissionGate) handoffLocked() {
	if len(g.waiters) == 0 {
		g.locked = false
		return
	}
	waiter := g.waiters[0]
	g.waiters[0] = nil
	g.waiters = g.waiters[1:]
	waiter.granted = true
	close(waiter.ready)
}

func (g *sessionAdmissionGate) removeWaiterLocked(target *sessionAdmissionWaiter) {
	for index, waiter := range g.waiters {
		if waiter != target {
			continue
		}
		copy(g.waiters[index:], g.waiters[index+1:])
		g.waiters[len(g.waiters)-1] = nil
		g.waiters = g.waiters[:len(g.waiters)-1]
		return
	}
}

// ErrSessionAdmissionTimeout reports that a queue operation could not acquire a
// session's admission within its budget because another admitted callback never
// released it. It is a distinct sentinel so operator-facing queue actions can
// return a retryable result instead of an opaque internal error.
var ErrSessionAdmissionTimeout = errors.New("queue session admission timed out")

// admissionWaitTimeout bounds how long one queue operation waits to acquire a
// session's admission. WebSocket-dispatched queue actions run under the hub's
// lifetime context, which carries no deadline, so honoring the caller's context
// alone leaves an operator action waiting forever behind an admitted callback
// that never returns. Queue admissions only cover queue state transitions, so
// exceeding this budget means another holder is stuck and the operation reports
// that instead of hanging.
var admissionWaitTimeout = 30 * time.Second

// admissionWaitBudget derives the acquisition budget for one admission and
// reports whether this operation supplied the budget. An earlier caller
// deadline still wins; otherwise the wait is bounded by admissionWaitTimeout.
func admissionWaitBudget(ctx context.Context) (context.Context, context.CancelFunc, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= admissionWaitTimeout {
		derived, cancel := context.WithCancel(ctx)
		return derived, cancel, false
	}
	waitCtx, cancel := context.WithTimeout(ctx, admissionWaitTimeout)
	return waitCtx, cancel, true
}

// admissionWaitError reports ErrSessionAdmissionTimeout when the acquisition ran
// out of the budget this operation supplied, and the caller's own error when the
// caller cancelled or ran out of its own deadline first.
func admissionWaitError(err error, ownBudget bool) error {
	if err == nil {
		return nil
	}
	if ownBudget && errors.Is(err, context.DeadlineExceeded) {
		return ErrSessionAdmissionTimeout
	}
	return err
}

// lifecycleReadGateRetryInterval bounds how long a task-wide purge barrier makes
// a session admission wait before it re-checks its own context.
const lifecycleReadGateRetryInterval = 20 * time.Millisecond

// rlockWithContext acquires the task-wide lifecycle barrier for reading. A task
// purge holds the matching write lock across the repository purge, and a queue
// admission must observe that barrier; waiting is therefore retryable but never
// unbounded, so an abandoned request stops occupying a goroutine.
func rlockWithContext(ctx context.Context, mu *sync.RWMutex) error {
	for {
		if mu.TryRLock() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		timer := time.NewTimer(lifecycleReadGateRetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
