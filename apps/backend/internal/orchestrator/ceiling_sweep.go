package orchestrator

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ceilingSweepInterval is AC-17a's periodic retry interval, no longer than
// 60 seconds. It is a code constant rather than an env var, for the same
// reason idleReaperInterval is: this card's one env var (AC-18a) is the
// ceiling value itself, not its plumbing.
const ceilingSweepInterval = 20 * time.Second

// ceilingSweepCause records why a pass is running. A release-driven pass retries
// every deferred launch immediately, because capacity it can see freeing up must
// not wait on retry pacing; a periodic pass is the backstop and applies it.
type ceilingSweepCause int

const (
	ceilingSweepPeriodic ceilingSweepCause = iota
	ceilingSweepSignal
)

type ceilingSweepCauseContextKey struct{}

func withCeilingSweepCause(ctx context.Context, cause ceilingSweepCause) context.Context {
	return context.WithValue(ctx, ceilingSweepCauseContextKey{}, cause)
}

// isPeriodicCeilingSweep reports whether ctx carries the periodic cause. A
// context with no cause — a direct drain from a caller that is not the sweeper —
// is not paced.
func isPeriodicCeilingSweep(ctx context.Context) bool {
	cause, ok := ctx.Value(ceilingSweepCauseContextKey{}).(ceilingSweepCause)
	return ok && cause == ceilingSweepPeriodic
}

// ceilingSweeper owns the AC-50 background goroutine: the AC-17a periodic
// sweep and AC-15's retry-on-release driver are one component, modelled on
// idleSessionReaper ("a single owner of one background goroutine on
// Service"). All methods are nil-safe: a Service with sweeper == nil treats
// the sweep as off, which is what keeps existing tests unchanged.
type ceilingSweeper struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	workers  sync.WaitGroup
	started  bool
	interval time.Duration
	// signal carries release-driven wake-ups (AC-15). Buffered to exactly
	// one: a signal received while a pass is already running or already
	// pending coalesces into that one further pass rather than queuing
	// another (AC-15e).
	signal chan struct{}
}

func newCeilingSweeper() *ceilingSweeper {
	return &ceilingSweeper{
		interval: ceilingSweepInterval,
		signal:   make(chan struct{}, 1),
	}
}

// start launches the sweeper loop with the given tick callback. Idempotent:
// a second call on a running sweeper is a no-op.
func (r *ceilingSweeper) start(parent context.Context, tick func(ctx context.Context, cause ceilingSweepCause)) bool {
	if r == nil || tick == nil {
		return false
	}
	if parent == nil {
		parent = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return false
	}
	loopCtx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	r.started = true
	r.workers.Add(1)
	go func() {
		defer r.workers.Done()
		r.runLoop(loopCtx, tick)
	}()
	return true
}

// stop signals the sweeper to exit and waits for it. Safe to call on a
// never-started / already-stopped sweeper.
func (r *ceilingSweeper) stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Unlock()
	r.workers.Wait()
	r.mu.Lock()
	r.cancel = nil
	r.started = false
	r.mu.Unlock()
}

// signalNow requests a drain pass promptly (AC-15), coalescing with any pass
// already queued or currently running into exactly one further pass.
func (r *ceilingSweeper) signalNow() {
	if r == nil {
		return
	}
	select {
	case r.signal <- struct{}{}:
	default:
	}
}

// runLoop selects on the ticker, the signal channel and the loop context.
// The tick runs synchronously in this one goroutine, so two ticks — whether
// both timer-driven, both signal-driven, or one of each — can never overlap
// (AC-50a).
func (r *ceilingSweeper) runLoop(ctx context.Context, tick func(ctx context.Context, cause ceilingSweepCause)) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick(ctx, ceilingSweepPeriodic)
		case <-r.signal:
			tick(ctx, ceilingSweepSignal)
		}
	}
}

// Service-level hooks, mirroring idleSessionReaper's.

// startCeilingSweeper wires the sweeper into Service.Start.
func (s *Service) startCeilingSweeper(ctx context.Context) {
	if s.ceilingSweeper == nil {
		return
	}
	if !s.ceilingSweeper.start(ctx, s.ceilingSweepTick) {
		return
	}
	s.logger.Info("session ceiling sweeper started",
		zap.Duration("interval", s.ceilingSweeper.interval))
}

// stopCeilingSweeper joins the sweeper. Must be called before Service.Stop
// returns to honour the goroutine-ownership invariant.
func (s *Service) stopCeilingSweeper() {
	if s.ceilingSweeper == nil {
		return
	}
	s.ceilingSweeper.stop()
	s.logger.Info("session ceiling sweeper stopped")
}

// signalCeilingSweep requests a prompt drain pass (AC-15). Nil-safe: most
// unit tests build a Service with no sweeper, and a release signalled before
// Start (or after Stop) simply has nothing listening, which is harmless
// because nothing is waiting on it either.
func (s *Service) signalCeilingSweep() {
	if s.ceilingSweeper == nil {
		return
	}
	s.ceilingSweeper.signalNow()
}

// ceilingSweepTick is the AC-50 tick body. It is both AC-7's periodic
// backstop and AC-15/AC-17a's retry driver, because AC-50 states they are
// one component sharing one goroutine.
func (s *Service) ceilingSweepTick(ctx context.Context, cause ceilingSweepCause) {
	if s.sessionCeiling != nil {
		s.sessionCeiling.expireStaleReservations()
	}
	s.drainDeferredCeilingLaunches(withCeilingSweepCause(ctx, cause))
	// Reuse this pass as the runtime owner of the orphaned-starting-route
	// reconcile: durable statuses that never became a pending wait have no
	// timer of their own, so a route stranded after startup otherwise stays
	// "starting" until the next process restart. The reconcile is a no-op
	// while dynamic routing is disabled.
	s.reconcileOrphanedDynamicStartingRoutes(ctx)
}
