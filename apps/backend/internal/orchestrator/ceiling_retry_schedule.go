package orchestrator

import (
	"sync"
	"time"
)

// ceilingRetryBaseInterval is the wait before the first retry of a launch the
// session ceiling refused, doubling up to ceilingRetryMaxInterval. It is two
// sweep intervals, not one: the periodic pass runs exactly every
// ceilingSweepInterval, so a one-interval wait would come due on the very next
// tick and pace nothing. A wait equal to the tick is only reached from a pass
// that is not the ticker — a release-driven pass retries unconditionally
// instead, so it never consults this schedule at all.
const (
	ceilingRetryBaseInterval = 2 * ceilingSweepInterval
	ceilingRetryMaxInterval  = 5*time.Minute - ceilingSweepInterval
)

// deferredRetrySchedule paces the periodic ceiling sweep. While the ceiling is
// saturated every deferred launch is refused again on every tick, and each
// refusal costs the same admission, route and record round trips whether or not
// anything changed. Spacing the attempts out keeps a long saturation from
// repeating that work every ceilingSweepInterval forever, while a released
// reservation still retries immediately: capacity that actually freed up does
// not wait for a backoff.
//
// The schedule is process-local retry pacing, not durable state. A restart
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
	// while it waited is a different launch and starts from the base interval.
	identity    string
	refusals    int
	nextAttempt time.Time
	failed      bool
}

func newDeferredRetrySchedule() *deferredRetrySchedule {
	return &deferredRetrySchedule{entries: map[string]*deferredRetryEntry{}, now: time.Now}
}

// beginAttempt reports whether the periodic sweep may attempt taskID's record
// now, and reserves the following slot when it may. Counting the attempt when it
// is granted rather than when it is refused keeps the call a single decision
// point; settle discards the count for a record that stopped waiting.
func (s *deferredRetrySchedule) beginAttempt(taskID string) bool {
	if s == nil || taskID == "" {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	entry, tracked := s.entries[taskID]
	if !tracked {
		entry = &deferredRetryEntry{}
		s.entries[taskID] = entry
	}
	// refusals == 0 means no attempt has been counted yet, so the record is due
	// however far the clock has moved since it was first tracked.
	if entry.refusals > 0 && now.Before(entry.nextAttempt) {
		return false
	}
	entry.refusals++
	entry.failed = false
	entry.nextAttempt = now.Add(s.delay(entry.refusals))
	return true
}

// observe re-binds a tracked entry to the record the sweep actually read. A
// record whose identity changed is a different launch, so it is retried on the
// next tick instead of inheriting the previous launch's backoff.
func (s *deferredRetrySchedule) observe(taskID, identity string) {
	if s == nil || taskID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, tracked := s.entries[taskID]
	if !tracked {
		return
	}
	if entry.identity == "" {
		// The first pass that reads a record binds it. Binding is not a
		// replacement, so it must not clear a wait this pass already counted.
		entry.identity = identity
		return
	}
	if entry.identity != identity {
		entry.identity = identity
		entry.refusals = 0
		entry.failed = false
		entry.nextAttempt = s.now()
	}
}

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

func (s *deferredRetrySchedule) failureWaiting(taskID, identity string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[taskID]
	return entry != nil && entry.identity == identity && entry.failed && s.now().Before(entry.nextAttempt)
}

// settle forgets a task that is no longer waiting, so the next launch it defers
// starts from the base interval instead of inheriting a stale backoff.
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

// delay is the wait after the refusals-th consecutive attempt: the base interval
// doubling per refusal, capped so a long saturation still retries on a bounded
// cadence rather than drifting into never.
func (s *deferredRetrySchedule) delay(refusals int) time.Duration {
	wait := ceilingRetryBaseInterval
	for attempt := 1; attempt < refusals; attempt++ {
		wait *= 2
		if wait >= ceilingRetryMaxInterval {
			return ceilingRetryMaxInterval
		}
	}
	return wait
}
