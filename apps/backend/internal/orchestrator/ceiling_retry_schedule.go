package orchestrator

import (
	"sync"
	"time"
)

// ceilingRetryBaseInterval is the wait before a launch that failed for a
// non-capacity reason is retried. It is two sweep intervals, not one: the
// periodic pass runs exactly every ceilingSweepInterval, so a one-interval wait
// would come due on the very next tick and pace nothing.
const ceilingRetryBaseInterval = 2 * ceilingSweepInterval

// ceilingProgressStallInterval is how long a head may go without a replay
// attempt before it stops holding queue precedence. It is three sweep
// intervals: long enough to absorb a slow sweep or a transient stall, short
// enough that a stuck head yields within a minute instead of blocking the
// queue for twenty.
const ceilingProgressStallInterval = 3 * ceilingSweepInterval

// deferredRetrySchedule remembers, per deferred task, which durable record the
// sweep last saw and whether that record's last replay failed for a reason
// unrelated to capacity.
//
// It is deliberately not a refusal timer. A launch the ceiling refused is
// retried only when an admission input actually changes: the periodic sweep
// arms a lane once that lane has free capacity, and a release, a failed launch,
// or an applied capacity change signals a pass immediately. While a lane is
// saturated there is nothing to re-decide, so no retry is scheduled from the
// clock alone. The one time-based wait kept here is for a replay that failed
// for a non-capacity reason, so a broken launch is not hammered on every tick
// that sees free capacity.
//
// The schedule is process-local retry state, not durable state. A restart
// replays every deferred launch once, which is the correct thing to do anyway,
// and no persisted record depends on it.
type deferredRetrySchedule struct {
	mu      sync.Mutex
	entries map[string]*deferredRetryEntry
	// now is the clock, replaced in tests.
	now func() time.Time
}

type deferredRetryEntry struct {
	// identity is the record this entry describes. A record that was replaced
	// while it waited is a different launch and starts with no failure wait.
	identity    string
	nextAttempt time.Time
	failed      bool
	// lastAttempt is when the sweep last started a replay for this task.
	// A head whose last attempt is too old is not being retried — the
	// sweeper is blocked or stuck — and must not hold queue precedence.
	lastAttempt time.Time
}

func newDeferredRetrySchedule() *deferredRetrySchedule {
	return &deferredRetrySchedule{entries: map[string]*deferredRetryEntry{}, now: time.Now}
}

// observe re-binds a tracked entry to the record the sweep actually read. A
// record whose identity changed is a different launch, so a failure wait it
// inherited from its predecessor is cleared.
func (s *deferredRetrySchedule) observe(taskID, identity string) {
	if s == nil || taskID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, tracked := s.entries[taskID]
	if !tracked || entry.identity == identity {
		return
	}
	entry.identity = identity
	entry.failed = false
	entry.nextAttempt = time.Time{}
	// A replaced record is a launch that was never attempted. It must not
	// inherit its predecessor's attempt time, or it would hold precedence
	// for up to a stall interval without ever being retried itself.
	entry.lastAttempt = time.Time{}
}

// recordFailure starts a bounded wait before a launch that failed for a
// non-capacity reason is retried, so a permanently failing replay does not run
// on every pass while its lane has free capacity.
func (s *deferredRetrySchedule) recordFailure(taskID, identity string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[taskID]
	if entry == nil {
		entry = &deferredRetryEntry{}
		s.entries[taskID] = entry
	}
	entry.failed = true
	entry.identity = identity
	entry.nextAttempt = s.now().Add(ceilingRetryBaseInterval)
}

// failureWaiting reports whether a record's non-capacity failure wait is still
// in effect. A record whose identity changed since the failure is a different
// launch and is not held back.
func (s *deferredRetrySchedule) failureWaiting(taskID, identity string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[taskID]
	return entry != nil && entry.identity == identity && entry.failed && s.now().Before(entry.nextAttempt)
}

// failureRecorded reports whether a record's most recent replay failed for a
// non-capacity reason and the record has not been replaced or settled since.
// Unlike failureWaiting it does not expire with the retry backoff: a head whose
// replay keeps failing must keep yielding its free-slot precedence to later
// automatic launches until it actually starts or is replaced, so a permanently
// broken head (an un-attachable workspace, a launch error) cannot hold a free
// slot behind it while it never starts itself. A transient failure recovers as
// soon as the record's next replay succeeds (settle) or it is replaced
// (observe).
func (s *deferredRetrySchedule) failureRecorded(taskID, identity string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[taskID]
	return entry != nil && entry.identity == identity && entry.failed
}

// markAttempt records that the sweep acquired this task's record for a
// replay. It is called after the claim, not at tick entry: a wedged replay
// that never reaches the claim must keep a stale clock and yield precedence,
// while a head the sweep keeps dispatching stays fresh.
func (s *deferredRetrySchedule) markAttempt(taskID, identity string) {
	if s == nil || taskID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[taskID]
	if entry == nil {
		entry = &deferredRetryEntry{}
		s.entries[taskID] = entry
	}
	entry.identity = identity
	entry.lastAttempt = s.now()
}

// progressStalled reports whether the sweep has not started a replay for this
// task's current record within the stall interval. An entry the sweep never
// attempted yet is not stalled: the sweep may simply not have reached it. A
// replaced record starts fresh through observe, which clears lastAttempt.
func (s *deferredRetrySchedule) progressStalled(taskID, identity string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[taskID]
	if entry == nil || entry.identity != identity {
		return false
	}
	if entry.lastAttempt.IsZero() {
		return false
	}
	return s.now().Sub(entry.lastAttempt) > ceilingProgressStallInterval
}

// settle forgets a task that is no longer waiting, so a later launch it defers
// starts fresh instead of inheriting a stale failure wait.
func (s *deferredRetrySchedule) settle(taskID string) {
	if s == nil || taskID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, taskID)
}

// prune forgets every task absent from the deferred set the sweep just listed,
// so a dropped, launched or deleted task does not keep an entry alive.
func (s *deferredRetrySchedule) prune(deferred map[string]struct{}) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for taskID := range s.entries {
		if _, stillWaiting := deferred[taskID]; !stillWaiting {
			delete(s.entries, taskID)
		}
	}
}
