package dashboard

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/office/repository/sqlite"
	"github.com/kandev/kandev/internal/system/sessioncapacity"
)

type countingCapacitySource struct {
	observation sessioncapacity.Observation
	calls       atomic.Int32
	started     chan struct{}
	release     chan struct{}
	once        sync.Once
}

func (s *countingCapacitySource) SessionCapacityObservation(context.Context) sessioncapacity.Observation {
	s.calls.Add(1)
	if s.started != nil {
		s.once.Do(func() { close(s.started) })
	}
	if s.release != nil {
		<-s.release
	}
	return s.observation
}

func TestControlCapacityCacheReusesObservationWithinTTL(t *testing.T) {
	source := &countingCapacitySource{observation: sessioncapacity.Observation{
		Waiting: sessioncapacity.WaitingObservation{Known: true, Counts: map[string]int{
			sessioncapacity.WaitingReasonGradeLimit: 2,
			sessioncapacity.WaitingReasonBothLimits: 1,
			sessioncapacity.WaitingReasonTotalLimit: 1,
		}},
	}}
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	cache := &controlCapacityCache{source: source, ttl: 2 * time.Second, now: func() time.Time { return now }}

	first := cache.waiting(context.Background())
	second := cache.waiting(context.Background())
	if source.calls.Load() != 1 {
		t.Fatalf("observations = %d, want 1 inside the TTL", source.calls.Load())
	}
	if first != second {
		t.Fatal("a cached read must return the same snapshot")
	}
	if first.Grade != 3 || first.Total != 4 {
		t.Fatalf("cached waiting = %#v, want grade 3 total 4", first)
	}

	now = now.Add(2 * time.Second)
	third := cache.waiting(context.Background())
	if source.calls.Load() != 2 {
		t.Fatalf("observations = %d, want 2 after the TTL", source.calls.Load())
	}
	if third == nil || !third.Known {
		t.Fatalf("refreshed waiting = %#v, want known", third)
	}
}

func TestControlCapacityCacheKeepsUnknownForTheTTL(t *testing.T) {
	source := &countingCapacitySource{observation: sessioncapacity.Observation{
		Waiting: sessioncapacity.WaitingObservation{Known: false},
	}}
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	cache := &controlCapacityCache{source: source, ttl: time.Minute, now: func() time.Time { return now }}

	first := cache.waiting(context.Background())
	second := cache.waiting(context.Background())
	if source.calls.Load() != 1 {
		t.Fatalf("observations = %d, want 1", source.calls.Load())
	}
	if first.Known || second.Known {
		t.Fatalf("unknown waiting = %#v / %#v, want unknown", first, second)
	}
}

func TestControlCapacityCacheJoinsConcurrentDashboardReads(t *testing.T) {
	source := &countingCapacitySource{
		observation: sessioncapacity.Observation{
			Waiting: sessioncapacity.WaitingObservation{Known: true, Counts: map[string]int{
				sessioncapacity.WaitingReasonGradeLimit: 1,
			}},
		},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	cache := &controlCapacityCache{source: source, ttl: time.Minute, now: time.Now}

	var wg sync.WaitGroup
	results := make([]*sqlite.ControlWaiting, 5)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = cache.waiting(context.Background())
		}(i)
	}
	<-source.started
	close(source.release)
	wg.Wait()

	if source.calls.Load() != 1 {
		t.Fatalf("observations = %d, want one joined read", source.calls.Load())
	}
	for _, result := range results {
		if result == nil || !result.Known || result.Grade != 1 {
			t.Fatalf("joined waiting = %#v, want the same known snapshot", result)
		}
	}
}
