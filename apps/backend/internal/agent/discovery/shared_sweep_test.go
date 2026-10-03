package discovery

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/agents"
	"github.com/kandev/kandev/internal/agent/registry"
)

// Every caller that misses the cache at once must share one sweep: each sweep
// runs every agent's installation check, and overlapping sweeps are what slow
// a check past its bound.
// @covers AC-AGENTS-INSTALL-DETECTION-002.5
func TestRegistryDetectSharesConcurrentSweep(t *testing.T) {
	blocking := &blockingDiscoveryAgent{
		discoveryTestAgent: &discoveryTestAgent{
			id:        "agent-a",
			discovery: &agents.DiscoveryResult{Available: true},
		},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	discoveryRegistry := newSingleAgentRegistry(t, blocking, time.Minute)

	const callers = 3
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := discoveryRegistry.Detect(context.Background())
			if err != nil {
				t.Errorf("detect: %v", err)
				return
			}
			if len(results) != 1 || !results[0].Available {
				t.Errorf("results = %+v, want agent-a available", results)
			}
		}()
	}

	select {
	case <-blocking.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("sweep never reached IsInstalled")
	}
	// Give the other callers time to join the in-flight sweep.
	time.Sleep(50 * time.Millisecond)
	close(blocking.release)
	wg.Wait()

	if got := blocking.calls(); got != 1 {
		t.Errorf("IsInstalled calls = %d, want 1 shared sweep", got)
	}
}

// @covers AC-AGENTS-INSTALL-DETECTION-002.5
func TestRegistryDetectReturnsCallerCancellationWithoutCancelingSweep(t *testing.T) {
	blocking := &blockingDiscoveryAgent{
		discoveryTestAgent: &discoveryTestAgent{
			id:        "agent-a",
			discovery: &agents.DiscoveryResult{Available: true},
		},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	discoveryRegistry := newSingleAgentRegistry(t, blocking, time.Minute)
	swept := make(chan []Availability, 1)
	discoveryRegistry.OnSweep(func(results []Availability) { swept <- results })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := discoveryRegistry.Detect(ctx)
		done <- err
	}()
	select {
	case <-blocking.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("sweep never reached IsInstalled")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("detect error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("detect did not return after its caller canceled")
	}

	close(blocking.release)
	select {
	case results := <-swept:
		if len(results) != 1 || !results[0].Available {
			t.Fatalf("published results = %+v, want agent-a available", results)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the shared sweep did not finish after its first caller left")
	}
	if got := blocking.calls(); got != 1 {
		t.Errorf("IsInstalled calls = %d, want 1", got)
	}
}

func TestRegistryOnSweepRunsForFreshSweepsOnly(t *testing.T) {
	agent := &discoveryTestAgent{
		id:        "agent-a",
		discovery: &agents.DiscoveryResult{Available: true},
	}
	discoveryRegistry := newSingleAgentRegistry(t, agent, time.Minute)
	var sweeps atomic.Int32
	discoveryRegistry.OnSweep(func(results []Availability) {
		sweeps.Add(1)
		if len(results) != 1 || results[0].Name != "agent-a" {
			t.Errorf("listener results = %+v, want agent-a", results)
		}
	})

	detectNames(t, discoveryRegistry)
	detectNames(t, discoveryRegistry)
	if got := sweeps.Load(); got != 1 {
		t.Fatalf("listener calls after a cache hit = %d, want 1", got)
	}

	discoveryRegistry.InvalidateCache()
	detectNames(t, discoveryRegistry)
	if got := sweeps.Load(); got != 2 {
		t.Fatalf("listener calls after invalidation = %d, want 2", got)
	}
}

// @covers AC-AGENTS-INSTALL-DETECTION-002.1
func TestRegistryAgentAvailability(t *testing.T) {
	log := newDiscoveryTestLogger(t)
	reg := registry.NewRegistry(log)
	for _, ag := range []agents.Agent{
		&discoveryTestAgent{id: "agent-installed", discovery: &agents.DiscoveryResult{Available: true}},
		&discoveryTestAgent{id: "agent-missing", discovery: &agents.DiscoveryResult{Available: false}},
		&virtualTestAgent{discoveryTestAgent: &discoveryTestAgent{
			id:        "agent-virtual",
			discovery: &agents.DiscoveryResult{Available: true},
		}},
	} {
		if err := reg.Register(ag); err != nil {
			t.Fatalf("register %s: %v", ag.ID(), err)
		}
	}
	discoveryRegistry := loadDiscoveryRegistry(t, reg, log)

	tests := []struct {
		name          string
		wantAvailable bool
		wantKnown     bool
	}{
		{name: "agent-installed", wantAvailable: true, wantKnown: true},
		{name: "agent-missing", wantAvailable: false, wantKnown: true},
		{name: "agent-virtual", wantAvailable: false, wantKnown: false},
		{name: "agent-unregistered", wantAvailable: false, wantKnown: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			available, known, err := discoveryRegistry.AgentAvailability(context.Background(), tt.name)
			if err != nil {
				t.Fatalf("availability: %v", err)
			}
			if available != tt.wantAvailable || known != tt.wantKnown {
				t.Errorf("availability = (%v, %v), want (%v, %v)",
					available, known, tt.wantAvailable, tt.wantKnown)
			}
		})
	}
}
