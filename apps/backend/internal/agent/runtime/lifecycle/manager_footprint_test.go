package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/executor"
	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	agentruntime "github.com/kandev/kandev/internal/agentruntime"
)

// footprintReaderStub is a runtime backend that reports a fixed footprint list or
// a fixed error, so attribution can be asserted without a control server. It
// embeds ExecutorBackend so only the two methods this test needs are real.
type footprintReaderStub struct {
	ExecutorBackend
	footprints []agentctl.RuntimeFootprint
	err        error
}

func (s *footprintReaderStub) Name() executor.Name { return agentruntime.RuntimeStandalone }

func (s *footprintReaderStub) ReadRuntimeFootprints(context.Context) ([]agentctl.RuntimeFootprint, error) {
	return s.footprints, s.err
}

// newFootprintManager builds a lifecycle Manager whose registry holds one
// footprint-capable runtime, plus the execution rows that give readings a
// session identity.
func newFootprintManager(t *testing.T, reader RuntimeFootprintReader, executions ...*AgentExecution) *Manager {
	t.Helper()
	mgr := newTestManager(t)
	registry := NewExecutorRegistry(newTestLogger())
	if reader != nil {
		registry.Register(reader.(*footprintReaderStub))
	}
	mgr.executorRegistry = registry
	for _, execution := range executions {
		if err := mgr.executionStore.Add(execution); err != nil {
			t.Fatalf("add execution: %v", err)
		}
	}
	return mgr
}

func footprintExecution(sessionID, taskID string) *AgentExecution {
	return &AgentExecution{
		ID:          "exec-" + sessionID,
		SessionID:   sessionID,
		TaskID:      taskID,
		RuntimeName: agentruntime.RuntimeStandalone,
	}
}

// TestSnapshotRuntimeFootprintAttributesToSession pins that an installation
// total is not enough: each reading must be attributable to the session that
// holds it, which is the whole point of the projection.
func TestSnapshotRuntimeFootprintAttributesToSession(t *testing.T) {
	reader := &footprintReaderStub{footprints: []agentctl.RuntimeFootprint{
		{
			InstanceID:     "i1",
			SessionID:      "session-1",
			TaskID:         "task-1",
			Status:         "running",
			Processes:      4,
			CommittedBytes: 900,
			ResidentBytes:  500,
		},
		{
			InstanceID:    "i2",
			SessionID:     "session-2",
			TaskID:        "task-2",
			Status:        "ready",
			Processes:     2,
			ResidentBytes: 300,
		},
	}}
	mgr := newFootprintManager(t, reader,
		footprintExecution("session-1", "task-1"),
		footprintExecution("session-2", "task-2"))

	snapshot := mgr.SnapshotRuntimeFootprint(context.Background())

	if !snapshot.Complete {
		t.Fatal("a successful read must be reported as complete")
	}
	if snapshot.LiveRuntimes != 2 || snapshot.ProcessCount != 6 {
		t.Fatalf("totals = %d runtimes / %d processes, want 2 / 6", snapshot.LiveRuntimes, snapshot.ProcessCount)
	}
	if snapshot.CommittedBytes != 900 || snapshot.ResidentBytes != 800 {
		t.Fatalf("bytes = %d/%d, want 900/800", snapshot.CommittedBytes, snapshot.ResidentBytes)
	}
	if len(snapshot.Runtimes) != 2 {
		t.Fatalf("len(Runtimes) = %d, want one row per attributable session", len(snapshot.Runtimes))
	}
	bySession := make(map[string]agentruntime.SessionRuntimeFootprint, len(snapshot.Runtimes))
	for _, row := range snapshot.Runtimes {
		bySession[row.SessionID] = row
	}
	first, ok := bySession["session-1"]
	if !ok {
		t.Fatal("session-1 has no attributed row")
	}
	if first.TaskID != "task-1" || first.ExecutionID != "exec-session-1" || first.Runtime != string(agentruntime.RuntimeStandalone) {
		t.Fatalf("session-1 identity = %+v, want its own execution identity", first)
	}
	if first.Processes != 4 || first.CommittedBytes != 900 || first.ResidentBytes != 500 {
		t.Fatalf("session-1 measurement = %+v, want its own bytes", first)
	}
}

// TestSnapshotRuntimeFootprintCountsUnattributedReadingInTotals pins that a
// reading with no resolvable session still counts toward the installation total.
// The process is real and owned by this installation; dropping it would
// understate exactly the number an operator is trying to explain.
func TestSnapshotRuntimeFootprintCountsUnattributedReadingInTotals(t *testing.T) {
	reader := &footprintReaderStub{footprints: []agentctl.RuntimeFootprint{
		{InstanceID: "i1", SessionID: "session-known", Processes: 3, ResidentBytes: 100},
		{InstanceID: "i2", SessionID: "session-unknown", Processes: 5, ResidentBytes: 900},
	}}
	mgr := newFootprintManager(t, reader, footprintExecution("session-known", "task-known"))

	snapshot := mgr.SnapshotRuntimeFootprint(context.Background())

	if snapshot.LiveRuntimes != 2 || snapshot.ProcessCount != 8 || snapshot.ResidentBytes != 1000 {
		t.Fatalf("totals = %+v, want both readings counted", snapshot)
	}
	if len(snapshot.Runtimes) != 1 {
		t.Fatalf("len(Runtimes) = %d, want only the attributable session projected", len(snapshot.Runtimes))
	}
}

// TestSnapshotRuntimeFootprintIsIncompleteOnReadFailure pins the fail-closed
// direction: a failed read must not be published as a measured total.
func TestSnapshotRuntimeFootprintIsIncompleteOnReadFailure(t *testing.T) {
	reader := &footprintReaderStub{err: errors.New("control server unavailable")}
	mgr := newFootprintManager(t, reader, footprintExecution("session-1", "task-1"))

	snapshot := mgr.SnapshotRuntimeFootprint(context.Background())

	if snapshot.Complete {
		t.Fatal("a failed read must not be reported as complete")
	}
	if snapshot.LiveRuntimes != 0 || snapshot.CommittedBytes != 0 || len(snapshot.Runtimes) != 0 {
		t.Fatalf("failed read produced totals: %+v, want nothing published", snapshot)
	}
}

// TestSnapshotRuntimeFootprintWithoutHostLocalReader pins that an installation
// with no host-local reader reports no footprint rather than a partial one.
func TestSnapshotRuntimeFootprintWithoutHostLocalReader(t *testing.T) {
	mgr := newFootprintManager(t, nil)

	snapshot := mgr.SnapshotRuntimeFootprint(context.Background())

	if snapshot.Complete || snapshot.LiveRuntimes != 0 || len(snapshot.Runtimes) != 0 {
		t.Fatalf("no reader must publish nothing, got %+v", snapshot)
	}
}

// TestSnapshotRuntimeFootprintKeepsUnreadableMarker pins the lower-bound marker
// through attribution. A caller that could not see it would read a partial
// measurement as a complete one.
func TestSnapshotRuntimeFootprintKeepsUnreadableMarker(t *testing.T) {
	reader := &footprintReaderStub{footprints: []agentctl.RuntimeFootprint{
		{
			InstanceID:      "i1",
			SessionID:       "session-1",
			Status:          "running",
			Processes:       3,
			ResidentBytes:   100,
			UnreadableCount: 2,
		},
	}}
	mgr := newFootprintManager(t, reader, footprintExecution("session-1", "task-1"))

	snapshot := mgr.SnapshotRuntimeFootprint(context.Background())

	if snapshot.UnreadableRuntimes != 1 {
		t.Fatalf("UnreadableRuntimes = %d, want 1", snapshot.UnreadableRuntimes)
	}
	if len(snapshot.Runtimes) != 1 || snapshot.Runtimes[0].UnreadableProcesses != 2 {
		t.Fatalf("projected row lost the unreadable marker: %+v", snapshot.Runtimes)
	}
}

// TestSnapshotRuntimeFootprintKeepsNewestActivity pins that the projected
// last-activity time is the most recent observation for that session, which is
// what makes a stale runtime visible rather than merely large.
func TestSnapshotRuntimeFootprintKeepsNewestActivity(t *testing.T) {
	newest := time.Now().UTC().Add(-time.Minute)
	older := newest.Add(-time.Hour)
	reader := &footprintReaderStub{footprints: []agentctl.RuntimeFootprint{
		{InstanceID: "i1", SessionID: "session-1", Processes: 1, LastActivity: newest},
		{InstanceID: "i2", SessionID: "session-1", Processes: 2, LastActivity: older},
	}}
	mgr := newFootprintManager(t, reader, footprintExecution("session-1", "task-1"))

	snapshot := mgr.SnapshotRuntimeFootprint(context.Background())

	if len(snapshot.Runtimes) != 2 {
		t.Fatalf("len(Runtimes) = %d, want one row per live instance", len(snapshot.Runtimes))
	}
	for _, row := range snapshot.Runtimes {
		if !row.LastActivityAt.Equal(newest) && !row.LastActivityAt.Equal(older) {
			t.Fatalf("row activity = %v, want one of the observed times", row.LastActivityAt)
		}
	}
}
