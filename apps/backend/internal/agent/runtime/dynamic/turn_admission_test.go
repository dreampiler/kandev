package dynamic

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type mutableAdmissionUsage struct {
	fixedUsage
	full bool
}

type blockedAdmissionUsage struct {
	fixedUsage
	entered chan struct{}
	release chan struct{}
	block   bool
}

func (u *blockedAdmissionUsage) CheckAdmission(ctx context.Context, _ Candidate, _ string, _ time.Time) (PaceScore, error) {
	if !u.block {
		return PaceScore{}, nil
	}
	close(u.entered)
	select {
	case <-u.release:
		return PaceScore{}, nil
	case <-ctx.Done():
		return PaceScore{}, ctx.Err()
	}
}

func TestProviderTierTurnAdmissionRechecksGenerationWithoutHoldingStateLock(t *testing.T) {
	u := &blockedAdmissionUsage{entered: make(chan struct{}), release: make(chan struct{})}
	e := NewEngine(WithUsageSnapshotProvider(u))
	profile := Profile{ID: "dynamic", Candidates: []Candidate{{ID: "first", Enabled: true}}}
	decision, err := e.Select("session", profile, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	u.block = true
	done := make(chan error, 1)
	claimed := false
	go func() {
		done <- e.WithTurnAdmission(context.Background(), profile, decision, func() error { claimed = true; return nil })
	}()
	<-u.entered
	replaced := make(chan struct{})
	go func() {
		e.mu.Lock()
		state := e.states[decision.SessionID]
		state.Generation++
		e.states[decision.SessionID] = state
		e.mu.Unlock()
		close(replaced)
	}()
	select {
	case <-replaced:
	case <-time.After(time.Second):
		close(u.release)
		<-done
		<-replaced
		t.Fatal("admission I/O held the engine state lock")
	}
	close(u.release)
	if err := <-done; !errors.Is(err, ErrStaleGeneration) || claimed {
		t.Fatalf("stale admission = %v, claimed = %v", err, claimed)
	}
}

func TestProviderTierExhaustionSkipsWithoutReordering(t *testing.T) {
	engine := NewEngine(WithUsageSnapshotProvider(fixedUsage{
		"first": {AdmissionReason: "limit_daily", AdmissionRetryAt: time.Now().Add(time.Hour)},
	}))
	profile := Profile{ID: "dynamic", Candidates: []Candidate{
		{ID: "first", Enabled: true}, {ID: "second", Enabled: true}, {ID: "third", Enabled: true},
	}}
	decision, err := engine.Select("session", profile, 0, "")
	if err != nil || decision.ExecutionProfileID != "second" {
		t.Fatalf("selection = %+v, %v; want the first remaining configured candidate", decision, err)
	}
}

func (u *mutableAdmissionUsage) CheckAdmission(_ context.Context, _ Candidate, _ string, now time.Time) (PaceScore, error) {
	if u.full {
		return PaceScore{AdmissionReason: "limit_minute", AdmissionRetryAt: now.Add(time.Minute)}, nil
	}
	return PaceScore{}, nil
}

func TestProviderTierTurnAdmissionPreservesTheIncumbentOnDeferral(t *testing.T) {
	usage := &mutableAdmissionUsage{}
	engine := NewEngine(WithUsageSnapshotProvider(usage))
	profile := Profile{ID: "dynamic", Candidates: []Candidate{{ID: "incumbent", Enabled: true}}}
	decision, err := engine.Select("session", profile, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	usage.full = true
	claimed := false
	err = engine.WithTurnAdmission(context.Background(), profile, decision, func() error {
		claimed = true
		return nil
	})
	var wait *NoEligibleCandidateError
	if !errors.As(err, &wait) || !wait.ResourceWait || claimed {
		t.Fatalf("exhausted final claim = %v, dispatched = %v", err, claimed)
	}
	state, ok := engine.State("session")
	if !ok || state.ExecutionProfileID != decision.ExecutionProfileID || state.Generation != decision.Generation || state.Status != "waiting" {
		t.Fatalf("deferral changed incumbent attribution: %+v", state)
	}
	usage.full = false
	if err := engine.WithTurnAdmission(context.Background(), profile, decision, func() error { claimed = true; return nil }); err != nil || !claimed {
		t.Fatalf("available final claim = %v, dispatched = %v", err, claimed)
	}
}

func TestProviderTierTurnAdmissionSerializesTheLastSlot(t *testing.T) {
	usage := &mutableAdmissionUsage{}
	engine := NewEngine(WithUsageSnapshotProvider(usage))
	profile := Profile{ID: "dynamic", Candidates: []Candidate{{ID: "shared", Enabled: true}}}
	decisions := make([]RouteDecision, 2)
	for i, session := range []string{"session-a", "session-b"} {
		var err error
		decisions[i], err = engine.Select(session, profile, 0, "")
		if err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, decision := range decisions {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- engine.WithTurnAdmission(context.Background(), profile, decision, func() error {
				usage.full = true
				return nil
			})
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	admitted, deferred := 0, 0
	for err := range results {
		var wait *NoEligibleCandidateError
		switch {
		case err == nil:
			admitted++
		case errors.As(err, &wait) && wait.ResourceWait:
			deferred++
		default:
			t.Fatalf("unexpected claim error: %v", err)
		}
	}
	if admitted != 1 || deferred != 1 {
		t.Fatalf("last-slot results = %d admitted, %d deferred", admitted, deferred)
	}
}
