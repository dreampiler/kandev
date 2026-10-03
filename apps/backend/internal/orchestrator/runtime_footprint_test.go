package orchestrator

import (
	"context"
	"testing"
	"time"

	agentruntime "github.com/kandev/kandev/internal/agentruntime"
	orchexec "github.com/kandev/kandev/internal/orchestrator/executor"
)

// footprintObserverStub reports a fixed snapshot, so the projection's fail-closed
// behaviour can be asserted without a runtime tier behind it.
type footprintObserverStub struct {
	orchexec.AgentManagerClient
	snapshot agentruntime.RuntimeFootprintSnapshot
	calls    int
}

func (s *footprintObserverStub) SnapshotRuntimeFootprint(context.Context) agentruntime.RuntimeFootprintSnapshot {
	s.calls++
	return s.snapshot
}

// TestObserveRuntimeFootprintOnceProjectsPerSession pins that one observation
// becomes a bounded per-session projection carrying the session's own identity,
// process count, and bytes. An installation total alone is not what an operator
// can act on.
func TestObserveRuntimeFootprintOnceProjectsPerSession(t *testing.T) {
	observed := time.Now().UTC().Add(-time.Minute)
	stub := &footprintObserverStub{snapshot: agentruntime.RuntimeFootprintSnapshot{
		LiveRuntimes:   2,
		ProcessCount:   7,
		CommittedBytes: 1500,
		ResidentBytes:  900,
		Complete:       true,
		Runtimes: []agentruntime.SessionRuntimeFootprint{
			{
				SessionID: "session-1", TaskID: "task-1", ExecutionID: "exec-1",
				Runtime: string(agentruntime.RuntimeStandalone), Status: "running",
				Processes: 4, CommittedBytes: 1000, ResidentBytes: 600, LastActivityAt: observed,
			},
			{
				SessionID: "session-2", TaskID: "task-2", ExecutionID: "exec-2",
				Runtime: string(agentruntime.RuntimeStandalone), Status: "ready",
				Processes: 3, CommittedBytes: 500, ResidentBytes: 300,
			},
		},
	}}

	svc := &Service{agentManager: stub, logger: testLogger()}
	svc.observeRuntimeFootprintOnce(context.Background())

	projection := svc.RuntimeFootprint()
	if !projection.Complete {
		t.Fatal("a complete observation must project as complete")
	}
	if projection.LiveRuntimes != 2 || projection.Processes != 7 {
		t.Fatalf("totals = %d runtimes / %d processes, want 2 / 7", projection.LiveRuntimes, projection.Processes)
	}
	if projection.CommittedBytes != 1500 || projection.ResidentBytes != 900 {
		t.Fatalf("bytes = %d/%d, want 1500/900", projection.CommittedBytes, projection.ResidentBytes)
	}
	if len(projection.Runtimes) != 2 {
		t.Fatalf("len(Runtimes) = %d, want one row per live runtime", len(projection.Runtimes))
	}
	if projection.ObservedAt.IsZero() {
		t.Fatal("ObservedAt must be set so a reader can tell a stale projection from a current one")
	}

	first := projection.Runtimes[0]
	if first.SessionID != "session-1" || first.TaskID != "task-1" || first.ExecutionID != "exec-1" {
		t.Fatalf("row identity = %+v, want the session's own identity", first)
	}
	if first.Processes != 4 || first.CommittedBytes != 1000 || first.ResidentBytes != 600 {
		t.Fatalf("row measurement = %+v, want the session's own bytes", first)
	}
	if !first.LastActivityAt.Equal(observed) {
		t.Fatalf("row last activity = %v, want %v", first.LastActivityAt, observed)
	}
}

// TestObserveRuntimeFootprintOnceReplacesTotalsOnFailedRead pins the fail-closed
// direction at the projection boundary. A failed read must not leave the previous
// reading's totals readable as if they were still current, which is exactly how a
// stale total gets trusted during an incident.
func TestObserveRuntimeFootprintOnceReplacesTotalsOnFailedRead(t *testing.T) {
	stub := &footprintObserverStub{snapshot: agentruntime.RuntimeFootprintSnapshot{
		LiveRuntimes: 3, ProcessCount: 9, CommittedBytes: 3000, ResidentBytes: 2000,
		Complete: true,
		Runtimes: []agentruntime.SessionRuntimeFootprint{
			{SessionID: "session-1", Processes: 9, CommittedBytes: 3000},
		},
	}}

	svc := &Service{agentManager: stub, logger: testLogger()}
	svc.observeRuntimeFootprintOnce(context.Background())
	if !svc.RuntimeFootprint().Complete {
		t.Fatal("first observation should project as complete")
	}

	stub.snapshot = agentruntime.RuntimeFootprintSnapshot{Complete: false}
	svc.observeRuntimeFootprintOnce(context.Background())

	projection := svc.RuntimeFootprint()
	if projection.Complete {
		t.Fatal("a failed read must not project as complete")
	}
	if projection.LiveRuntimes != 0 || projection.Processes != 0 {
		t.Fatalf("failed read kept totals: %d runtimes / %d processes", projection.LiveRuntimes, projection.Processes)
	}
	if projection.CommittedBytes != 0 || projection.ResidentBytes != 0 {
		t.Fatalf("failed read kept bytes: %d/%d", projection.CommittedBytes, projection.ResidentBytes)
	}
	if len(projection.Runtimes) != 0 {
		t.Fatalf("failed read kept %d rows, want none", len(projection.Runtimes))
	}
}

// TestObserveRuntimeFootprintOnceWithNoObserver pins that a runtime tier without
// the capability leaves the projection untouched rather than publishing an empty
// reading that looks like "this installation holds no agent memory".
func TestObserveRuntimeFootprintOnceWithNoObserver(t *testing.T) {
	svc := &Service{agentManager: nil, logger: testLogger()}
	svc.observeRuntimeFootprintOnce(context.Background())

	if svc.RuntimeFootprint().ObservedAt != (time.Time{}) {
		t.Fatal("a runtime tier with no observer must not publish a projection")
	}
}

// TestRuntimeFootprintReturnsACopy pins that a reader cannot mutate the
// projection the maintenance tick owns.
func TestRuntimeFootprintReturnsACopy(t *testing.T) {
	svc := &Service{agentManager: &footprintObserverStub{snapshot: agentruntime.RuntimeFootprintSnapshot{
		LiveRuntimes: 1, Complete: true,
		Runtimes: []agentruntime.SessionRuntimeFootprint{{SessionID: "session-1", Processes: 2}},
	}}, logger: testLogger()}
	svc.observeRuntimeFootprintOnce(context.Background())

	first := svc.RuntimeFootprint()
	first.Runtimes[0].SessionID = "mutated"
	first.LiveRuntimes = 999

	second := svc.RuntimeFootprint()
	if second.Runtimes[0].SessionID != "session-1" || second.LiveRuntimes != 1 {
		t.Fatalf("projection was mutated through the returned value: %+v", second)
	}
}
